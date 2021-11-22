package sharding

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/beacon"
	act "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/filecoin-project/lotus/chain/sharding/actors/shard"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/node/impl/client"
	"github.com/filecoin-project/lotus/node/impl/common"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/impl/market"
	"github.com/filecoin-project/lotus/node/impl/net"
	"github.com/filecoin-project/lotus/node/impl/paych"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	init_ "github.com/filecoin-project/specs-actors/actors/builtin/init"
	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
)

var log = logging.Logger("sharding")

// ShardingSub is the sharding manager in the root chain
type ShardingSub struct {
	ctx context.Context
	// Listener for events of the root chain.
	events *events.Events
	// This is the API for the fullNode in the root chain.
	// api  *impl.FullNodeAPI
	api  *SubnetAPI
	host host.Host

	pubsub *pubsub.PubSub
	// Root ds
	ds           dtypes.MetadataDS
	beacon       beacon.Schedule
	syscalls     vm.SyscallBuilder
	us           stmgr.UpgradeSchedule
	verifier     ffiwrapper.Verifier
	nodeServer   api.FullNodeServer
	pmgr         peermgr.MaybePeerMgr
	bootstrapper dtypes.Bootstrapper

	lk     sync.RWMutex
	shards map[naming.SubnetID]*Shard

	j journal.Journal
}

func NewShardSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	//api impl.FullNodeAPI,
	self peer.ID,
	pubsub *pubsub.PubSub,
	ds dtypes.MetadataDS,
	host host.Host,
	beacon beacon.Schedule,
	syscalls vm.SyscallBuilder,
	us stmgr.UpgradeSchedule,
	nodeServer api.FullNodeServer,
	verifier ffiwrapper.Verifier,
	pmgr peermgr.MaybePeerMgr,
	bootstrapper dtypes.Bootstrapper,
	commonapi common.CommonAPI,
	netapi net.NetAPI,
	chainapi full.ChainAPI,
	clientapi client.API,
	mpoolapi full.MpoolAPI,
	gasapi full.GasAPI,
	marketapi market.MarketAPI,
	paychapi paych.PaychAPI,
	stateapi full.StateAPI,
	msigapi full.MsigAPI,
	walletapi full.WalletAPI,
	netName dtypes.NetworkName,
	syncapi full.SyncAPI,
	beaconapi full.BeaconAPI,
	j journal.Journal) (*ShardingSub, error) {

	var err error
	ctx := helpers.LifecycleCtx(mctx, lc)
	s := &ShardingSub{
		ctx:          ctx,
		pubsub:       pubsub,
		host:         host,
		ds:           ds,
		syscalls:     syscalls,
		us:           us,
		j:            j,
		pmgr:         pmgr,
		nodeServer:   nodeServer,
		bootstrapper: bootstrapper,
		verifier:     verifier,
		shards:       make(map[naming.SubnetID]*Shard),
	}

	s.api = &SubnetAPI{
		commonapi,
		netapi,
		chainapi,
		clientapi,
		mpoolapi,
		gasapi,
		marketapi,
		paychapi,
		stateapi,
		msigapi,
		walletapi,
		syncapi,
		beaconapi,
		ds,
		netName,
		s,
	}

	// Starting shardSub to listen to events in the root chain.
	s.events, err = events.NewEvents(ctx, s.api)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *ShardingSub) startShard(ctx context.Context, id naming.SubnetID,
	parentAPI *SubnetAPI, consensus shard.ConsensusType,
	genesis []byte) error {
	var err error
	// Subnets inherit the context from the SubnetManager.
	ctx, cancel := context.WithCancel(s.ctx)

	log.Infow("Creating new shard", "shardID", id)
	sh := &Shard{
		ctx:        ctx,
		ctxCancel:  cancel,
		ID:         id,
		host:       s.host,
		pubsub:     s.pubsub,
		nodeServer: s.nodeServer,
		pmgr:       s.pmgr,
		consType:   consensus,
	}

	// Add shard to registry
	s.shards[id] = sh

	// Wrap the ds with prefix
	sh.ds = nsds.Wrap(s.ds, ds.NewKey(sh.ID.String()))
	// TODO: We should not use the metadata datastore here. We need
	// to create the corresponding blockstores. Deferring once we
	// figure out if it works.
	sh.bs = blockstore.FromDatastore(s.ds)

	// Select the right TipSetExecutor for the consensus algorithms chosen.
	tsExec, err := tipSetExecutor(consensus)
	if err != nil {
		log.Errorw("Error getting TipSetExecutor for consensus", "shardID", id, "err", err)
		return err
	}
	weight, err := weight(consensus)
	if err != nil {
		log.Errorw("Error getting weight for consensus", "shardID", id, "err", err)
		return err
	}

	sh.ch = store.NewChainStore(sh.bs, sh.bs, sh.ds, weight, s.j)
	sh.sm, err = stmgr.NewStateManager(sh.ch, tsExec, s.syscalls, s.us, s.beacon)
	if err != nil {
		log.Errorw("Error creating state manager for shard", "shardID", id, "err", err)
		return err
	}
	// Start state manager.
	sh.sm.Start(ctx)

	gen, err := sh.LoadGenesis(genesis)
	if err != nil {
		log.Errorw("Error loading genesis bootstrap for shard", "shardID", id, "err", err)
		return err
	}
	sh.cons, err = newConsensus(consensus, sh.sm, s.beacon, s.verifier, gen)
	if err != nil {
		log.Errorw("Error creating consensus", "shardID", id, "err", err)
		return err
	}
	log.Infow("Genesis and consensus for shard created", "shardID", id, "consensus", consensus)

	// We configure a new handler for the shard syncing exchange protocol.
	sh.exchangeServer()
	// We are passing to the syncer a new exchange client for the shard to enable
	// peers to catch up with the shard chain.
	// NOTE: We reuse the same peer manager from the root chain.
	sh.syncer, err = chain.NewSyncer(sh.ds, sh.sm, sh.exchangeClient(ctx), chain.NewSyncManager, s.host.ConnManager(), s.host.ID(), s.beacon, gen, sh.cons)
	if err != nil {
		log.Errorw("Error creating syncer for shard", "shardID", id, "err", err)
		return err
	}
	// Start syncer for the shard
	sh.syncer.Start()
	// Hello protocol needs to run after the syncer is intialized and the genesis
	// is created but before we set-up the gossipsub topics to listen for
	// new blocks and messages.
	sh.runHello(ctx)
	bserv := blockservice.New(sh.bs, offline.Exchange(sh.bs))
	prov := messagepool.NewProvider(sh.sm, s.pubsub)

	sh.mpool, err = messagepool.New(prov, sh.ds, s.us, dtypes.NetworkName(sh.ID.String()), s.j)
	if err != nil {
		log.Errorw("Error creating message pool for shard", "shardID", id, "err", err)
		return err
	}

	// This functions create a new pubsub topic for the shard to start
	// listening to new messages and blocks for the shard.
	err = sh.HandleIncomingBlocks(ctx, bserv)
	if err != nil {
		log.Errorw("HandleIncomingBlocks failed for shard", "shardID", id, "err", err)
		return err
	}
	err = sh.HandleIncomingMessages(ctx, s.bootstrapper)
	if err != nil {
		log.Errorw("HandleIncomingMessages failed for shard", "shardID", id, "err", err)
		return err
	}
	log.Infow("Listening for new blocks and messages in shard", "shardID", id)

	log.Infow("Populating and registering API for", "shardID", id)
	err = sh.populateAPIs(parentAPI, s.host, tsExec)
	if err != nil {
		log.Errorw("Error populating APIs for shard", "shardID", id, "err", err)
		return err
	}

	// Listening to events on the shard actor from the shard chain.
	// We can create new shards from an existing one, and we need to
	// monitor that (thus the "hierarchical" in the consensus).
	sh.events, err = events.NewEvents(ctx, sh.api)
	if err != nil {
		log.Errorw("Events couldn't be initialized for shard", "shardID", id, "err", err)
		return err
	}
	go s.listenShardEvents(ctx, sh)
	log.Infow("Listening to shard events in shard", "shardID", id)

	/* TODO: Determine when to start mining in a chain. This may be an
	independent command
		// If is miner start mining in shard.
		if info.isMiner {
			sh.mine(ctx)
		}
	*/

	log.Infow("Successfully spawned shard", "shardID", id)

	return nil
}

