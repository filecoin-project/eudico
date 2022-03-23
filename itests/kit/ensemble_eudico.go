package kit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	libp2pcrypto "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	snmgr "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/manager"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/cmd/lotus-seed/seed"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/mock"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/mockstorage"
)

const (
	TSPoWConsensusGenesisTestFile = "../testdata/tspow.gen"

	DelegatedConsensusGenesisTestFile = "../testdata/delegcns.gen"
	DelegatedConsensusKeyFile         = "../testdata/f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key"

	TendermintConsensusGenesisTestFile = "../testdata/tendermint.gen"
	TendermintConsensusTestDir         = "../testdata/tendermint-test"
	TendermintConsensusKeyFile         = TendermintConsensusTestDir + "/config/priv_validator_key.json"
	TendermintApplicationAddress       = "tcp://127.0.0.1:26658"

	//TODO: do we need this?
	FilcnsConsensusGenesisTestFile = "../testdata/filcns.gen"
)

func init() {
	chain.BootstrapPeerThreshold = 1
	messagepool.HeadChangeCoalesceMinDelay = time.Microsecond
	messagepool.HeadChangeCoalesceMaxDelay = 2 * time.Microsecond
	messagepool.HeadChangeCoalesceMergeInterval = 100 * time.Nanosecond
}

// EudicoEnsemble is a collection of nodes instantiated within a test.
//
// Create a new ensemble with:
//
//   ens := kit.NewEudicoEnsemble()
//
// Create full nodes and miners:
//
//   var full TestFullNode
//   var miner TestMiner
//   ens.FullNode(&full, opts...)       // populates a full node
//   ens.Miner(&miner, &full, opts...)  // populates a miner, using the full node as its chain daemon
//
// It is possible to pass functional options to set initial balances,
// presealed sectors, owner keys, etc.
//
// After the initial nodes are added, call `ens.Start()` to forge genesis
// and start the network. Mining will NOT be started automatically. It needs
// to be started explicitly by calling `BeginMining`.
//
// Nodes also need to be connected with one another, either via `ens.Connect()`
// or `ens.InterconnectAll()`. A common inchantation for simple tests is to do:
//
//   ens.InterconnectAll().BeginMining(blocktime)
//
// You can continue to add more nodes, but you must always follow with
// `ens.Start()` to activate the new nodes.
//
// The API is chainable, so it's possible to do a lot in a very succinct way:
//
//   kit.NewEnsemble().FullNode(&full).Miner(&miner, &full).Start().InterconnectAll().BeginMining()
//
// You can also find convenient fullnode:miner presets, such as 1:1, 1:2,
// and 2:1, e.g.:
//
//   kit.EnsembleMinimal()
//   kit.EnsembleOneTwo()
//   kit.EnsembleTwoOne()
//
type EudicoEnsemble struct {
	t            *testing.T
	bootstrapped bool
	genesisBlock bytes.Buffer
	mn           mocknet.Mocknet
	options      *ensembleOpts

	inactive struct {
		fullnodes []*TestFullNode
		miners    []*TestMiner
	}
	active struct {
		fullnodes []*TestFullNode
		miners    []*TestMiner
		bms       map[*TestMiner]*BlockMiner
	}
	genesis struct {
		version  network.Version
		miners   []genesis.Miner
		accounts []genesis.Actor
	}
}

func EudicoRootConsensusMiner(t *testing.T, opts ...interface{}) (
	rootMiner func(ctx context.Context, addr address.Address, api v1api.FullNode) error,
	subnetMinerType hierarchical.ConsensusType) {

	eopts, _ := siftOptions(t, opts)

	options := DefaultEnsembleOpts
	for _, o := range eopts {
		err := o(&options)
		require.NoError(t, err)
	}

	switch options.rootConsensus {
	case hierarchical.Tendermint:
		rootMiner = tendermint.Mine
	case hierarchical.PoW:
		rootMiner = tspow.Mine
	case hierarchical.Delegated:
		rootMiner = delegcns.Mine
	}

	switch options.subnetConsensus {
	case hierarchical.Tendermint:
		subnetMinerType = hierarchical.Tendermint
	case hierarchical.PoW:
		subnetMinerType = hierarchical.PoW
	case hierarchical.Delegated:
		subnetMinerType = hierarchical.Delegated
	}

	return
}

