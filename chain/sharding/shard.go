package sharding

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	filcnsminer "github.com/filecoin-project/lotus/chain/consensus/filcns/miner"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/sub"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/ipld/go-car"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"
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
	// Consensus type
	consType shard.ConsensusType
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
	// Reusing peermanager from root chain.
	pmgr peermgr.MaybePeerMgr

	// Shard context
	ctx       context.Context
	ctxCancel context.CancelFunc

	hello *helloService

	// Mining context
	minlk      sync.Mutex
	miningCtx  context.Context
	miningCncl context.CancelFunc
}

// LoadGenesis from serialized genesis bootstrap
func (sh *Shard) LoadGenesis(genBytes []byte) (chain.Genesis, error) {
	c, err := car.LoadCar(sh.bs, bytes.NewReader(genBytes))
	if err != nil {
		return nil, xerrors.Errorf("loading genesis car file failed: %w", err)
	}
	if len(c.Roots) != 1 {
		return nil, xerrors.New("expected genesis file to have one root")
	}
	root, err := sh.bs.Get(c.Roots[0])
	if err != nil {
		return nil, err
	}

	h, err := types.DecodeBlock(root.RawData())
	if err != nil {
		return nil, xerrors.Errorf("decoding block failed: %w", err)
	}

	err = sh.ch.SetGenesis(h)
	if err != nil {
		log.Errorw("Error setting genesis for shard", "err", err)
		return nil, err
	}
	//LoadGenesis to pass it
	return chain.LoadGenesis(sh.sm)
}

func (sh *Shard) HandleIncomingMessages(ctx context.Context, bootstrapper dtypes.Bootstrapper) error {
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

	if bootstrapper {
		subscribe()
		return nil
	}

	// wait until we are synced within 10 epochs -- env var can override
	waitForSync(sh.sm, 10, subscribe)
	return nil
}

// Close the shard
//
// Stop all processes and remove all handlers.
func (sh *Shard) Close(ctx context.Context) error {
	log.Infow("Closing shard", "shardID", sh.ID)
	// Remove hello and exchange handlers to stop accepting requests from peers.
	sh.host.RemoveStreamHandler(protocol.ID(BlockSyncProtoPrefix + sh.netName))
	sh.host.RemoveStreamHandler(protocol.ID(HelloProtoPrefix + sh.netName))
	// Remove pubsub topic validators for the shard.
	err1 := sh.pubsub.UnregisterTopicValidator(build.BlocksTopic(dtypes.NetworkName(sh.netName)))
	err2 := sh.pubsub.UnregisterTopicValidator(build.MessagesTopic(dtypes.NetworkName(sh.netName)))
	// Close chainstore
	err3 := sh.ch.Close()
	// Stop state manager
	err4 := sh.sm.Stop(ctx)
	// Stop syncer
	sh.syncer.Stop()
	// Close message pool
	err5 := sh.mpool.Close()

	// TODO: Do we need to do something else to fully close the
	// shard. We'll need to revisit this.
	// Check: https://github.com/filecoin-project/eudico/issues/38
	// TODO: We should maybe check also if it is worth removing the
	// chainstore for the shard from the datastore (as it is no longer
	// needed).
	sh.ctxCancel()

	return multierr.Combine(
		err1,
		err2,
		err3,
		err4,
		err5,
	)
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

// Checks if we are mining in a shard.
func (sh *Shard) isMining() bool {
	sh.minlk.Lock()
	defer sh.minlk.Unlock()
	return sh.miningCtx != nil
}

func (sh *Shard) mine(ctx context.Context) error {
	if sh.miningCtx != nil {
		log.Warnw("already mining in shard", "shardID", sh.ID)
		return nil
	}
	// TODO: As-is a node will keep mining in a shard until the node process
	// is completely stopped. In the next iteration we need to figure out
	// how to manage contexts for when a shard is killed or a node moves into
	// another shard. (see next function)
	// Mining in the root chain is an independent process.
	// TODO: We should check if these processes throw an error
	switch sh.consType {
	case shard.Delegated:
		// Assigning mining context.
		sh.miningCtx, sh.miningCncl = context.WithCancel(ctx)
		go delegcns.Mine(sh.miningCtx, sh.api)
	case shard.PoW:
		miner, err := sh.getWallet(ctx)
		if err != nil {
			log.Errorw("no valid identity found for PoW mining", "err", err)
			return err
		}
		sh.miningCtx, sh.miningCncl = context.WithCancel(ctx)
		go tspow.Mine(sh.miningCtx, miner, sh.api)
	case shard.FilCns:

		// TODO: We should make this configurable.
		minerID := "t01000"
		miner, err := address.NewFromString(minerID)
		if err != nil {
			return xerrors.Errorf("parsing miner address: %w", err)
		}
		if miner.Protocol() != address.ID {
			return xerrors.Errorf("must be miner ID (t0x) address")
		}
		ssize := filcnsminer.DefaultPreSealSectorSize
		isGenesis := true
		// TODO: This needs to be the same one configured in the shard-actor.
		genPath := "/tmp/" + sh.ID + "/" + sh.ID
		psPaths := []string{genPath}
		psMeta := filepath.Join(genPath, "pre-seal-"+miner.String()+".json")
		mopts := filcnsminer.NewOpts(miner.String(), genPath, ssize, psPaths, psMeta, isGenesis)
		go filcnsminer.Mine(ctx, miner, sh.api, mopts)
	default:
		return xerrors.New("consensus type not suported")
	}
	log.Infow("Started mining in shard", "shardID", sh.ID, "consensus", sh.consType)
	return nil
}

func (sh *Shard) stopMining(ctx context.Context) {
	sh.minlk.Lock()
	defer sh.minlk.Unlock()
	if sh.miningCncl != nil {
		log.Infow("Stop mining in shard", "shardID", sh.ID)
		sh.miningCncl()
	}
}

// Get an identity from the peer's wallet.
// First check if a default identity has been set and
// if not take the first from the list.
// NOTE: We should probably make this configurable.
func (sh *Shard) getWallet(ctx context.Context) (address.Address, error) {
	addr, err := sh.api.WalletDefaultAddress(ctx)
	// If no defualt wallet set
	if err != nil || addr == address.Undef {
		addrs, err := sh.api.WalletList(ctx)
		if err != nil {
			return address.Undef, err
		}
		if len(addrs) == 0 {
			return address.Undef, xerrors.Errorf("no valid wallet found in peer")
		}
		addr = addrs[0]
	}
	return addr, nil
}
