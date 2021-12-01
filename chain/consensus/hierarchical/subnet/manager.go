package subnet

import (
	"bytes"
	"context"
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
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	ctypes "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/messagepool"
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
	"github.com/filecoin-project/specs-actors/actors/util/adt"
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

var log = logging.Logger("subnet")

// SubnetMgr is the subneting manager in the root chain
type SubnetMgr struct {
	ctx context.Context
	// Listener for events of the root chain.
	events *events.Events
	// This is the API for the fullNode in the root chain.
	// api  *impl.FullNodeAPI
	api  *API
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

	lk      sync.RWMutex
	subnets map[hierarchical.SubnetID]*Subnet

	j journal.Journal
}

func NewSubnetMgr(
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
	j journal.Journal) (*SubnetMgr, error) {

	var err error
	ctx := helpers.LifecycleCtx(mctx, lc)
	s := &SubnetMgr{
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
		subnets:      make(map[hierarchical.SubnetID]*Subnet),
	}

	s.api = &API{
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

	// Starting subnetSub to listen to events in the root chain.
	s.events, err = events.NewEvents(ctx, s.api)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *SubnetMgr) startSubnet(id hierarchical.SubnetID,
	parentAPI *API, consensus subnet.ConsensusType,
	genesis []byte) error {
	var err error
	// Subnets inherit the context from the SubnetManager.
	ctx, cancel := context.WithCancel(s.ctx)

	log.Infow("Creating new subnet", "subnetID", id)
	sh := &Subnet{
		ctx:        ctx,
		ctxCancel:  cancel,
		ID:         id,
		host:       s.host,
		pubsub:     s.pubsub,
		nodeServer: s.nodeServer,
		pmgr:       s.pmgr,
		consType:   consensus,
	}

	// Add subnet to registry
	s.subnets[id] = sh

	// Wrap the ds with prefix
	sh.ds = nsds.Wrap(s.ds, ds.NewKey(sh.ID.String()))
	// TODO: We should not use the metadata datastore here. We need
	// to create the corresponding blockstores. Deferring once we
	// figure out if it works.
	sh.bs = blockstore.FromDatastore(s.ds)

	// Select the right TipSetExecutor for the consensus algorithms chosen.
	tsExec, err := tipSetExecutor(consensus)
	if err != nil {
		log.Errorw("Error getting TipSetExecutor for consensus", "subnetID", id, "err", err)
		return err
	}
	weight, err := weight(consensus)
	if err != nil {
		log.Errorw("Error getting weight for consensus", "subnetID", id, "err", err)
		return err
	}

	sh.ch = store.NewChainStore(sh.bs, sh.bs, sh.ds, weight, s.j)
	sh.sm, err = stmgr.NewStateManager(sh.ch, tsExec, s.syscalls, s.us, s.beacon)
	if err != nil {
		log.Errorw("Error creating state manager for subnet", "subnetID", id, "err", err)
		return err
	}
	// Start state manager.
	sh.sm.Start(ctx)

	gen, err := sh.LoadGenesis(genesis)
	if err != nil {
		log.Errorw("Error loading genesis bootstrap for subnet", "subnetID", id, "err", err)
		return err
	}
	sh.cons, err = newConsensus(consensus, sh.sm, s.beacon, s.verifier, gen)
	if err != nil {
		log.Errorw("Error creating consensus", "subnetID", id, "err", err)
		return err
	}
	log.Infow("Genesis and consensus for subnet created", "subnetID", id, "consensus", consensus)

	// We configure a new handler for the subnet syncing exchange protocol.
	sh.exchangeServer()
	// We are passing to the syncer a new exchange client for the subnet to enable
	// peers to catch up with the subnet chain.
	// NOTE: We reuse the same peer manager from the root chain.
	sh.syncer, err = chain.NewSyncer(sh.ds, sh.sm, sh.exchangeClient(ctx), chain.NewSyncManager, s.host.ConnManager(), s.host.ID(), s.beacon, gen, sh.cons)
	if err != nil {
		log.Errorw("Error creating syncer for subnet", "subnetID", id, "err", err)
		return err
	}
	// Start syncer for the subnet
	sh.syncer.Start()
	// Hello protocol needs to run after the syncer is intialized and the genesis
	// is created but before we set-up the gossipsub topics to listen for
	// new blocks and messages.
	sh.runHello(ctx)
	bserv := blockservice.New(sh.bs, offline.Exchange(sh.bs))
	prov := messagepool.NewProvider(sh.sm, s.pubsub)

	sh.mpool, err = messagepool.New(prov, sh.ds, s.us, dtypes.NetworkName(sh.ID.String()), s.j)
	if err != nil {
		log.Errorw("Error creating message pool for subnet", "subnetID", id, "err", err)
		return err
	}

	// This functions create a new pubsub topic for the subnet to start
	// listening to new messages and blocks for the subnet.
	err = sh.HandleIncomingBlocks(ctx, bserv)
	if err != nil {
		log.Errorw("HandleIncomingBlocks failed for subnet", "subnetID", id, "err", err)
		return err
	}
	err = sh.HandleIncomingMessages(ctx, s.bootstrapper)
	if err != nil {
		log.Errorw("HandleIncomingMessages failed for subnet", "subnetID", id, "err", err)
		return err
	}
	log.Infow("Listening for new blocks and messages in subnet", "subnetID", id)

	log.Infow("Populating and registering API for", "subnetID", id)
	err = sh.populateAPIs(parentAPI, s.host, tsExec)
	if err != nil {
		log.Errorw("Error populating APIs for subnet", "subnetID", id, "err", err)
		return err
	}

	// Listening to events on the subnet actor from the subnet chain.
	// We can create new subnets from an existing one, and we need to
	// monitor that (thus the "hierarchical" in the consensus).
	sh.events, err = events.NewEvents(ctx, sh.api)
	if err != nil {
		log.Errorw("Events couldn't be initialized for subnet", "subnetID", id, "err", err)
		return err
	}
	go s.listenSubnetEvents(ctx, sh)
	log.Infow("Listening to SCA events in subnet", "subnetID", id)

	log.Infow("Successfully spawned subnet", "subnetID", id)

	return nil
}

func (s *SubnetMgr) Start(ctx context.Context) {
	// Start listening to events in the SCA contract from root right away.
	// Every peer in the hierarchy needs to be aware of these events.
	s.listenSubnetEvents(ctx, nil)
}

func (s *SubnetMgr) Close(ctx context.Context) error {
	for _, sh := range s.subnets {
		err := sh.Close(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func BuildSubnetMgr(mctx helpers.MetricsCtx, lc fx.Lifecycle, s *SubnetMgr) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	s.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// NOTE: Closing subneting sub here. Whatever the hell that means...
			// It may be worth revisiting this.
			return s.Close(ctx)
		},
	})

}

func (s *SubnetMgr) AddSubnet(
	ctx context.Context, wallet address.Address,
	parent hierarchical.SubnetID, name string,
	consensus uint64, minerStake abi.TokenAmount,
	checkPeriod abi.ChainEpoch,
	delegminer address.Address) (address.Address, error) {

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI := s.getAPI(parent)
	if parentAPI == nil {
		return address.Undef, xerrors.Errorf("not syncing with parent network")
	}
	// Populate constructor parameters for subnet actor
	addp := &subnet.ConstructParams{
		NetworkName:   string(s.api.NetName),
		MinMinerStake: minerStake,
		Name:          name,
		Consensus:     subnet.ConsensusType(consensus),
		DelegMiner:    delegminer,
		CheckPeriod:   checkPeriod,
	}

	seraddp, err := actors.SerializeParams(addp)
	if err != nil {
		return address.Undef, err
	}

	params := &init_.ExecParams{
		CodeCID:           act.SubnetActorCodeID,
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

	r := &init_.ExecReturn{}
	if err := r.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		return address.Undef, err
	}
	return r.IDAddress, nil
}

func (s *SubnetMgr) JoinSubnet(
	ctx context.Context, wallet address.Address,
	value abi.TokenAmount,
	id hierarchical.SubnetID) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.Lock()
	defer s.lk.Unlock()

	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	// Get the parent and the actor to know where to send the message.
	// Not everything needs to be sent to the root.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     SubnetActor,
		From:   wallet,
		Value:  value,
		Method: subnet.Methods.Join,
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
		log.Infow("Already joined subnet %v. Adding more stake to subnet", "subnetID", id)
		return smsg.Cid(), nil
	}

	// If not we need to initialize the subnet in our client to start syncing.
	// Get genesis from actor state.
	st, err := parentAPI.getActorState(ctx, SubnetActor)
	if err != nil {
		return cid.Undef, nil
	}
	err = s.startSubnet(id, parentAPI, st.Consensus, st.Genesis)
	if err != nil {
		return cid.Undef, err
	}

	return smsg.Cid(), nil
}

