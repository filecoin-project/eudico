package sharding

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/exchange"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/sub"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/ipfs/go-blockservice"
	ds "github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

var log = logging.Logger("sharding")

// Shard object abstracting all sharding processes and objects.
type Shard struct {
	host host.Host
	// ShardID
	ID string
	// network name for shard
	netName string
	// Pubsub subcription for shard.
	// sub *pubsub.Subscription
	// Metadata datastore.
	ds dtypes.MetadataDS
	// Exposed blockstore
	// NOTE: We currently use the same blockstore for
	// everything in shards, this will need to be fixed.
	bs blockstore.Blockstore
	// State manager
	sm *stmgr.StateManager
	// chain
	ch *store.ChainStore
	// Consensus of the shard
	cons consensus.Consensus
	// Mempool for the shard.
	mpool *messagepool.MessagePool
	// Syncer for the shard chain
	syncer *chain.Syncer
	// Node server to register shard servers
	nodeServer api.FullNodeServer

	// Events for shard chain
	events *events.Events
	api    *impl.FullNodeAPI

	// Pubsub router from the root chain.
	pubsub *pubsub.PubSub
}

// ShardingSub is the sharding manager in the root chain
type ShardingSub struct {
	// Listener for events of the root chain.
	events *events.Events
	// This is the API for the fullNode in the root chain.
	api  *impl.FullNodeAPI
	host host.Host

	pubsub *pubsub.PubSub
	// Root ds
	ds         dtypes.MetadataDS
	exchange   exchange.Client //TODO: We may need to create a new one for every shard if syncing fails
	beacon     beacon.Schedule
	syscalls   vm.SyscallBuilder
	us         stmgr.UpgradeSchedule
	verifier   ffiwrapper.Verifier
	nodeServer api.FullNodeServer

	lk     sync.Mutex
	shards map[string]*Shard

	j journal.Journal
}

func NewShardSub(
	api impl.FullNodeAPI,
	self peer.ID,
	pubsub *pubsub.PubSub,
	ds dtypes.MetadataDS,
	host host.Host,
	exchange exchange.Client,
	beacon beacon.Schedule,
	syscalls vm.SyscallBuilder,
	us stmgr.UpgradeSchedule,
	nodeServer api.FullNodeServer,
	verifier ffiwrapper.Verifier,
	j journal.Journal) (*ShardingSub, error) {

	// Starting shardSub to listen to events in the root chain.
	e, err := events.NewEvents(context.TODO(), &api)
	if err != nil {
		return nil, err
	}

	return &ShardingSub{
		events:     e,
		api:        &api,
		pubsub:     pubsub,
		host:       host,
		exchange:   exchange,
		ds:         ds,
		syscalls:   syscalls,
		us:         us,
		j:          j,
		nodeServer: nodeServer,
		verifier:   verifier,
		shards:     make(map[string]*Shard),
	}, nil
}