// NewEudicoEnsemble instantiates a new blank EudicoEnsemble.
func NewEudicoEnsemble(t *testing.T, opts ...EnsembleOpt) *EudicoEnsemble {
	options := DefaultEnsembleOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(t, err)
	}

	n := &EudicoEnsemble{t: t, options: &options}
	n.active.bms = make(map[*TestMiner]*BlockMiner)

	for _, up := range options.upgradeSchedule {
		if up.Height < 0 {
			n.genesis.version = up.Network
		}
	}

	// add accounts from ensemble options to genesis.
	for _, acc := range options.accounts {
		n.genesis.accounts = append(n.genesis.accounts, genesis.Actor{
			Type:    genesis.TAccount,
			Balance: acc.initialBalance,
			Meta:    (&genesis.AccountMeta{Owner: acc.key.Address}).ActorMeta(),
		})
	}

	return n
}

// FullNode enrolls a new full node.
func (n *EudicoEnsemble) FullNode(full *TestFullNode, opts ...NodeOpt) *EudicoEnsemble {
	options := DefaultNodeOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(n.t, err)
	}

	key, err := wallet.GenerateKey(types.KTSecp256k1)
	require.NoError(n.t, err)

	if !n.bootstrapped && !options.balance.IsZero() {
		// if we still haven't forged genesis, create a key+address, and assign
		// it some FIL; this will be set as the default wallet when the node is
		// started.
		genacc := genesis.Actor{
			Type:    genesis.TAccount,
			Balance: options.balance,
			Meta:    (&genesis.AccountMeta{Owner: key.Address}).ActorMeta(),
		}

		n.genesis.accounts = append(n.genesis.accounts, genacc)
	}

	*full = TestFullNode{t: n.t, options: options, DefaultKey: key}
	n.inactive.fullnodes = append(n.inactive.fullnodes, full)
	return n
}

// Miner enrolls a new miner, using the provided full node for chain
// interactions.
func (n *EudicoEnsemble) Miner(minerNode *TestMiner, full *TestFullNode, opts ...NodeOpt) *EudicoEnsemble {
	require.NotNil(n.t, full, "full node required when instantiating miner")

	options := DefaultNodeOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(n.t, err)
	}

	privkey, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	require.NoError(n.t, err)

	peerId, err := peer.IDFromPrivateKey(privkey)
	require.NoError(n.t, err)

	tdir, err := ioutil.TempDir("", "preseal-memgen")
	require.NoError(n.t, err)

	minerCnt := len(n.inactive.miners) + len(n.active.miners)

	actorAddr, err := address.NewIDAddress(genesis2.MinerStart + uint64(minerCnt))
	require.NoError(n.t, err)

	if options.mainMiner != nil {
		actorAddr = options.mainMiner.ActorAddr
	}

	ownerKey := options.ownerKey

	if !n.bootstrapped {
		var (
			sectors = options.sectors
			k       *types.KeyInfo
			genm    *genesis.Miner
		)

		// Will use 2KiB sectors by default (default value of sectorSize).
		proofType, err := miner.SealProofTypeFromSectorSize(options.sectorSize, n.genesis.version)
		require.NoError(n.t, err)

		// Create the preseal commitment.

		if n.options.mockProofs {
			genm, k, err = mockstorage.PreSeal(proofType, actorAddr, sectors)
		} else {
			genm, k, err = seed.PreSeal(actorAddr, proofType, 0, sectors, tdir, []byte("make genesis mem random"), nil, true)
		}
		require.NoError(n.t, err)

		genm.PeerId = peerId
		_ = k

		// create an owner key, and assign it some FIL.
		ownerKey, err = wallet.GenerateKey(types.KTSecp256k1)

		genacc := genesis.Actor{
			Type:    genesis.TAccount,
			Balance: options.balance,
			Meta:    (&genesis.AccountMeta{Owner: ownerKey.Address}).ActorMeta(),
		}

		n.genesis.miners = append(n.genesis.miners, *genm)
		n.genesis.accounts = append(n.genesis.accounts, genacc)
	} else {
		require.NotNil(n.t, ownerKey, "worker key can't be null if initializing a miner after genesis")
	}

	rl, err := net.Listen("tcp", "127.0.0.1:")
	require.NoError(n.t, err)

	*minerNode = TestMiner{
		t:              n.t,
		ActorAddr:      actorAddr,
		OwnerKey:       ownerKey,
		FullNode:       full,
		PresealDir:     tdir,
		options:        options,
		RemoteListener: rl,
	}

	minerNode.Libp2p.PeerID = peerId
	minerNode.Libp2p.PrivKey = privkey

	n.inactive.miners = append(n.inactive.miners, minerNode)

	return n
}

func NewRootTSPoWConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return tspow.NewTSPoWConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

func NewRootDelegatedConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return delegcns.NewDelegatedConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

func NewRootTendermintConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return tendermint.NewConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

func NewFilecoinExpectedConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return filcns.NewFilecoinExpectedConsensus(ctx, sm, beacon, r, verifier, genesis)
}

func NetworkName(mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	cs *store.ChainStore,
	tsexec stmgr.Executor,
	syscalls vm.SyscallBuilder,
	us stmgr.UpgradeSchedule,
	_ dtypes.AfterGenesisSet) (dtypes.NetworkName, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)

	// The statemanager is initialized here only get the network name
	// so we can use a nil resolver
	sm, err := stmgr.NewStateManager(cs, tsexec, nil, syscalls, us, nil)
	if err != nil {
		return "", err
	}

	netName, err := stmgr.GetNetworkName(ctx, sm, cs.GetHeaviestTipSet().ParentState())
	return netName, err
}

// Start starts all enrolled nodes.
func (n *EudicoEnsemble) Start() *EudicoEnsemble {
	ctx := context.Background()

	var gtempl *genesis.Template
	if !n.bootstrapped {
		// We haven't been bootstrapped yet, we need to generate genesis and
		// create the networking backbone.
		gtempl = n.generateGenesis()
		n.mn = mocknet.New()
	}

	serverOptions := make([]jsonrpc.ServerOption, 0)
	globalMux := mux.NewRouter()
	subnetMux := mux.NewRouter()
	globalMux.NewRoute().PathPrefix("/subnet/").Handler(subnetMux)

	var err error
	serveNamedApi := func(p string, iapi api.FullNode) error {
		pp := path.Join("/subnet/", p+"/")

		var h http.Handler
		// If this is a full node API
		api, ok := iapi.(*impl.FullNodeAPI)
		if ok {
			// Instantiate the full node handler.
			h, err = node.FullNodeHandler(pp, api, true, serverOptions...)
			if err != nil {
				return fmt.Errorf("failed to instantiate rpc handler: %s", err)
			}
		} else {
			// If not instantiate a subnet api
			api, ok := iapi.(*snmgr.API)
			if !ok {
				return xerrors.Errorf("Couldn't instantiate new subnet API. Something went wrong: %s", err)
			}
			// Instantiate the full node handler.
			h, err = snmgr.FullNodeHandler(pp, api, true, serverOptions...)
			if err != nil {
				return fmt.Errorf("failed to instantiate rpc handler: %s", err)
			}
		}
		fmt.Println("[*] serve new subnet API", pp)
		subnetMux.NewRoute().PathPrefix(pp).Handler(h)
		return nil
	}

	var consensusConstructor interface{}
	switch n.options.rootConsensus {
	case hierarchical.PoW:
		consensusConstructor = NewRootTSPoWConsensus
	case hierarchical.Delegated:
		consensusConstructor = NewRootDelegatedConsensus
	case hierarchical.Tendermint:
		consensusConstructor = NewRootTendermintConsensus
	case hierarchical.FilecoinEC:
		consensusConstructor = NewFilecoinExpectedConsensus
	default:
		n.t.Fatalf("unknown consensus constructor %d", n.options.rootConsensus)
	}

	var weightConstructor interface{}
	switch n.options.rootConsensus {
	case hierarchical.PoW:
		weightConstructor = tspow.Weight
	case hierarchical.Delegated:
		weightConstructor = delegcns.Weight
	case hierarchical.Tendermint:
		weightConstructor = tendermint.Weight
	case hierarchical.FilecoinEC:
		weightConstructor = filcns.Weight
	default:
		n.t.Fatalf("unknown consensus weight %d", n.options.rootConsensus)
	}

	// ---------------------
	//  FULL NODES
	// ---------------------

	// Create all inactive full nodes.
	for i, full := range n.inactive.fullnodes {
		r := repo.NewMemory(nil)
		opts := []node.Option{
			node.FullAPI(&full.FullNode, node.Lite(full.options.lite)),

			node.Base(),
			node.Repo(r),

			node.MockHost(n.mn),
			node.Test(),

			node.Override(new(dtypes.NetworkName), NetworkName),
			node.Override(new(consensus.Consensus), consensusConstructor),
			node.Override(new(store.WeightFunc), weightConstructor),
			node.Unset(new(*slashfilter.SlashFilter)),
			node.Override(new(stmgr.Executor), common.RootTipSetExecutor),
			node.Override(new(stmgr.UpgradeSchedule), common.DefaultUpgradeSchedule()),

			// so that we subscribe to pubsub topics immediately
			node.Override(new(dtypes.Bootstrapper), dtypes.Bootstrapper(true)),

			node.Override(new(api.FullNodeServer), serveNamedApi),

			node.Override(node.SetApiEndpointKey, func(lr repo.LockedRepo) error {
				apima, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/1234")
				if err != nil {
					return err
				}
				return lr.SetAPIEndpoint(apima)
			}),
			node.Unset(node.RunPeerMgrKey),
			node.Unset(new(*peermgr.PeerMgr)),
		}

		// append any node builder options.
		opts = append(opts, full.options.extraNodeOpts...)

		var genBytes []byte
		var testDataFileErr error
		switch n.options.rootConsensus {
		case hierarchical.PoW:
			genBytes, testDataFileErr = ioutil.ReadFile(TSPoWConsensusGenesisTestFile)
		case hierarchical.Delegated:
			genBytes, testDataFileErr = ioutil.ReadFile(DelegatedConsensusGenesisTestFile)
		case hierarchical.Tendermint:
			genBytes, testDataFileErr = ioutil.ReadFile(TendermintConsensusGenesisTestFile)
		case hierarchical.FilecoinEC:
			genBytes, testDataFileErr = ioutil.ReadFile(FilcnsConsensusGenesisTestFile)
		default:
			n.t.Fatalf("unknown consensus genesis file %d", n.options.rootConsensus)
		}

		require.NoError(n.t, testDataFileErr)

		// Either generate the genesis or inject it.
		if i == 0 && !n.bootstrapped {
			//TODO: compare with original itests and fix
			opts = append(opts, node.Override(new(modules.Genesis), modules.LoadGenesis(genBytes)))
			_ = gtempl
			//opts = append(opts, node.Override(new(modules.Genesis), testing2.MakeGenesisMem(&n.genesisBlock, *gtempl)))
		} else {
			opts = append(opts, node.Override(new(modules.Genesis), modules.LoadGenesis(genBytes)))
		}

		// Are we mocking proofs?
		if n.options.mockProofs {
			opts = append(opts,
				node.Override(new(ffiwrapper.Verifier), mock.MockVerifier),
				node.Override(new(ffiwrapper.Prover), mock.MockProver),
			)
		}

		// Call option builders, passing active nodes as the parameter
		for _, bopt := range full.options.optBuilders {
			opts = append(opts, bopt(n.active.fullnodes))
		}

		// Construct the full node.
		stop, err := node.New(ctx, opts...)
		require.NoError(n.t, err)

		var addr address.Address
		var ki types.KeyInfo
		switch n.options.rootConsensus {
		case hierarchical.PoW:
			addr, err = full.WalletImport(context.Background(), &full.DefaultKey.KeyInfo)
		case hierarchical.Delegated:
			err = ReadKeyInfoFromFile(DelegatedConsensusKeyFile, &ki)
			require.NoError(n.t, err)
			addr, err = full.WalletImport(context.Background(), &ki)
			require.NoError(n.t, err)
		case hierarchical.Tendermint:
			ki, err := tendermint.GetSecp256k1TendermintKey(TendermintConsensusKeyFile)
			require.NoError(n.t, err)
			addr, err = full.WalletImport(ctx, ki)
			require.NoError(n.t, err)
		default:
			n.t.Fatalf("unknown consensus type %d", n.options.rootConsensus)
		}
		require.NoError(n.t, err)

		err = full.WalletSetDefault(context.Background(), addr)
		require.NoError(n.t, err)

		// Are we hitting this node through its RPC?
		if full.options.rpc {
			withRPC := fullRpc(n.t, full)
			n.inactive.fullnodes[i] = withRPC
		}

		n.t.Cleanup(func() { _ = stop(context.Background()) })

		n.active.fullnodes = append(n.active.fullnodes, full)
	}

	// If we are here, we have processed all inactive fullnodes and moved them
	// to active, so clear the slice.
	n.inactive.fullnodes = n.inactive.fullnodes[:0]

	// Link all the nodes.
	err = n.mn.LinkAll()
	require.NoError(n.t, err)
	return n
}