func (s *ShardingSub) listenShardEvents(ctx context.Context, sh *Shard) {
	api := s.api
	evs := s.events
	id := naming.Root

	// If shard is nil, we are listening from the root chain.
	if sh != nil {
		id = sh.ID
		api = sh.api
		evs = sh.events
	}

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for shard actor in shard", "shardID", id)

		// Add the new shard to the struct.
		shardMap, ok := states.(map[string]diffInfo)
		if !ok {
			log.Error("Error casting diff structure")
			return true, err
		}

		// Trigger the detected change in shards.
		return s.triggerChange(ctx, api, shardMap)

	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		/*
				oldAct, err := api.StateGetActor(ctx, shardactor.ShardActorAddr, oldTs.Key())
				if err != nil {
					return false, nil, err
				}
				newAct, err := api.StateGetActor(ctx, shardactor.ShardActorAddr, newTs.Key())
				if err != nil {
					return false, nil, err
				}

			var oldSt, newSt shardactor.ShardState

			bs := blockstore.NewAPIBlockstore(api)
			cst := cbor.NewCborStore(bs)
			if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
				return false, nil, err
			}
			if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
				return false, nil, err
			}

			// If no changes in the state return false.
			if reflect.DeepEqual(newSt, oldSt) {
				return false, nil, nil
			}

			/*
				outDiff := make(map[string]diffInfo)
				// Start running state checks to build diff.
				// Check first if there are new shards.
				if err := s.checkNewShard(ctx, outDiff, cst, oldSt, newSt); err != nil {
					log.Errorw("error in checkNewShard", "err", err)
					return false, nil, err
				}
				// Check if state in existing shards has changed.
				if err := s.checkShardChange(ctx, outDiff, cst, oldSt, newSt); err != nil {
					log.Errorw("error in checkShardChange", "err", err)
					return false, nil, err
				}

				if len(outDiff) > 0 {
					return true, outDiff, nil
				}
		*/

		return false, nil, nil

	}

	err := evs.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (s *ShardingSub) triggerChange(ctx context.Context, api *SubnetAPI, shardMap map[string]diffInfo) (more bool, err error) {
	/*
		s.lk.Lock()
		defer s.lk.Unlock()
		// For each new shard detected
		for k, diff := range shardMap {
			// If the shard has not been removed.
			if !diff.isRm {
				log.Infow("Change event triggered for shard", "shardID", k)
				_, ok := s.shards[k]
				// If we are not already subscribed to the shard.
				if !ok {
					// Create shard with id from parentAPI
					err := s.startShard(ctx, k, api, diff)
					if err != nil {
						log.Errorw("Error creating new shard", "shardID", k, "err", err)
						return true, err
					}
				} else {
					// If diff says we can start mining and we are not mining
					if mining := s.shards[k].isMining(); !mining && shardMap[k].isMiner {
						s.shards[k].mine(ctx)
					}
					// TODO: We currently don't support taking part of the stake
					// from the shard, if we eventually do so, we'll also need
					// to check if we've been removed from the list of miners
					// in which case we'll need to stop mining here.
				}
			} else {
				log.Infow("Leave event triggered for shard", "shardID", k)
				// Stop all processes for shard.
				err := s.shards[k].Close(ctx)
				if err != nil {
					log.Errorw("error closing shard", "shardID", k, "err", err)
					return true, err
				}
				// Remove from shard registry.
				delete(s.shards, k)
				return false, nil
			}
		}
	*/
	return true, nil
}
func (s *ShardingSub) Start(ctx context.Context) {
	s.listenShardEvents(ctx, nil)
}

