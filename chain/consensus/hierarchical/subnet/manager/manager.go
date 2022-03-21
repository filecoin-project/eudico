package subnetmgr

import (
	"bytes"
	"context"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	init_ "github.com/filecoin-project/specs-actors/actors/builtin/init"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
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

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/beacon"
	act "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	subiface "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	subcns "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
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
	commonapi "github.com/filecoin-project/lotus/node/impl/common"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/impl/market"
	"github.com/filecoin-project/lotus/node/impl/net"
	"github.com/filecoin-project/lotus/node/impl/paych"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
)

var log = logging.Logger("subnetMgr")

// SubnetMgr is the subneting manager in the root chain
type SubnetMgr struct {
	ctx context.Context
	// Listener for events of the root chain.
	events *events.Events
	// This is the API for the fullNode in the root chain.
	// api  *impl.FullNodeAPI
	api  *API
	host host.Host
	self peer.ID

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
	subnets map[address.SubnetID]*Subnet

	// Cross-msg general pool
	cm *crossMsgPool
	// Root cross-msg resolver. Each subnet has one.
	r *resolver.Resolver

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
	commonapi commonapi.CommonAPI,
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
	r *resolver.Resolver,
	j journal.Journal) (*SubnetMgr, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)
	var err error

	s := &SubnetMgr{
		ctx:          ctx,
		pubsub:       pubsub,
		host:         host,
		self:         self,
		ds:           ds,
		syscalls:     syscalls,
		us:           us,
		j:            j,
		pmgr:         pmgr,
		nodeServer:   nodeServer,
		bootstrapper: bootstrapper,
		verifier:     verifier,
		subnets:      make(map[address.SubnetID]*Subnet),
		cm:           newCrossMsgPool(),
		r:            r,
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

func (s *SubnetMgr) startSubnet(id address.SubnetID,
	parentAPI *API, consensus hierarchical.ConsensusType,
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

	// Instantiate new cross-msg resolver
	sh.r = resolver.NewResolver(s.self, sh.ds, sh.pubsub, sh.ID)

	// Select the right TipSetExecutor for the consensus algorithms chosen.
	tsExec := common.TipSetExecutor(s)
	weight, err := subcns.Weight(consensus)
	if err != nil {
		log.Errorw("Error getting weight for consensus", "subnetID", id, "err", err)
		return err
	}

	sh.ch = store.NewChainStore(sh.bs, sh.bs, sh.ds, weight, s.j)
	sh.sm, err = stmgr.NewStateManager(sh.ch, tsExec, sh.r, s.syscalls, s.us, s.beacon)
	if err != nil {
		log.Errorw("Error creating state manager for subnet", "subnetID", id, "err", err)
		return err
	}
	// Start state manager.
	sh.sm.Start(ctx)

	gen, err := sh.LoadGenesis(ctx, genesis)
	if err != nil {
		log.Errorw("Error loading genesis bootstrap for subnet", "subnetID", id, "err", err)
		return err
	}
	// Instantiate consensus
	sh.cons, err = subcns.New(ctx, consensus, sh.sm, s, s.beacon, sh.r, s.verifier, gen, dtypes.NetworkName(id))
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

	// FIXME: Consider inheriting Bitswap ChainBlockService instead of using
	// offline.Exchange here. See builder_chain to undertand how is built.
	bserv := blockservice.New(sh.bs, offline.Exchange(sh.bs))
	prov := messagepool.NewProvider(sh.sm, s.pubsub)

	sh.mpool, err = messagepool.New(ctx, prov, sh.ds, s.us, dtypes.NetworkName(sh.ID.String()), s.j)
	if err != nil {
		log.Errorw("Error creating message pool for subnet", "subnetID", id, "err", err)
		return err
	}

	// Start listening to cross-msg resolve messages
	err = sh.r.HandleMsgs(ctx, s)
	if err != nil {
		return xerrors.Errorf("error initializing cross-msg resolver: %s", err)
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
			log.Errorf("error closing subnet %s: %w", sh.ID, err)
			// NOTE: Even if we fail to close a subnet we should continue
			// and not return. We shouldn't stop half-way.
			// return err
		}
	}
	// Close resolver
	return s.r.Close()
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
	parent address.SubnetID, name string,
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
		Consensus:     hierarchical.ConsensusType(consensus),
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
	id address.SubnetID) (cid.Cid, error) {

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
		log.Errorw("Error pushing join subnet message to parent api", "err", aerr)
		return cid.Undef, aerr
	}

	msg := smsg.Cid()

	// Wait state message.
	_, aerr = parentAPI.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		log.Errorw("Error waiting for message to be committed", "err", aerr)
		return cid.Undef, aerr
	}

	// See if we are already syncing with that chain. If this
	// is the case we don't have to do much after the stake has been added.
	if s.getAPI(id) != nil {
		log.Infow("Already joined subnet %v. Adding more stake to subnet", "subnetID", id)
		return smsg.Cid(), nil
	}

	// If not we need to initialize the subnet in our client to start syncing.
	err = s.syncSubnet(ctx, id, parentAPI)
	if err != nil {
		return cid.Undef, err
	}

	return smsg.Cid(), nil
}