// InterconnectAll connects all miners and full nodes to one another.
func (n *EudicoEnsemble) InterconnectAll() *EudicoEnsemble {
	// connect full nodes to miners.
	for _, from := range n.active.fullnodes {
		for _, to := range n.active.miners {
			// []*TestMiner to []api.CommonAPI type coercion not possible
			// so cannot use variadic form.
			n.Connect(from, to)
		}
	}

	// connect full nodes between each other, skipping ourselves.
	last := len(n.active.fullnodes) - 1
	for i, from := range n.active.fullnodes {
		if i == last {
			continue
		}
		for _, to := range n.active.fullnodes[i+1:] {
			n.Connect(from, to)
		}
	}
	return n
}

// Connect connects one full node to the provided full nodes.
func (n *EudicoEnsemble) Connect(from api.Net, to ...api.Net) *EudicoEnsemble {
	addr, err := from.NetAddrsListen(context.Background())
	require.NoError(n.t, err)

	for _, other := range to {
		err = other.NetConnect(context.Background(), addr)
		require.NoError(n.t, err)
	}
	return n
}

func (n *EudicoEnsemble) BeginMiningMustPost(blocktime time.Duration, miners ...*TestMiner) []*BlockMiner {
	ctx := context.Background()

	// wait one second to make sure that nodes are connected and have handshaken.
	// TODO make this deterministic by listening to identify events on the
	//  libp2p eventbus instead (or something else).
	time.Sleep(1 * time.Second)

	var bms []*BlockMiner
	if len(miners) == 0 {
		// no miners have been provided explicitly, instantiate block miners
		// for all active miners that aren't still mining.
		for _, m := range n.active.miners {
			if _, ok := n.active.bms[m]; ok {
				continue // skip, already have a block miner
			}
			miners = append(miners, m)
		}
	}

	if len(miners) > 1 {
		n.t.Fatalf("Only one active miner for MustPost, but have %d", len(miners))
	}

	for _, m := range miners {
		bm := NewBlockMiner(n.t, m)
		bm.MineBlocksMustPost(ctx, blocktime)
		n.t.Cleanup(bm.Stop)

		bms = append(bms, bm)

		n.active.bms[m] = bm
	}

	return bms
}