func (s *ShardingSub) newShard(id string, parentAPI *impl.FullNodeAPI) error {
	var err error
	ctx := context.TODO()
	log.Infow("Creating new shard", "shardID", id)
	sh := &Shard{
		ID:         id,
		host:       s.host,
		netName:    "shard/" + id,
		pubsub:     s.pubsub,
		nodeServer: s.nodeServer,
	}
	s.lk.Lock()
	s.shards[id] = sh
	s.lk.Unlock()

	// Wrap the ds with prefix
	sh.ds = nsds.Wrap(s.ds, ds.NewKey(sh.netName))
	// TODO: We should not use the metadata datastore here. We need
	// to create the corresponding blockstores. Deferring once we
	// figure out if it works.
	sh.bs = blockstore.FromDatastore(s.ds)

	// TODO: We are currently limited to the use of delegated
	// consensus for every shard. In the next iteration the type
	// of consensus to use will be notified by the shard actor.
	tsExec := delegcns.TipSetExecutor()

	// TODO: Use the weight for the right consensus algorithm.
	sh.ch = store.NewChainStore(sh.bs, sh.bs, s.ds, delegcns.Weight, s.j)
	sh.sm, err = stmgr.NewStateManager(sh.ch, tsExec, s.syscalls, s.us)
	if err != nil {
		log.Errorw("Error creating state manager for shard", "shardID", id, "err", err)
		return err
	}
	// TODO: Have functions to generate the right genesis for the consensus used.
	// All os this will be extracted to a consensus specific function that
	// according to the consensus selected for the shard it does the right thing.
	template, err := delegatedGenTemplate(sh.netName)
	if err != nil {
		log.Errorw("Error creating genesis template for shard", "shardID", id, "err", err)
		return err
	}
	genBoot, err := MakeDelegatedGenesisBlock(ctx, s.j, sh.bs, s.syscalls, *template)
	if err != nil {
		log.Errorw("Error creating genesis block for shard", "shardID", id, "err", err)
		return err
	}
	// Set consensus in new chainStore
	err = sh.ch.SetGenesis(genBoot.Genesis)
	if err != nil {
		log.Errorw("Error setting genesis for shard", "shardID", id, "err", err)
		return err
	}
	//LoadGenesis to pass it
	gen, err := chain.LoadGenesis(sh.sm)
	if err != nil {
		log.Errorw("Error loading genesis for shard", "shardID", id, "err", err)
		return err
	}

	sh.cons = delegcns.NewDelegatedConsensus(sh.sm, s.beacon, s.verifier, gen)

	log.Infow("Genesis and consensus for shard created", "shardID", id)

	// TODO: We are using here for the same exchange of the root chain for the syncer.
	// We may need to consider adding a new syncer for each shard.
	// check exchange.NewClient() for this initialization.
	sh.syncer, err = chain.NewSyncer(s.ds, sh.sm, s.exchange, chain.NewSyncManager, s.host.ConnManager(), s.host.ID(), s.beacon, gen, sh.cons)
	if err != nil {
		log.Errorw("Error creating syncer for shard", "shardID", id, "err", err)
		return err
	}
	// Start syncer for the shard
	sh.syncer.Start()
	bserv := blockservice.New(sh.bs, offline.Exchange(sh.bs))

	prov := messagepool.NewProvider(sh.sm, s.pubsub)

	sh.mpool, err = messagepool.New(prov, s.ds, s.us, dtypes.NetworkName(template.NetworkName), s.j)
	if err != nil {
		log.Errorw("Error creating message pool for shard", "shardID", id, "err", err)
		return err
	}
	log.Infow("Populating and registering API for", "shardID", id)
	err = sh.populateAPIs(parentAPI, s.host, tsExec)
	if err != nil {
		log.Errorw("Error populating APIs for shard", "shardID", id, "err", err)
		return err
	}

	// This functions create a new pubsub topic for the shard to start
	// listening to new messages and blocks for the shard.
	err = sh.HandleIncomingMessages(ctx)
	if err != nil {
		log.Errorw("HandleIncomingMessages failed for shard", "shardID", id, "err", err)
		return err
	}
	err = sh.HandleIncomingBlocks(ctx, bserv)
	if err != nil {
		log.Errorw("HandleIncomingBlocks failed for shard", "shardID", id, "err", err)
		return err
	}
	log.Infow("Listening for new blocks and messages in shard", "shardID", id)

	// TODO: As-is a node will keep mining in a shard until the node process
	// is completely stopped. In the next iteration we need to figure out
	// how to manage contexts for when a shard is killed or a node moves into
	// another shard.
	// Mining in the root chain is an independent process.
	log.Infow("Started mining in shard", "shardID", id)
	go sh.mineDelegated(ctx)

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
	log.Infow("Successfully spawned shard", "shardID", id)

	return nil
}

func (sh *Shard) HandleIncomingMessages(ctx context.Context) error {
	nn := dtypes.NetworkName(sh.netName)
	v := sub.NewMessageValidator(sh.host.ID(), sh.mpool)

	if err := sh.pubsub.RegisterTopicValidator(build.MessagesTopic(nn), v.Validate); err != nil {
		return err
	}

	subscribe := func() {
		log.Infof("subscribing to pubsub topic %s", build.MessagesTopic(nn))

		msgsub, err := sh.pubsub.Subscribe(build.MessagesTopic(nn)) //nolint
		if err != nil {
			// TODO: We should maybe remove the panic from
			// here and return an error if we don't sync. I guess
			// we can afford an error in a shard sync
			panic(err)
		}

		go sub.HandleIncomingMessages(ctx, sh.mpool, msgsub)
	}

	// wait until we are synced within 10 epochs -- env var can override
	waitForSync(sh.sm, 10, subscribe)
	return nil
}