func (s *SubnetMgr) syncSubnet(ctx context.Context, id address.SubnetID, parentAPI *API) error {
	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
	if err != nil {
		return err
	}
	// See if we are already syncing with that chain.
	if s.getAPI(id) != nil {
		return xerrors.Errorf("Already syncing with subnet: %v", id)
	}

	// Get genesis from actor state.
	st, err := parentAPI.getActorState(ctx, SubnetActor)
	if err != nil {
		return err
	}

	return s.startSubnet(id, parentAPI, st.Consensus, st.Genesis)
}

// SyncSubnet starts syncing with a subnet even if we are not an active participant.
func (s *SubnetMgr) SyncSubnet(ctx context.Context, id address.SubnetID, stop bool) error {
	if stop {
		return s.stopSyncSubnet(ctx, id)
	}
	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return err
	}
	return s.syncSubnet(ctx, id, parentAPI)
}

// stopSyncSubnet stops syncing from a subnet
func (s *SubnetMgr) stopSyncSubnet(ctx context.Context, id address.SubnetID) error {
	if sh, _ := s.getSubnet(id); sh != nil {
		delete(s.subnets, id)
		return sh.Close(ctx)
	}
	return xerrors.Errorf("Not currently syncing with subnet: %s", id)
}

func (s *SubnetMgr) MineSubnet(
	ctx context.Context, wallet address.Address,
	id address.SubnetID, stop bool) error {

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
		return err
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
	id address.SubnetID) (cid.Cid, error) {

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
	if sh, _ := s.getSubnet(id); sh != nil {
		log.Infow("Stop syncing with subnet", "subnetID", id)
		delete(s.subnets, id)
		return msg, sh.Close(ctx)
	}

	return smsg.Cid(), nil
}

func (s *SubnetMgr) KillSubnet(
	ctx context.Context, wallet address.Address,
	id address.SubnetID) (cid.Cid, error) {

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

// isRoot checks if the
func (s *SubnetMgr) isRoot(id address.SubnetID) bool {
	return id.String() == string(s.api.NetName)
}

func (s *SubnetMgr) getAPI(id address.SubnetID) *API {
	if s.isRoot(id) || id == address.RootSubnet {
		return s.api
	}
	sh, ok := s.subnets[id]
	if !ok {
		return nil
	}
	return sh.api
}

func (s *SubnetMgr) getParentAPI(id address.SubnetID) (*API, error) {
	parentAPI := s.getAPI(id.Parent())
	if parentAPI == nil {
		return nil, xerrors.Errorf("not syncing with parent network")
	}
	return parentAPI, nil
}

func (s *SubnetMgr) getSubnet(id address.SubnetID) (*Subnet, error) {
	sh, ok := s.subnets[id]
	if !ok {
		return nil, xerrors.Errorf("Not part of subnet %v. Consider joining it", id)
	}
	return sh, nil
}

func (s *SubnetMgr) GetSubnetAPI(id address.SubnetID) (v1api.FullNode, error) {
	api := s.getAPI(id)
	if api == nil {
		return nil, xerrors.Errorf("subnet manager not syncing with network")
	}
	return api, nil
}

func (s *SubnetMgr) GetSCAState(ctx context.Context, id address.SubnetID) (*sca.SCAState, blockadt.Store, error) {
	api, err := s.GetSubnetAPI(id)
	if err != nil {
		return nil, nil, err
	}
	var st sca.SCAState
	subnetAct, err := api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading actor state: %w", err)
	}
	pbs := blockstore.NewAPIBlockstore(api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &st); err != nil {
		return nil, nil, xerrors.Errorf("getting actor state: %w", err)
	}
	return &st, blockadt.WrapStore(ctx, pcst), nil
}

var _ subiface.SubnetMgr = &SubnetMgr{}
