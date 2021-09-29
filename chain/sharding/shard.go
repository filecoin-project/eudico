package sharding

import (
	"context"
	"time"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/sub"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

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
