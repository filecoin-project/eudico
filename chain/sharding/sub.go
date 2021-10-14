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
	shardactor "github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"github.com/ipfs/go-blockservice"
	ds "github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
)

var log = logging.Logger("sharding")

// ShardingSub is the sharding manager in the root chain
type ShardingSub struct {
	// Listener for events of the root chain.
	events *events.Events
	// This is the API for the fullNode in the root chain.
	api  *impl.FullNodeAPI
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

	lk     sync.Mutex
	shards map[string]*Shard

	j journal.Journal
}

func NewShardSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	api impl.FullNodeAPI,
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
	j journal.Journal) (*ShardingSub, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)
	// Starting shardSub to listen to events in the root chain.
	e, err := events.NewEvents(ctx, &api)
	if err != nil {
		return nil, err
	}

	return &ShardingSub{
		events:       e,
		api:          &api,
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
		shards:       make(map[string]*Shard),
	}, nil
}

func (s *ShardingSub) newShard(ctx context.Context, id string, parentAPI *impl.FullNodeAPI, info diffInfo) error {
	var err error
	ctx, cancel := context.WithCancel(ctx)

	log.Infow("Creating new shard", "shardID", id)
	sh := &Shard{
		ctx:        ctx,
		ctxCancel:  cancel,
		ID:         id,
		host:       s.host,
		netName:    "shard/" + id,
		pubsub:     s.pubsub,
		nodeServer: s.nodeServer,
		pmgr:       s.pmgr,
		consType:   info.consensus,
	}

	// Add shard to registry
	s.shards[id] = sh

	// Wrap the ds with prefix
	sh.ds = nsds.Wrap(s.ds, ds.NewKey(sh.netName))
	// TODO: We should not use the metadata datastore here. We need
	// to create the corresponding blockstores. Deferring once we
	// figure out if it works.
	sh.bs = blockstore.FromDatastore(s.ds)

	// Select the right TipSetExecutor for the consensus algorithms chosen.
	tsExec, err := tipSetExecutor(info.consensus)
	if err != nil {
		log.Errorw("Error getting TipSetExecutor for consensus", "shardID", id, "err", err)
		return err
	}
	weight, err := weight(info.consensus)
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

	gen, err := sh.LoadGenesis(info.genesis)
	if err != nil {
		log.Errorw("Error loading genesis bootstrap for shard", "shardID", id, "err", err)
		return err
	}
	sh.cons, err = newConsensus(info.consensus, sh.sm, s.beacon, s.verifier, gen)
	if err != nil {
		log.Errorw("Error creating consensus", "shardID", id, "err", err)
		return err
	}
	log.Infow("Genesis and consensus for shard created", "shardID", id, "consensus", info.consensus)

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

	sh.mpool, err = messagepool.New(prov, sh.ds, s.us, dtypes.NetworkName(sh.netName), s.j)
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

	// If is miner start mining in shard.
	if info.isMiner {
		sh.mine(ctx)
	}

	log.Infow("Successfully spawned shard", "shardID", id)

	return nil
}

func (s *ShardingSub) listenShardEvents(ctx context.Context, sh *Shard) {
	api := s.api
	evs := s.events
	id := "root"

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

		// Start running state checks to build diff.
		// Check first if there are new shards.
		if f := s.checkNewShard(ctx, cst, oldSt, newSt); f != nil {
			return f()
		}
		// Check if state in existing shards has changed.
		f, err := s.checkShardChange(ctx, cst, oldSt, newSt)
		if err != nil {
			return false, nil, err
		}
		if f != nil {
			return f()
		}

		return false, nil, nil

	}

	err := evs.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (s *ShardingSub) triggerChange(ctx context.Context, api *impl.FullNodeAPI, shardMap map[string]diffInfo) (more bool, err error) {
	s.lk.Lock()
	defer s.lk.Unlock()
	// For each new shard detected
	for k, diff := range shardMap {
		// If the shard has not been removed.
		if !diff.isRm {
			log.Infow("Change event detected for shard", "shardID", k)
			_, ok := s.shards[k]
			// If we are not already subscribed to the shard.
			if !ok {
				// Create shard with id from parentAPI
				err := s.newShard(ctx, k, api, diff)
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
			log.Infow("Leave event detected for shard", "shardID", k)
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
	return true, nil
}
func (s *ShardingSub) Start(ctx context.Context) {
	s.listenShardEvents(ctx, nil)
}

func BuildShardingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, s *ShardingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	s.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// NOTE: Closing sharding sub here. Whatever the hell that means...
			// It may be worth revisiting this.
			for _, sh := range s.shards {
				err := sh.Close(ctx)
				if err != nil {
					return err
				}
			}
			return nil
		},
	})

}