func (s *SubnetMgr) MineSubnet(
	ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID, stop bool) error {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
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

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return err
	}
	// Get actor state to check if the subnet is active and we are in the list
	// of miners
	st, err := parentAPI.getActorState(ctx, SubnetActor)
	if err != nil {
		return nil
	}

	if st.IsMiner(wallet) && st.Status != subnet.Killed {
		log.Infow("Starting to mine subnet", "subnetID", id)
		// We need to start mining from the context of the subnet manager.
		return sh.mine(s.ctx)
	}

	return xerrors.Errorf("Address %v Not a miner in subnet, or subnet already killed", wallet)
}

func (s *SubnetMgr) LeaveSubnet(
	ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.Lock()
	defer s.lk.Unlock()

	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     SubnetActor,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: subnet.Methods.Leave,
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
		delete(s.subnets, id)
		return msg, sh.Close(ctx)
	}

	return smsg.Cid(), nil
}

func (s *SubnetMgr) KillSubnet(
	ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     SubnetActor,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: subnet.Methods.Kill,
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

func (s *SubnetMgr) SubmitSignedCheckpoint(
	ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID, ch *schema.Checkpoint) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	b, err := ch.MarshalBinary()
	if err != nil {
		return cid.Undef, err
	}
	params := &sca.CheckpointParams{Checkpoint: b}
	serparams, err := actors.SerializeParams(params)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed serializing init actor params: %s", err)
	}
	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     SubnetActor,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: subnet.Methods.SubmitCheckpoint,
		Params: serparams,
	}, nil)
	if aerr != nil {
		return cid.Undef, aerr
	}

	msg := smsg.Cid()

	/*
		// Wait state message.
		_, aerr = parentAPI.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
		if aerr != nil {
			return cid.Undef, aerr
		}
	*/

	chcid, _ := ch.Cid()
	log.Infow("Success signing checkpoint in subnet", "subnetID", id, "message", msg, "cid", chcid)
	return smsg.Cid(), nil
}

