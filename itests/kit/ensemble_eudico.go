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
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/ipfs/go-datastore"
	libp2pcrypto "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	abciserver "github.com/tendermint/tendermint/abci/server"
	tmlogger "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"go.uber.org/fx"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/go-storedcounter"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/dummy"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/submgr"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
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
	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/mock"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/lib/peermgr"
	lotusminer "github.com/filecoin-project/lotus/miner"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	testing2 "github.com/filecoin-project/lotus/node/modules/testing"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/mockstorage"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
	power2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/power"
)

const (
	TSPoWConsensusGenesisTestFile = "../testdata/tspow.gen"
	DummyConsensusGenesisTestFile = "../testdata/dummy.gen"

	DelegatedConsensusGenesisTestFile = "../testdata/delegcns.gen"
	DelegatedConsnensusMinerAddr      = "f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy"
	DelegatedConsensusKeyFile         = "../testdata/wallet/" + DelegatedConsnensusMinerAddr + ".key"

	TendermintConsensusGenesisTestFile = "../testdata/tendermint.gen"
	TendermintConsensusTestDir         = "../testdata/tendermint-test"
	TendermintConsensusKeyFile         = TendermintConsensusTestDir + "/config/priv_validator_key.json"
	TendermintApplicationAddress       = "tcp://127.0.0.1:26658"

	MirConsensusGenesisTestFile = "../testdata/mir.gen"

	FilcnsConsensusGenesisTestFile = "../testdata/filcns.gen"
)

func init() {
	chain.BootstrapPeerThreshold = 1
	messagepool.HeadChangeCoalesceMinDelay = time.Microsecond
	messagepool.HeadChangeCoalesceMaxDelay = 2 * time.Microsecond
	messagepool.HeadChangeCoalesceMergeInterval = 100 * time.Nanosecond
}

// EudicoEnsemble is a type similar to the original Ensemble type, but having extensions that allow to work with Eudico.
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

	tendermintContainerID string
	tendermintAppServer   service.Service

	valNetAddr string
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

// ValidatorInfo returns validators information.
func (n *EudicoEnsemble) ValidatorInfo(opts ...NodeOpt) (uint64, string) {
	return n.options.minValidators, n.valNetAddr
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

		// create an owner key, and assign it some FIL.
		ownerKey, err = wallet.NewKey(*k)
		require.NoError(n.t, err)

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
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) (consensus.Consensus, error) {
	return tendermint.NewConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

func NewFilecoinExpectedConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return filcns.NewFilecoinExpectedConsensus(ctx, sm, beacon, r, verifier, genesis)
}

func NewRootMirConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) (consensus.Consensus, error) {
	return mir.NewConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

func NewRootDummyConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) (consensus.Consensus, error) {
	return dummy.NewConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

func NetworkName(mctx helpers.MetricsCtx, lc fx.Lifecycle, cs *store.ChainStore, tsexec stmgr.Executor,
	syscalls vm.SyscallBuilder, us stmgr.UpgradeSchedule, _ dtypes.AfterGenesisSet) (dtypes.NetworkName, error) {

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

// Stop stop ensemble mechanisms.
func (n *EudicoEnsemble) Stop() error {
	if n.options.subnetConsensus == hierarchical.Tendermint ||
		n.options.rootConsensus == hierarchical.Tendermint {
		return n.stopTendermint()
	}

	if n.options.subnetConsensus == hierarchical.Mir ||
		n.options.rootConsensus == hierarchical.Mir {
		os.Unsetenv(mir.ValidatorsEnv) // nolint
		return n.stopMir()
	}
	return nil
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
			api, ok := iapi.(*submgr.API)
			if !ok {
				return xerrors.Errorf("Couldn't instantiate new subnet API. Something went wrong: %s", err)
			}
			// Instantiate the full node handler.
			h, err = submgr.FullNodeHandler(pp, api, true, serverOptions...)
			if err != nil {
				return fmt.Errorf("failed to instantiate rpc handler: %s", err)
			}
		}
		subnetMux.NewRoute().PathPrefix(pp).Handler(h)
		return nil
	}

	var rootConsensusConstructor interface{}
	switch n.options.rootConsensus {
	case hierarchical.PoW:
		rootConsensusConstructor = NewRootTSPoWConsensus
	case hierarchical.Delegated:
		rootConsensusConstructor = NewRootDelegatedConsensus
	case hierarchical.Tendermint:
		rootConsensusConstructor = NewRootTendermintConsensus
	case hierarchical.Mir:
		rootConsensusConstructor = NewRootMirConsensus
	case hierarchical.FilecoinEC:
		rootConsensusConstructor = NewFilecoinExpectedConsensus
	case hierarchical.Dummy:
		rootConsensusConstructor = NewRootDummyConsensus
	default:
		n.t.Fatalf("unknown consensus constructor %d", n.options.rootConsensus)
	}

	var rootConsensusWeight interface{}
	switch n.options.rootConsensus {
	case hierarchical.PoW:
		rootConsensusWeight = tspow.Weight
	case hierarchical.Delegated:
		rootConsensusWeight = delegcns.Weight
	case hierarchical.Tendermint:
		rootConsensusWeight = tendermint.Weight
	case hierarchical.Mir:
		rootConsensusWeight = mir.Weight
	case hierarchical.FilecoinEC:
		rootConsensusWeight = filcns.Weight
	case hierarchical.Dummy:
		rootConsensusWeight = dummy.Weight
	default:
		n.t.Fatalf("unknown consensus weight %d", n.options.rootConsensus)
	}

	if n.options.subnetConsensus == hierarchical.Tendermint ||
		n.options.rootConsensus == hierarchical.Tendermint {
		err := n.startTendermint()
		require.NoError(n.t, err)
	}

	if n.options.subnetConsensus == hierarchical.Mir ||
		n.options.rootConsensus == hierarchical.Mir {
		err := n.startMir()
		require.NoError(n.t, err)
	}

	// ---------------------
	//  GENESIS BLOCKS
	// ---------------------
	var rootGenesisBytes []byte
	var gferr error
	switch n.options.rootConsensus {
	case hierarchical.PoW:
		err := subnet.CreateGenesisFile(ctx, TSPoWConsensusGenesisTestFile, hierarchical.PoW, address.Undef)
		require.NoError(n.t, err)
		rootGenesisBytes, gferr = ioutil.ReadFile(TSPoWConsensusGenesisTestFile)
	case hierarchical.Delegated:
		miner, err := address.NewFromString(DelegatedConsnensusMinerAddr)
		require.NoError(n.t, err)
		require.Equal(n.t, address.SECP256K1, miner.Protocol())
		cerr := subnet.CreateGenesisFile(ctx, DelegatedConsensusGenesisTestFile, hierarchical.Delegated, miner)
		require.NoError(n.t, cerr)
		rootGenesisBytes, gferr = ioutil.ReadFile(DelegatedConsensusGenesisTestFile)
	case hierarchical.Tendermint:
		err := subnet.CreateGenesisFile(ctx, TendermintConsensusGenesisTestFile, hierarchical.Tendermint, address.Undef)
		require.NoError(n.t, err)
		rootGenesisBytes, gferr = ioutil.ReadFile(TendermintConsensusGenesisTestFile)
	case hierarchical.Mir:
		err := subnet.CreateGenesisFile(ctx, MirConsensusGenesisTestFile, hierarchical.Mir, address.Undef)
		require.NoError(n.t, err)
		rootGenesisBytes, gferr = ioutil.ReadFile(MirConsensusGenesisTestFile)
	case hierarchical.Dummy:
		err := subnet.CreateGenesisFile(ctx, DummyConsensusGenesisTestFile, hierarchical.Dummy, address.Undef)
		require.NoError(n.t, err)
		rootGenesisBytes, gferr = ioutil.ReadFile(DummyConsensusGenesisTestFile)
	case hierarchical.FilecoinEC:
	default:
		n.t.Fatalf("unknown genesis file %d", n.options.rootConsensus)
	}
	require.NoError(n.t, gferr)

	gtempl.NetworkName = "/root"

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

			// so that we subscribe to pubsub topics immediately
			node.Override(new(dtypes.Bootstrapper), dtypes.Bootstrapper(true)),

			node.Override(new(dtypes.NetworkName), NetworkName),
			node.Override(new(consensus.Consensus), rootConsensusConstructor),
			node.Override(new(store.WeightFunc), rootConsensusWeight),
			node.Unset(new(*slashfilter.SlashFilter)),
			node.Override(new(stmgr.Executor), common.RootTipSetExecutor),
			node.Override(new(stmgr.UpgradeSchedule), common.DefaultUpgradeSchedule()),
			node.Override(new(*resolver.Resolver), resolver.NewRootResolver),
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

		// TODO: compare with original itests and fix
		// Either generate the genesis or inject it.
		if n.options.rootConsensus == hierarchical.FilecoinEC {
			if i == 0 && !n.bootstrapped {
				opts = append(opts, node.Override(new(modules.Genesis), testing2.MakeGenesisMem(&n.genesisBlock, *gtempl)))
			} else {
				opts = append(opts, node.Override(new(modules.Genesis), modules.LoadGenesis(n.genesisBlock.Bytes())))
			}
		} else {
			opts = append(opts, node.Override(new(modules.Genesis), modules.LoadGenesis(rootGenesisBytes)))
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
			addr, err = full.WalletImport(ctx, &full.DefaultKey.KeyInfo)
			require.NoError(n.t, err)
		case hierarchical.Delegated:
			err = ReadKeyInfoFromFile(DelegatedConsensusKeyFile, &ki)
			require.NoError(n.t, err)
			addr, err = full.WalletImport(ctx, &ki)
			require.NoError(n.t, err)
		case hierarchical.Tendermint:
			ki, err := tendermint.GetSecp256k1TendermintKey(TendermintConsensusKeyFile)
			require.NoError(n.t, err)
			addr, err = full.WalletImport(ctx, ki)
			require.NoError(n.t, err)
		case hierarchical.Mir:
			addr, err = full.WalletImport(ctx, &full.DefaultKey.KeyInfo)
			require.NoError(n.t, err)
		case hierarchical.Dummy:
			addr, err = full.WalletImport(ctx, &full.DefaultKey.KeyInfo)
			require.NoError(n.t, err)
		case hierarchical.FilecoinEC:
			addr, err = full.WalletImport(ctx, &full.DefaultKey.KeyInfo)
			require.NoError(n.t, err)
		default:
			n.t.Fatalf("unknown consensus type %d", n.options.rootConsensus)
		}

		err = full.WalletSetDefault(ctx, addr)
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

	// ---------------------
	//  MINERS
	// ---------------------

	// Create all inactive miners.
	for i, m := range n.inactive.miners {
		if n.bootstrapped {
			if m.options.mainMiner == nil {
				// this is a miner created after genesis, so it won't have a preseal.
				// we need to create it on chain.

				// we get the proof type for the requested sector size, for
				// the current network version.
				nv, err := m.FullNode.FullNode.StateNetworkVersion(ctx, types.EmptyTSK)
				require.NoError(n.t, err)

				proofType, err := miner.SealProofTypeFromSectorSize(m.options.sectorSize, nv)
				require.NoError(n.t, err)

				params, aerr := actors.SerializeParams(&power2.CreateMinerParams{
					Owner:         m.OwnerKey.Address,
					Worker:        m.OwnerKey.Address,
					SealProofType: proofType,
					Peer:          abi.PeerID(m.Libp2p.PeerID),
				})
				require.NoError(n.t, aerr)

				createStorageMinerMsg := &types.Message{
					From:  m.OwnerKey.Address,
					To:    power.Address,
					Value: big.Zero(),

					Method: power.Methods.CreateMiner,
					Params: params,
				}
				signed, err := m.FullNode.FullNode.MpoolPushMessage(ctx, createStorageMinerMsg, nil)
				require.NoError(n.t, err)

				mw, err := m.FullNode.FullNode.StateWaitMsg(ctx, signed.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
				require.NoError(n.t, err)
				require.Equal(n.t, exitcode.Ok, mw.Receipt.ExitCode)

				var retval power2.CreateMinerReturn
				err = retval.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return))
				require.NoError(n.t, err, "failed to create miner")

				m.ActorAddr = retval.IDAddress
			} else {
				params, err := actors.SerializeParams(&miner2.ChangePeerIDParams{NewID: abi.PeerID(m.Libp2p.PeerID)})
				require.NoError(n.t, err)

				msg := &types.Message{
					To:     m.options.mainMiner.ActorAddr,
					From:   m.options.mainMiner.OwnerKey.Address,
					Method: miner.Methods.ChangePeerID,
					Params: params,
					Value:  types.NewInt(0),
				}

				signed, err2 := m.FullNode.FullNode.MpoolPushMessage(ctx, msg, nil)
				require.NoError(n.t, err2)

				mw, err2 := m.FullNode.FullNode.StateWaitMsg(ctx, signed.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
				require.NoError(n.t, err2)
				require.Equal(n.t, exitcode.Ok, mw.Receipt.ExitCode)
			}
		}

		has, err := m.FullNode.WalletHas(ctx, m.OwnerKey.Address)
		require.NoError(n.t, err)

		// Only import the owner's full key into our companion full node, if we
		// don't have it still.
		if !has {
			_, err = m.FullNode.WalletImport(ctx, &m.OwnerKey.KeyInfo)
			require.NoError(n.t, err)
		}

		// // Set it as the default address.
		// err = m.FullNode.WalletSetDefault(ctx, m.OwnerAddr.Address)
		// require.NoError(n.t, err)

		r := repo.NewMemory(nil)

		lr, err := r.Lock(repo.StorageMiner)
		require.NoError(n.t, err)

		c, err := lr.Config()
		require.NoError(n.t, err)

		cfg, ok := c.(*config.StorageMiner)
		if !ok {
			n.t.Fatalf("invalid config from repo, got: %T", c)
		}
		cfg.Common.API.RemoteListenAddress = m.RemoteListener.Addr().String()
		cfg.Subsystems.EnableMarkets = m.options.subsystems.Has(SMarkets)
		cfg.Subsystems.EnableMining = m.options.subsystems.Has(SMining)
		cfg.Subsystems.EnableSealing = m.options.subsystems.Has(SSealing)
		cfg.Subsystems.EnableSectorStorage = m.options.subsystems.Has(SSectorStorage)
		cfg.Dealmaking.MaxStagingDealsBytes = m.options.maxStagingDealsBytes

		if m.options.mainMiner != nil {
			token, err := m.options.mainMiner.FullNode.AuthNew(ctx, api.AllPermissions)
			require.NoError(n.t, err)

			cfg.Subsystems.SectorIndexApiInfo = fmt.Sprintf("%s:%s", token, m.options.mainMiner.ListenAddr)
			cfg.Subsystems.SealerApiInfo = fmt.Sprintf("%s:%s", token, m.options.mainMiner.ListenAddr)

			fmt.Println("config for market node, setting SectorIndexApiInfo to: ", cfg.Subsystems.SectorIndexApiInfo)
			fmt.Println("config for market node, setting SealerApiInfo to: ", cfg.Subsystems.SealerApiInfo)
		}

		err = lr.SetConfig(func(raw interface{}) {
			rcfg := raw.(*config.StorageMiner)
			*rcfg = *cfg
		})
		require.NoError(n.t, err)

		ks, err := lr.KeyStore()
		require.NoError(n.t, err)

		pk, err := libp2pcrypto.MarshalPrivateKey(m.Libp2p.PrivKey)
		require.NoError(n.t, err)

		err = ks.Put("libp2p-host", types.KeyInfo{
			Type:       "libp2p-host",
			PrivateKey: pk,
		})
		require.NoError(n.t, err)

		ds, err := lr.Datastore(context.TODO(), "/metadata")
		require.NoError(n.t, err)

		err = ds.Put(ctx, datastore.NewKey("miner-address"), m.ActorAddr.Bytes())
		require.NoError(n.t, err)

		nic := storedcounter.New(ds, datastore.NewKey(modules.StorageCounterDSPrefix))
		for i := 0; i < m.options.sectors; i++ {
			_, err := nic.Next()
			require.NoError(n.t, err)
		}
		_, err = nic.Next()
		require.NoError(n.t, err)

		err = lr.Close()
		require.NoError(n.t, err)

		if m.options.mainMiner == nil {
			enc, err := actors.SerializeParams(&miner2.ChangePeerIDParams{NewID: abi.PeerID(m.Libp2p.PeerID)})
			require.NoError(n.t, err)

			msg := &types.Message{
				From:   m.OwnerKey.Address,
				To:     m.ActorAddr,
				Method: miner.Methods.ChangePeerID,
				Params: enc,
				Value:  types.NewInt(0),
			}

			_, err2 := m.FullNode.MpoolPushMessage(ctx, msg, nil)
			require.NoError(n.t, err2)
		}

		var mineBlock = make(chan lotusminer.MineReq)
		opts := []node.Option{
			node.StorageMiner(&m.StorageMiner, cfg.Subsystems),
			node.Base(),
			node.Repo(r),
			node.Test(),

			node.If(!m.options.disableLibp2p, node.MockHost(n.mn)),

			node.Override(new(v1api.FullNode), m.FullNode.FullNode),
			node.Override(new(*lotusminer.Miner), lotusminer.NewTestMiner(mineBlock, m.ActorAddr)),

			// disable resource filtering so that local worker gets assigned tasks
			// regardless of system pressure.
			// node.Override(new(sectorstorage.SealerConfig), func() sectorstorage.SealerConfig {
			//         scfg := config.DefaultStorageMiner()
			//         scfg.Storage.ResourceFiltering = sectorstorage.ResourceFilteringDisabled
			//         return scfg.Storage
			// }),

			// upgrades
			node.Override(new(stmgr.UpgradeSchedule), n.options.upgradeSchedule),
		}

		// append any node builder options.
		opts = append(opts, m.options.extraNodeOpts...)

		idAddr, err := address.IDFromAddress(m.ActorAddr)
		require.NoError(n.t, err)

		// preload preseals if the network still hasn't bootstrapped.
		var presealSectors []abi.SectorID
		if !n.bootstrapped {
			sectors := n.genesis.miners[i].Sectors
			for _, sector := range sectors {
				presealSectors = append(presealSectors, abi.SectorID{
					Miner:  abi.ActorID(idAddr),
					Number: sector.SectorID,
				})
			}
		}

		if n.options.mockProofs {
			opts = append(opts,
				node.Override(new(*mock.SectorMgr), func() (*mock.SectorMgr, error) {
					return mock.NewMockSectorMgr(presealSectors), nil
				}),
				node.Override(new(sectorstorage.SectorManager), node.From(new(*mock.SectorMgr))),
				node.Override(new(sectorstorage.Unsealer), node.From(new(*mock.SectorMgr))),
				node.Override(new(sectorstorage.PieceProvider), node.From(new(*mock.SectorMgr))),

				node.Override(new(ffiwrapper.Verifier), mock.MockVerifier),
				node.Override(new(ffiwrapper.Prover), mock.MockProver),
				node.Unset(new(*sectorstorage.Manager)),
			)
		}

		// start node
		stop, err := node.New(ctx, opts...)
		require.NoError(n.t, err)

		// using real proofs, therefore need real sectors.
		if !n.bootstrapped && !n.options.mockProofs {
			err := m.StorageAddLocal(ctx, m.PresealDir)
			require.NoError(n.t, err)
		}

		n.t.Cleanup(func() { _ = stop(context.Background()) })

		// Are we hitting this node through its RPC?
		if m.options.rpc {
			withRPC := minerRpc(n.t, m)
			n.inactive.miners[i] = withRPC
		}

		mineOne := func(ctx context.Context, req lotusminer.MineReq) error {
			select {
			case mineBlock <- req:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		m.MineOne = mineOne
		m.Stop = stop

		n.active.miners = append(n.active.miners, m)
	}
	// If we are here, we have processed all inactive miners and moved them
	// to active, so clear the slice.
	n.inactive.miners = n.inactive.miners[:0]

	// Link all the nodes.
	err = n.mn.LinkAll()
	require.NoError(n.t, err)

	return n
}

func (n *EudicoEnsemble) GetDefaultKeyAddr() []address.Address {
	var addrs []address.Address
	for _, full := range n.active.fullnodes {
		addr, err := full.WalletDefaultAddress(context.Background())
		require.NoError(n.t, err)
		addrs = append(addrs, addr)

	}
	return addrs
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

func (n *EudicoEnsemble) startTendermint() error {
	// ensure there is no old tendermint data and config files.
	if err := n.removeTendermintFiles(); err != nil {
		return err
	}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	serverErrors := make(chan error, 1)
	app, err := tendermint.NewApplication()
	if err != nil {
		return err
	}

	logger := tmlogger.MustNewDefaultLogger(tmlogger.LogFormatPlain, tmlogger.LogLevelInfo, false)
	n.tendermintAppServer = abciserver.NewSocketServer(TendermintApplicationAddress, app)
	n.tendermintAppServer.SetLogger(logger)

	go func() {
		serverErrors <- n.tendermintAppServer.Start()
	}()

	// start Tendermint validator
	tm, err := StartTendermintContainer()
	if err != nil {
		return err
	}

	n.tendermintContainerID = tm.ID
	return nil
}

func (n *EudicoEnsemble) removeTendermintFiles() error {
	if err := os.RemoveAll(TendermintConsensusTestDir + "/config"); err != nil {
		return err
	}
	if err := os.RemoveAll(TendermintConsensusTestDir + "/data"); err != nil {
		return err
	}
	return nil
}

func (n *EudicoEnsemble) removeMirFiles() error {
	files, err := filepath.Glob("./eudico-wal*")
	if err != nil {
		return err
	}
	for _, f := range files {
		err = os.RemoveAll("./" + f)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *EudicoEnsemble) stopTendermint() error {
	if n.tendermintAppServer == nil {
		return xerrors.New("tendermint server is not running")
	}

	if err := n.tendermintAppServer.Stop(); err != nil {
		return xerrors.Errorf("failed to stop tendermint server %w:", err)
	}

	if n.tendermintContainerID == "" {
		return xerrors.New("tendermint container ID is not set")
	}

	err1 := StopContainer(n.tendermintContainerID)
	err2 := n.removeTendermintFiles()
	return multierr.Combine(
		err1,
		err2,
	)
}

func (n *EudicoEnsemble) startMir() error {
	return n.removeMirFiles()
}

func (n *EudicoEnsemble) stopMir() error {
	return n.removeMirFiles()
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

type EudicoRootMiner func(ctx context.Context, addr address.Address, api v1api.FullNode) error

// EudicoEnsembleTwoMiners creates types for root miner and subnet miner that can be used to spawn real miners.
// The main reason why this hack is used is that the Filecoin consensus and Eudico consensus protocol have different implementations.
// For example, the Filecoin consensus doesn't have a single Mine() function that implements mining.
func EudicoEnsembleTwoMiners(t *testing.T, opts ...interface{}) (*TestFullNode, interface{}, hierarchical.ConsensusType, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full  TestFullNode
		miner TestMiner
	)

	options := DefaultEnsembleOpts
	for _, o := range eopts {
		err := o(&options)
		require.NoError(t, err)
	}

	var rootMiner EudicoRootMiner

	switch options.rootConsensus {
	case hierarchical.Tendermint:
		rootMiner = tendermint.Mine
	case hierarchical.PoW:
		rootMiner = tspow.Mine
	case hierarchical.Delegated:
		rootMiner = delegcns.Mine
	case hierarchical.Mir:
		rootMiner = mir.Mine
	case hierarchical.Dummy:
		rootMiner = dummy.Mine
	case hierarchical.FilecoinEC:
		// Filecoin miner is created below within itests approach.
		break
	default:
		t.Fatalf("root consensus %d not supported", options.rootConsensus)
	}

	var subnetMinerType hierarchical.ConsensusType

	switch options.subnetConsensus {
	case hierarchical.Tendermint:
		subnetMinerType = hierarchical.Tendermint
	case hierarchical.PoW:
		subnetMinerType = hierarchical.PoW
	case hierarchical.Delegated:
		subnetMinerType = hierarchical.Delegated
	case hierarchical.Mir:
		subnetMinerType = hierarchical.Mir
	case hierarchical.Dummy:
		subnetMinerType = hierarchical.Dummy
	default:
		t.Fatalf("subnet consensus %d not supported", options.subnetConsensus)
	}

	var ens *EudicoEnsemble

	if options.rootConsensus == hierarchical.FilecoinEC {
		ens = NewEudicoEnsemble(t, eopts...).FullNode(&full, nopts...).Miner(&miner, &full, nopts...).Start()
		return &full, &miner, subnetMinerType, ens
	}
	ens = NewEudicoEnsemble(t, eopts...).FullNode(&full, nopts...).Start()

	if options.rootConsensus == hierarchical.Mir {
		addr, err := full.WalletDefaultAddress(context.Background())
		require.NoError(t, err)

		libp2pPrivKeyBytes, err := full.PrivKey(context.Background())
		require.NoError(t, err)

		mirNodeID := fmt.Sprintf("%s:%s", address.RootSubnet, addr.String())

		a, err := GetLibp2pAddr(libp2pPrivKeyBytes)
		require.NoError(t, err)

		err = os.Setenv(mir.ValidatorsEnv, fmt.Sprintf("%s@%s", mirNodeID, a))
		require.NoError(t, err)
	}

	if options.subnetConsensus == hierarchical.Mir {
		libp2pPrivKeyBytes, err := full.PrivKey(context.Background())
		require.NoError(t, err)

		a, err := GetLibp2pAddr(libp2pPrivKeyBytes)
		require.NoError(t, err)

		ens.valNetAddr = a.String()
	}

	return &full, rootMiner, subnetMinerType, ens
}

// EudicoEnsembleFullNodeOnly creates and starts a EudicoEnsemble with a single full node.
// It is used with Eudico consensus protocols that implementation is different from Filecoin one.
func EudicoEnsembleFullNodeOnly(t *testing.T, opts ...interface{}) (*TestFullNode, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full TestFullNode
	)
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&full, nopts...).Start()
	return &full, ens
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

// EudicoEnsembleTwoNodes creates and starts an Ensemble with two full nodes.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EudicoEnsembleTwoNodes(t *testing.T, opts ...interface{}) (*TestFullNode, *TestFullNode, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		one, two TestFullNode
	)
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&one, nopts...).FullNode(&two, nopts...).Start()
	return &one, &two, ens
}

// EudicoEnsembleFourNodes creates and starts an Ensemble with for full nodes.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EudicoEnsembleFourNodes(t *testing.T, opts ...interface{}) (*TestFullNode, *TestFullNode, *TestFullNode, *TestFullNode, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		n1, n2, n3, n4 TestFullNode
	)
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&n1, nopts...).FullNode(&n2, nopts...).FullNode(&n3, nopts...).FullNode(&n4, nopts...).Start()
	return &n1, &n2, &n3, &n4, ens
}

// EudicoEnsembleThreeNodes creates and starts an Ensemble with for full nodes.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EudicoEnsembleThreeNodes(t *testing.T, opts ...interface{}) (*TestFullNode, *TestFullNode, *TestFullNode, *EudicoEnsemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		n1, n2, n3 TestFullNode
	)
	ens := NewEudicoEnsemble(t, eopts...).FullNode(&n1, nopts...).FullNode(&n2, nopts...).FullNode(&n3, nopts...).Start()
	return &n1, &n2, &n3, ens
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
