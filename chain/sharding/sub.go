package sharding

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
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
	"github.com/libp2p/go-libp2p-core/connmgr"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

var log = logging.Logger("sharding")

type Shard struct {
	ID  string
	sub *pubsub.Subscription
	ds  dtypes.MetadataDS
	sm  *stmgr.StateManager
}

type ShardingSub struct {
	events *events.Events
	api    api.FullNode
	self   peer.ID

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

<<<<<<< HEAD
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
	j journal.Journal) (*ShardingSub, error) {

=======
	host host.Host
}

func NewShardSub(api impl.FullNodeAPI, host host.Host) (*ShardingSub, error) {
>>>>>>> 1ef906acc027f69bd748008fa46f0b487e12c86a
	e, err := events.NewEvents(context.TODO(), &api)
	if err != nil {
		return nil, err
	}

	// TODO: Verify that
	return &ShardingSub{
<<<<<<< HEAD
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
		shards:   make(map[string]*Shard),
=======
		events: e,
		api:    &api,
		host:   host,
>>>>>>> 1ef906acc027f69bd748008fa46f0b487e12c86a
	}, nil
}

func (s *ShardingSub) newShard(id string) error {
	shid := "/shards/" + id
	// Create shard struct
	sh := &Shard{ID: id}
	s.lk.Lock()
	s.shards[id] = sh
	s.lk.Unlock()

	// join the pubsub topic for the shard
	topic, err := s.pubsub.Join(shid)
	if err != nil {
		return err
	}

	// and subscribe to it
	sh.sub, err = topic.Subscribe()
	if err != nil {
		return err
	}

	// Wrap the ds with prefix
	sh.ds = nsds.Wrap(s.ds, ds.NewKey(shid))
	// TODO: We should not use the metadata datstore here. We need
	// to create the corresponding blockstores. Deferring once we
	// figure out if it works.
	bs := blockstore.FromDatastore(s.ds)

	// TODO: Using delegated consensus executor and weight but we
	// should use the one for the consensus we are actually launching.
	exec := delegcns.TipSetExecutor()

	// TODO: We should use the right consensus weight and not delegcns weight
	ch := store.NewChainStore(bs, bs, s.ds, delegcns.Weight, s.j)
	sh.sm, err = stmgr.NewStateManager(ch, exec, s.syscalls, s.us)
	if err != nil {
		return err
	}
	template, err := delegatedGenTemplate(shid)
	if err != nil {
		return err
	}
	genBoot, err := MakeDelegatedGenesisBlock(context.TODO(), s.j, bs, s.syscalls, *template)
	if err != nil {
		return err
	}
	// Set consensus in new chainStore
	err = ch.SetGenesis(genBoot.Genesis)
	if err != nil {
		return err
	}
	//LoadGenesis to pass it
	gen, err := chain.LoadGenesis(sh.sm)
	if err != nil {
		return err
	}

	// TODO: This is the genesis bootstrap but I need to pass the actual genesis.
	cons := delegcns.NewDelegatedConsensus(sh.sm, s.beacon, s.verifier, gen)

	// NOTE: Check if the self works here.
	syncer, err := chain.NewSyncer(s.ds, sh.sm, s.exchange, chain.NewSyncManager, s.connmgr, s.self, s.beacon, gen, cons)
	// Start syncer for the shard
	syncer.Start()
	bserv := blockservice.New(bs, offline.Exchange(bs))
	// WIP: Generate APIs for shard and get the provider.
	prov := messagepool.NewProvider(sh.sm, s.pubsub)

	mpool, err := messagepool.New(prov, s.ds, s.us, dtypes.NetworkName(template.NetworkName), s.j)
	go sub.HandleIncomingBlocks(context.TODO(), sh.sub, syncer, bserv, s.connmgr)
	go sub.HandleIncomingMessages(context.TODO(), mpool, sh.sub)

	// TODO: Mining process here so we keep mining to the shard.
	/*
					stmgr, err := stmgr.NewStateManager(cs *store.ChainStore, exec stmgr.Executor, sys vm.SyscallBuilder, us UpgradeSchedule)

			   		// Create chainstore
			   		// --- > Get the datastore and wrap it in a namespace.
			   		// New state manager for the shard.
			   		// New exchange client

			   		syncer, err := sync.NewSyncer(ds dtypes.MetadataDS,
			   	sm *stmgr.StateManager,
			   	exchange exchange.Client,
			   	syncMgrCtor SyncManagerCtor,
			   	self peer.ID,
			   	beacon beacon.Schedule,
			   	gent Genesis,
			   	consensus consensus.Consensus) (*Syncer, error) {
			   		syncer.Start()
			   		// Handleincmingblocks in imcoming.go
			   		sub.HandleIncomingBlocks(ctx, sub, s, bs, cmgr)
			   		// New services to mine in the shard
			   		// Sync api to send the blocks
			   		// In the shard actor we need to store the power table requirede to
			   		// create deterministically the genesis block for every shards when someone
			   		// new joins.
			   		// New API to interact with shards.


		// Pending:
			- The API for the shard
			- How to keep mining in the shard (we will still be mining in the top)
	*/
	return nil
}

func (s *ShardingSub) Start() {
	ctx := context.TODO()

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		fmt.Println(fmt.Sprintf("STATE CHANGE %#v", states))
		// Add the new shard to the struct.
		id, ok := states.(string)
		if !ok {
			return true, err
		}

		// Create shard and subscribe to pubsub topic
		err = s.newShard(id)
		if err != nil {
			return true, err
		}

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		oldAct, err := s.api.StateGetActor(ctx, delegcns.ShardActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}
		newAct, err := s.api.StateGetActor(ctx, delegcns.ShardActorAddr, newTs.Key())
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

		diff := map[string]struct{}{}
		for _, shard := range newSt.Shards {
			diff[string(shard)] = struct{}{}
		}
		for _, shard := range oldSt.Shards {
			delete(diff, string(shard))
		}
		return true, diff, nil
	}

	err := s.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}