func (s *ShardingSub) Close(ctx context.Context) error {
	for _, sh := range s.shards {
		err := sh.Close(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func BuildShardingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, s *ShardingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	s.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// NOTE: Closing sharding sub here. Whatever the hell that means...
			// It may be worth revisiting this.
			return s.Close(ctx)
		},
	})

}

func (s *ShardingSub) AddShard(
	ctx context.Context, wallet address.Address,
	parent naming.SubnetID, name string,
	consensus uint64, minerStake abi.TokenAmount,
	delegminer address.Address) (address.Address, error) {

	// Get the api for the parent network hosting the shard actor
	// for the subnet.
	parentAPI := s.getAPI(parent)
	if parentAPI == nil {

		return address.Undef, xerrors.Errorf("not syncing with parent network")
	}
	// Populate constructor parameters for subnet actor
	addp := &shard.ConstructParams{
		NetworkName:   string(s.api.NetName),
		MinMinerStake: minerStake,
		Name:          name,
		Consensus:     shard.ConsensusType(consensus),
		DelegMiner:    delegminer,
	}

	seraddp, err := actors.SerializeParams(addp)
	if err != nil {
		return address.Undef, err
	}

	params := &init_.ExecParams{
		CodeCID:           act.ShardActorCodeID,
		ConstructorParams: seraddp,
	}
	serparams, err := actors.SerializeParams(params)
	if err != nil {
		return address.Undef, xerrors.Errorf("failed serializing init actor params: %s", err)
	}

	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     builtin.InitActorAddr,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsInit.Exec,
		Params: serparams,
	}, nil)
	if aerr != nil {
		return address.Undef, aerr
	}

	msg := smsg.Cid()
	mw, aerr := parentAPI.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return address.Undef, aerr
	}

	fmt.Printf("Return: %x\n", mw.Receipt.Return)
	r := &init_.ExecReturn{}
	if err := r.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		return address.Undef, err
	}
	return r.IDAddress, nil
}

func (s *ShardingSub) getAPI(n naming.SubnetID) *SubnetAPI {
	if n.String() == string(s.api.NetName) {
		return s.api
	}
	sh, ok := s.shards[n]
	if !ok {
		return nil
	}
	return sh.api
}

func (s *ShardingSub) getSubnet(n naming.SubnetID) (*Shard, error) {
	sh, ok := s.shards[n]
	if !ok {
		return nil, xerrors.Errorf("Not part of subnet %v. Consider joining it", n)
	}
	return sh, nil
}

func (a *SubnetAPI) getActorState(ctx context.Context, shardActor address.Address) (*shard.ShardState, error) {
	act, err := a.StateGetActor(ctx, shardActor, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	var st shard.ShardState
	bs := blockstore.NewAPIBlockstore(a)
	cst := cbor.NewCborStore(bs)
	if err := cst.Get(ctx, act.Head, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *ShardingSub) JoinShard(
	ctx context.Context, wallet address.Address,
	value abi.TokenAmount,
	id naming.SubnetID) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for shards.
	s.lk.Lock()
	defer s.lk.Unlock()

	// Get actor from subnet ID
	shardActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the shard actor
	// for the subnet.
	parentAPI := s.getAPI(id.Parent())
	if parentAPI == nil {
		return cid.Undef, xerrors.Errorf("not syncing with parent network")
	}

	// Get the parent and the actor to know where to send the message.
	// Not everything needs to be sent to the root.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     shardActor,
		From:   wallet,
		Value:  value,
		Method: shard.Methods.Join,
		Params: nil,
	}, nil)
	if aerr != nil {
		return cid.Undef, aerr
	}

	msg := smsg.Cid()

	// Wait state message.
	_, aerr = parentAPI.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return cid.Undef, aerr
	}

	// See if we are already syncing with that chain. If this
	// is the case we don't have to do much after the stake has been added.
	if s.getAPI(id) != nil {
		log.Infow("Already joined subnet %v. Adding more stake to shard", id)
		return smsg.Cid(), nil
	}

	// If not we need to initialize the subnet in our client to start syncing.
	// Get genesis from actor state.
	st, err := parentAPI.getActorState(ctx, shardActor)
	if err != nil {
		return cid.Undef, nil
	}
	err = s.startShard(ctx, id, parentAPI, st.Consensus, st.Genesis)

	return smsg.Cid(), nil
}

