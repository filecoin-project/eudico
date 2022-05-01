package subnetmgr

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/ipld/go-car"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	subcns "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/sub"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

// Subnet object abstracting all subneting processes and objects.
type Subnet struct {
	host host.Host
	// SubnetID
	ID address.SubnetID
	// Metadata datastore.
	ds dtypes.MetadataDS
	// Exposed blockstore
	// NOTE: We currently use the same blockstore for
	// everything in subnets, this will need to be fixed.
	bs blockstore.Blockstore
	// State manager
	sm *stmgr.StateManager
	// chain
	ch *store.ChainStore
	// Consensus type
	consType hierarchical.ConsensusType
	// Consensus of the subnet
	cons consensus.Consensus
	// Mempool for the subnet.
	mpool *messagepool.MessagePool
	// Syncer for the subnet chain
	syncer *chain.Syncer
	// Node server to register subnet servers
	nodeServer api.FullNodeServer

	// Events for subnet chain
	events *events.Events
	api    *API

	// Pubsub router from the root chain.
	pubsub *pubsub.PubSub
	// Reusing peermanager from root chain.
	pmgr peermgr.MaybePeerMgr

	// Subnet context
	ctx       context.Context
	ctxCancel context.CancelFunc

	hello *helloService

	// Mining context
	minlk      sync.Mutex
	miningCtx  context.Context
	miningCncl context.CancelFunc

	// Checkpointing signing state
	checklk      sync.RWMutex
	signingState *signingState

	// Cross-msg resolver
	r *resolver.Resolver
}

// LoadGenesis from serialized genesis bootstrap
func (sh *Subnet) LoadGenesis(ctx context.Context, genBytes []byte) (chain.Genesis, error) {
	c, err := car.LoadCar(ctx, sh.bs, bytes.NewReader(genBytes))
	if err != nil {
		return nil, xerrors.Errorf("loading genesis car file failed: %w", err)
	}
	if len(c.Roots) != 1 {
		return nil, xerrors.New("expected genesis file to have one root")
	}
	root, err := sh.bs.Get(ctx, c.Roots[0])
	if err != nil {
		return nil, err
	}

	h, err := types.DecodeBlock(root.RawData())
	if err != nil {
		return nil, xerrors.Errorf("decoding block failed: %w", err)
	}

	err = sh.ch.SetGenesis(ctx, h)
	if err != nil {
		log.Errorw("Error setting genesis for subnet", "err", err)
		return nil, err
	}
	// LoadGenesis to pass it.
	return chain.LoadGenesis(ctx, sh.sm)
}

func (sh *Subnet) HandleIncomingMessages(ctx context.Context, bootstrapper dtypes.Bootstrapper) error {
	nn := dtypes.NetworkName(sh.ID.String())
	v := sub.NewMessageValidator(sh.host.ID(), sh.mpool)

	if err := sh.pubsub.RegisterTopicValidator(build.MessagesTopic(nn), v.Validate); err != nil {
		return err
	}

	subscribe := func() {
		log.Infof("subscribing to pubsub topic %s", build.MessagesTopic(nn))

		msgsub, err := sh.pubsub.Subscribe(build.MessagesTopic(nn)) // nolint
		if err != nil {
			// TODO: We should maybe remove the panic from
			// here and return an error if we don't sync. I guess
			// we can afford an error in a subnet sync
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

// Close the subnet
//
// Stop all processes and remove all handlers.
func (sh *Subnet) Close(ctx context.Context) error {
	log.Infow("Closing subnet", "subnetID", sh.ID)
	_ = sh.stopMining(ctx)

	// Remove hello and exchange handlers to stop accepting requests from peers.
	sh.host.RemoveStreamHandler(protocol.ID(BlockSyncProtoPrefix + sh.ID.String()))
	sh.host.RemoveStreamHandler(protocol.ID(HelloProtoPrefix + sh.ID.String()))
	// Remove pubsub topic validators for the subnet.
	err1 := sh.pubsub.UnregisterTopicValidator(build.BlocksTopic(dtypes.NetworkName(sh.ID.String())))
	err2 := sh.pubsub.UnregisterTopicValidator(build.MessagesTopic(dtypes.NetworkName(sh.ID.String())))
	// Close chainstore
	err3 := sh.ch.Close()
	// Stop state manager
	err4 := sh.sm.Stop(ctx)
	// Stop syncer
	sh.syncer.Stop()
	// Close message pool
	err5 := sh.mpool.Close()
	// Close resolver.
	err6 := sh.r.Close()

	// TODO: Do we need to do something else to fully close the
	// subnet. We'll need to revisit this.
	// Check: https://github.com/filecoin-project/eudico/issues/38
	// TODO: We should maybe check also if it is worth removing the
	// chainstore for the subnet from the datastore (as it is no longer
	// needed). See: https://github.com/filecoin-project/eudico/issues/79
	sh.ctxCancel()

	return multierr.Combine(
		err1,
		err2,
		err3,
		err4,
		err5,
		err6,
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

func (sh *Subnet) HandleIncomingBlocks(ctx context.Context, bserv dtypes.ChainBlockService) error {
	nn := dtypes.NetworkName(sh.ID.String())
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

	blocksub, err := sh.pubsub.Subscribe(build.BlocksTopic(nn)) // nolint
	if err != nil {
		return err
	}

	go sub.HandleIncomingBlocks(ctx, blocksub, sh.syncer, bserv, sh.host.ConnManager())
	return nil
}

// Checks if we are mining in a subnet.
func (sh *Subnet) isMining() bool {
	sh.minlk.Lock()
	defer sh.minlk.Unlock()
	return sh.miningCtx != nil
}

func (sh *Subnet) mine(ctx context.Context, wallet address.Address) error {
	if sh.miningCtx != nil {
		log.Warnw("already mining in subnet", "subnetID", sh.ID)
		return nil
	}

	mctx, cancel := context.WithCancel(ctx)
	if err := subcns.Mine(mctx, sh.api, wallet, sh.consType); err != nil {
		cancel()
		return err
	}
	// Set context and cancel for mining if started successfully
	sh.miningCtx, sh.miningCncl = mctx, cancel
	log.Infow("Started mining in subnet", "subnetID", sh.ID, "consensus", sh.consType)

	return nil
}

func (sh *Subnet) stopMining(ctx context.Context) error {
	sh.minlk.Lock()
	defer sh.minlk.Unlock()
	if sh.miningCncl != nil {
		log.Infow("Stop mining in subnet", "subnetID", sh.ID)
		sh.miningCncl()
		sh.miningCtx = nil
		return nil
	}
	return xerrors.Errorf("Currently not mining in subnet")
}