func (s *SubnetMgr) ListCheckpoints(
	ctx context.Context, id hierarchical.SubnetID, num int) ([]*schema.Checkpoint, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	subnetActAddr, err := id.Actor()
	if err != nil {
		return nil, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return nil, err
	}

	subAPI := s.getAPI(id)
	if subAPI == nil {
		xerrors.Errorf("Not listening to subnet")
	}

	subnetAct, err := parentAPI.StateGetActor(ctx, subnetActAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	var snst subnet.SubnetState
	pbs := blockstore.NewAPIBlockstore(parentAPI)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &snst); err != nil {
		return nil, err
	}
	pstore := adt.WrapStore(ctx, pcst)
	out := make([]*schema.Checkpoint, 0)
	ts := subAPI.ChainAPI.Chain.GetHeaviestTipSet()
	currEpoch := ts.Height()
	for i := 0; i < num; i++ {
		signWindow := ctypes.CheckpointEpoch(abi.ChainEpoch(int(currEpoch)-i*int(snst.CheckPeriod)), snst.CheckPeriod)
		if signWindow < 0 {
			break
		}
		ch, found, err := snst.GetCheckpoint(pstore, signWindow)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (s *SubnetMgr) getAPI(n hierarchical.SubnetID) *API {
	if n.String() == string(s.api.NetName) {
		return s.api
	}
	sh, ok := s.subnets[n]
	if !ok {
		return nil
	}
	return sh.api
}

func (s *SubnetMgr) getParentAPI(n hierarchical.SubnetID) (*API, error) {
	parentAPI := s.getAPI(n.Parent())
	if parentAPI == nil {
		return nil, xerrors.Errorf("not syncing with parent network")
	}
	return parentAPI, nil
}

func (s *SubnetMgr) getSubnet(n hierarchical.SubnetID) (*Subnet, error) {
	sh, ok := s.subnets[n]
	if !ok {
		return nil, xerrors.Errorf("Not part of subnet %v. Consider joining it", n)
	}
	return sh, nil
}