func (s *ShardingSub) Mine(
	ctx context.Context, wallet address.Address,
	id naming.SubnetID, stop bool) error {

	// TODO: Think a bit deeper the locking strategy for shards.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	shardActor, err := id.Actor()
	if err != nil {
		return err
	}

	// Get subnet
	sh, err := s.getSubnet(id)
	if err != nil {
		return err
	}

	// If stop try to stop mining right away
	if stop {
		return sh.stopMining(ctx)
	}

	// Get the api for the parent network hosting the shard actor
	// for the subnet.
	parentAPI := s.getAPI(id.Parent())
	if parentAPI == nil {
		return xerrors.Errorf("not syncing with parent network")
	}
	// Get actor state to check if the shard is active and we are in the list
	// of miners
	st, err := parentAPI.getActorState(ctx, shardActor)
	if err != nil {
		return nil
	}

	if st.IsMiner(wallet) && st.Status != shard.Killed {
		log.Infow("Starting to mine subnet", "subnetID", id)
		// We need to start mining from the context of the subnet manager.
		return sh.mine(s.ctx)
	}

	return xerrors.Errorf("Address %v Not a miner in subnet, or subnet already killed", wallet)
}

func (s *ShardingSub) Leave(
	ctx context.Context, wallet address.Address,
	id naming.SubnetID) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for shards.
	s.lk.Lock()
	defer s.lk.Unlock()

	// Get actor from subnet ID
	shardActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the shard actor
	// for the subnet.
	parentAPI := s.getAPI(id.Parent())
	if parentAPI == nil {
		return cid.Undef, xerrors.Errorf("not syncing with parent network")
	}

	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     shardActor,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: shard.Methods.Leave,
		Params: nil,
	}, nil)
	if aerr != nil {
		return cid.Undef, aerr
	}

	msg := smsg.Cid()

	// Wait state message.
	_, aerr = parentAPI.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return cid.Undef, aerr
	}

	// See if we are already syncing with that chain. If this
	// is the case we can remove the subnet
	if sh, _ := s.getSubnet(id); s != nil {
		log.Infow("Stop syncing with subnet", "subnetID", id)
		delete(s.shards, id)
		return msg, sh.Close(ctx)
	}

	return smsg.Cid(), nil
}

func (s *ShardingSub) Kill(
	ctx context.Context, wallet address.Address,
	id naming.SubnetID) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for shards.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	shardActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the shard actor
	// for the subnet.
	parentAPI := s.getAPI(id.Parent())
	if parentAPI == nil {
		return cid.Undef, xerrors.Errorf("not syncing with parent network")
	}

	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     shardActor,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: shard.Methods.Kill,
		Params: nil,
	}, nil)
	if aerr != nil {
		return cid.Undef, aerr
	}

	msg := smsg.Cid()

	// Wait state message.
	_, aerr = parentAPI.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return cid.Undef, aerr
	}

	log.Infow("Successfully send kill signal to ", "subnetID", id)

	return smsg.Cid(), nil
}
