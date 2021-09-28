package sharding

import (
	"context"
	"reflect"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
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
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/ipfs/go-blockservice"
	ds "github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/connmgr"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

var log = logging.Logger("sharding")

// Shard object abstracting all sharding processes and objects.
type Shard struct {
	// ShardID
	ID string
	// Topic name
	topic string
	// Pubsub subcription for shard.
	sub *pubsub.Subscription
	// Metadata datastore.
	ds dtypes.MetadataDS
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

	// Events for shard chain
	events   *events.Events
	eventAPI events.EventAPI

	// State API of the root API
	// NOTE: This is only used to have access to the
	// WalletAPI within the shard, this should change in the future.
	stateAPI *full.StateAPI
	// Pubsub router from the root chain.
	pubsub *pubsub.PubSub
}

// ShardingSub is the sharding manager in the root chain
type ShardingSub struct {
	// Listener for events of the root chain.
	events *events.Events
	// This is the API for the fullNode in the root chain.
	api  api.FullNode
	self peer.ID

	pubsub *pubsub.PubSub
	// Root ds
	ds       dtypes.MetadataDS
	connmgr  connmgr.ConnManager
	exchange exchange.Client //TODO: We may need to create a new one for every shard if syncing fails
	beacon   beacon.Schedule
	syscalls vm.SyscallBuilder
	us       stmgr.UpgradeSchedule
	verifier ffiwrapper.Verifier

	lk     sync.Mutex
	shards map[string]*Shard

	// TODO: the state here is only needed because we have the MinerCreateBlock
	// API attached to the state API. It probably should live somewhere better
	stateAPI full.StateAPI

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
	verifier ffiwrapper.Verifier,
	j journal.Journal,
	stateAPI full.StateAPI) (*ShardingSub, error) {

	// Starting shardSub to listen to events in the root chain.
	e, err := events.NewEvents(context.TODO(), &api)
	if err != nil {
		return nil, err
	}

	return &ShardingSub{
		events:   e,
		api:      &api,
		pubsub:   pubsub,
		self:     self,
		ds:       ds,
		syscalls: syscalls,
		us:       us,
		j:        j,
		connmgr:  host.ConnManager(),
		verifier: verifier,
		stateAPI: stateAPI,
		shards:   make(map[string]*Shard),
	}, nil
}

// TODO: when creating a new shard we should probably add the parent
// chain as an argument so there is knowledge about which one is the
// parent chain.
func (s *ShardingSub) newShard(id string) error {
	log.Infow("Creating new shard", "shardID", id)
	sh := &Shard{
		ID:       id,
		stateAPI: &s.stateAPI,
		topic:    "/shards/" + id,
		pubsub:   s.pubsub,
	}
	s.lk.Lock()
	s.shards[id] = sh
	s.lk.Unlock()

	// join the pubsub topic for the shard
	topic, err := s.pubsub.Join(sh.topic)
	if err != nil {
		return err
	}

	// and subscribe to it
	sh.sub, err = topic.Subscribe()
	if err != nil {
		return err
	}

	// Wrap the ds with prefix
	sh.ds = nsds.Wrap(s.ds, ds.NewKey(sh.topic))
	// TODO: We should not use the metadata datastore here. We need
	// to create the corresponding blockstores. Deferring once we
	// figure out if it works.
	bs := blockstore.FromDatastore(s.ds)

	// TODO: We are currently limited to the use of delegated
	// consensus for every shard. In the next iteration the type
	// of consensus to use will be notified by the shard actor.
	exec := delegcns.TipSetExecutor()

	// TODO: Use the weight for the right consensus algorithm.
	sh.ch = store.NewChainStore(bs, bs, s.ds, delegcns.Weight, s.j)
	sh.sm, err = stmgr.NewStateManager(sh.ch, exec, s.syscalls, s.us)
	if err != nil {
		log.Errorw("Error creating state manager for shard", "shardID", id)
		return err
	}
	// TODO: Have functions to generate the right genesis for the consensus used.
	// All os this will be extracted to a consensus specific function that
	// according to the consensus selected for the shard it does the right thing.
	template, err := delegatedGenTemplate(sh.topic)
	if err != nil {
		log.Errorw("Error creating genesis template for shard", "shardID", id)
		return err
	}
	genBoot, err := MakeDelegatedGenesisBlock(context.TODO(), s.j, bs, s.syscalls, *template)
	if err != nil {
		log.Errorw("Error creating genesis block for shard", "shardID", id)
		return err
	}
	// Set consensus in new chainStore
	err = sh.ch.SetGenesis(genBoot.Genesis)
	if err != nil {
		log.Errorw("Error setting genesis for shard", "shardID", id)
		return err
	}
	//LoadGenesis to pass it
	gen, err := chain.LoadGenesis(sh.sm)
	if err != nil {
		log.Errorw("Error loading genesis for shard", "shardID", id)
		return err
	}

	sh.cons = delegcns.NewDelegatedConsensus(sh.sm, s.beacon, s.verifier, gen)

	log.Infow("Genesis and consensus for shard created", "shardID", id)

	sh.syncer, err = chain.NewSyncer(s.ds, sh.sm, s.exchange, chain.NewSyncManager, s.connmgr, s.self, s.beacon, gen, sh.cons)
	if err != nil {
		log.Errorw("Error creating syncer for shard", "shardID", id)
		return err
	}
	// Start syncer for the shard
	sh.syncer.Start()
	bserv := blockservice.New(bs, offline.Exchange(bs))

	prov := messagepool.NewProvider(sh.sm, s.pubsub)

	sh.mpool, err = messagepool.New(prov, s.ds, s.us, dtypes.NetworkName(template.NetworkName), s.j)
	if err != nil {
		log.Errorw("Error creating message pool for shard", "shardID", id)
		return err
	}
	go sub.HandleIncomingBlocks(context.TODO(), sh.sub, sh.syncer, bserv, s.connmgr)
	go sub.HandleIncomingMessages(context.TODO(), sh.mpool, sh.sub)
	log.Infow("Listening for new blocks and messages in shard", "shardID", id)

	// TODO: As-is a node will keep mining in a shard until the node process
	// is completely stopped. In the next iteration we need to figure out
	// how to manage contexts for when a shard is killed or a node moves into
	// another shard.
	// Mining in the root chain is an independent process.
	log.Infow("Started mining in shard", "shardID", id)
	go sh.mineDelegated(context.TODO(), s.api)

	// Listening to events on the shard actor from the shard chain.
	// We can create new shards from an existing one, and we need to
	// monitor that (thus the "hierarchical" in the consensus).
	err = sh.InitEvents(context.TODO())
	if err != nil {
		return err
	}
	go s.listenShardEvents(context.TODO(), sh)
	log.Infow("Listening to shard events in shard", "shardID", id)
	log.Infow("Successfully spawned shard", "shardID", id)

	return nil
}

func (s *ShardingSub) listenShardEvents(ctx context.Context, sh *Shard) {
	api := s.api.(events.EventAPI)
	evs := s.events

	// If shard is nil, we are listening from the root chain.
	if sh != nil {
		api = sh.eventAPI
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

		// Create shard and subscribe to pubsub topic
		err = s.newShard(id)
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