// BeginMining kicks off mining for the specified miners. If nil or 0-length,
// it will kick off mining for all enrolled and active miners. It also adds a
// cleanup function to stop all mining operations on test teardown.
func (n *EudicoEnsemble) BeginMining(blocktime time.Duration, miners ...*TestMiner) []*BlockMiner {
	ctx := context.Background()

	// wait one second to make sure that nodes are connected and have handshaken.
	// TODO make this deterministic by listening to identify events on the
	//  libp2p eventbus instead (or something else).
	time.Sleep(1 * time.Second)

	var bms []*BlockMiner
	if len(miners) == 0 {
		// no miners have been provided explicitly, instantiate block miners
		// for all active miners that aren't still mining.
		for _, m := range n.active.miners {
			if _, ok := n.active.bms[m]; ok {
				continue // skip, already have a block miner
			}
			miners = append(miners, m)
		}
	}

	for _, m := range miners {
		bm := NewBlockMiner(n.t, m)
		bm.MineBlocks(ctx, blocktime)
		n.t.Cleanup(bm.Stop)

		bms = append(bms, bm)

		n.active.bms[m] = bm
	}

	return bms
}

func (n *EudicoEnsemble) generateGenesis() *genesis.Template {
	var verifRoot = gen.DefaultVerifregRootkeyActor
	if k := n.options.verifiedRoot.key; k != nil {
		verifRoot = genesis.Actor{
			Type:    genesis.TAccount,
			Balance: n.options.verifiedRoot.initialBalance,
			Meta:    (&genesis.AccountMeta{Owner: k.Address}).ActorMeta(),
		}
	}

	templ := &genesis.Template{
		NetworkVersion:   n.genesis.version,
		Accounts:         n.genesis.accounts,
		Miners:           n.genesis.miners,
		NetworkName:      "test",
		Timestamp:        uint64(time.Now().Unix() - int64(n.options.pastOffset.Seconds())),
		VerifregRootKey:  verifRoot,
		RemainderAccount: gen.DefaultRemainderAccountActor,
	}

	return templ
}