func waitForSync(stmgr *stmgr.StateManager, epochs int, subscribe func()) {
	nearsync := time.Duration(epochs*int(build.BlockDelaySecs)) * time.Second

	// early check, are we synced at start up?
	ts := stmgr.ChainStore().GetHeaviestTipSet()
	timestamp := ts.MinTimestamp()
	timestampTime := time.Unix(int64(timestamp), 0)
	if build.Clock.Since(timestampTime) < nearsync {
		subscribe()
		return
	}

	// we are not synced, subscribe to head changes and wait for sync
	stmgr.ChainStore().SubscribeHeadChanges(func(rev, app []*types.TipSet) error {
		if len(app) == 0 {
			return nil
		}

		latest := app[0].MinTimestamp()
		for _, ts := range app[1:] {
			timestamp := ts.MinTimestamp()
			if timestamp > latest {
				latest = timestamp
			}
		}

		latestTime := time.Unix(int64(latest), 0)
		if build.Clock.Since(latestTime) < nearsync {
			subscribe()
			return store.ErrNotifeeDone
		}

		return nil
	})
}

func (sh *Shard) HandleIncomingBlocks(ctx context.Context, bserv dtypes.ChainBlockService) error {
	nn := dtypes.NetworkName(sh.netName)
	v := sub.NewBlockValidator(
		sh.host.ID(), sh.ch, sh.cons,
		func(p peer.ID) {
			sh.pubsub.BlacklistPeer(p)
			sh.host.ConnManager().TagPeer(p, "badblock", -1000)
		})

	if err := sh.pubsub.RegisterTopicValidator(build.BlocksTopic(nn), v.Validate); err != nil {
		return err
	}

	log.Infof("subscribing to pubsub topic %s", build.BlocksTopic(nn))

	blocksub, err := sh.pubsub.Subscribe(build.BlocksTopic(nn)) //nolint
	if err != nil {
		return err
	}

	go sub.HandleIncomingBlocks(ctx, blocksub, sh.syncer, bserv, sh.host.ConnManager())
	return nil
}

func (s *ShardingSub) listenShardEvents(ctx context.Context, sh *Shard) {
	api := s.api
	evs := s.events

	// If shard is nil, we are listening from the root chain.
	if sh != nil {
		api = sh.api
		evs = sh.events
	}

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		if sh != nil {
			log.Infow("State change detected for shard actor in shard", "shardID", sh.ID)
		} else {
			log.Infow("State change detected for shard actor in shard", "shardID", "root")
		}

		// Add the new shard to the struct.
		shardMap, ok := states.(map[string]struct{})
		if !ok {
			log.Error("Error casting shardMap")
			return true, err
		}
		id := ""
		for k := range shardMap {
			// I just want the first one from the map. Once
			// we have the logic for the actor this must change.
			id = string(k)
			break
		}

		// Create shard with id from parentAPI
		err = s.newShard(id, api)
		if err != nil {
			log.Errorw("Error creating new shard", "shardID", id, "err", err)
			return true, err
		}

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		oldAct, err := api.StateGetActor(ctx, delegcns.ShardActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}
		newAct, err := api.StateGetActor(ctx, delegcns.ShardActorAddr, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		var oldSt, newSt delegcns.ShardState

		bs := blockstore.NewAPIBlockstore(s.api)
		cst := cbor.NewCborStore(bs)
		if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
			return false, nil, err
		}
		if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
			return false, nil, err
		}

		if reflect.DeepEqual(newSt, oldSt) {
			return false, nil, nil
		}

		// TODO: Add logic to decide if to run a new shard or not
		// according to state in actor.
		// TODO: Shard id is received as bytes, we should use CIDs for
		// shard IDs.
		diff := map[string]struct{}{}
		for _, shard := range newSt.Shards {
			diff[string(shard)] = struct{}{}
		}
		for _, shard := range oldSt.Shards {
			delete(diff, string(shard))
		}
		return true, diff, nil
	}

	err := evs.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}
func (s *ShardingSub) Start() {
	// TODO: Figure out contexts all around the place.
	s.listenShardEvents(context.TODO(), nil)
}