// EudicoEnsembleMinimal creates and starts an EudicoEnsemble with a single full node and a single miner.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EudicoEnsembleMinimal(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full  TestFullNode
		miner TestMiner
	)
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&full, nopts...).Miner(&miner, &full, nopts...).Start()
	return &full, &miner, ens
}

func EudicoEnsembleWithMinerAndMarketNodes(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *TestMiner, *EudicoEnsemble) {
	eopts, nopts := siftOptions(t, opts)

	var (
		fullnode     TestFullNode
		main, market TestMiner
	)

	mainNodeOpts := []NodeOpt{WithSubsystems(SSealing, SSectorStorage, SMining), DisableLibp2p()}
	mainNodeOpts = append(mainNodeOpts, nopts...)

	blockTime := 100 * time.Millisecond
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&fullnode, nopts...).Miner(&main, &fullnode, mainNodeOpts...).Start()
	ens.BeginMining(blockTime)

	marketNodeOpts := []NodeOpt{OwnerAddr(fullnode.DefaultKey), MainMiner(&main), WithSubsystems(SMarkets)}
	marketNodeOpts = append(marketNodeOpts, nopts...)

	ens.Miner(&market, &fullnode, marketNodeOpts...).Start().Connect(market, fullnode)

	return &fullnode, &main, &market, ens
}

// EudicoEnsembleTwoOne creates and starts an Ensemble with two full nodes and one miner.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EudicoEnsembleTwoOne(t *testing.T, opts ...interface{}) (*TestFullNode, *TestFullNode, *TestMiner, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		one, two TestFullNode
		miner    TestMiner
	)
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&one, nopts...).FullNode(&two, nopts...).Miner(&miner, &one, nopts...).Start()
	return &one, &two, &miner, ens
}

// EudicoEnsembleOneTwo creates and starts an Ensemble with one full node and two miners.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EudicoEnsembleOneTwo(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *TestMiner, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full     TestFullNode
		one, two TestMiner
	)
	ens := NewEudicoEnsemble(t, eopts...).
		FullNode(&full, nopts...).
		Miner(&one, &full, nopts...).
		Miner(&two, &full, nopts...).
		Start()

	return &full, &one, &two, ens
}

func ReadKeyInfoFromFile(name string, key interface{}) error {
	hexdata, err := ioutil.ReadFile(name)
	if err != nil {
		return err
	}

	data, err := hex.DecodeString(strings.TrimSpace(string(hexdata)))
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, &key)
	if err != nil {
		return err
	}

	return nil
}
