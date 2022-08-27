// stm: #integration
package itests

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/dummy"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func TestHC_SmokeTestWithDummyConsensus(t *testing.T) {
	t.Run("/root/dummy-/subnet/dummy", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetDummy())
	})
}

func TestHC_DataRaces(t *testing.T) {
	t.Run("/root/dummy-/subnet/mir", func(t *testing.T) {
		runDataRacesTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetMir(), kit.MinValidators(3))
	})
}

func TestHC_TwoNodesTestsWithMirConsensus(t *testing.T) {
	t.Run("/root/mir-/subnet/mir", func(t *testing.T) {
		runTwoNodesTestsWithMir(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetMir())
	})
}

func TestHC_TwoNodesCrossMessage(t *testing.T) {
	t.Run("/root/mir-/subnet/pow", func(t *testing.T) {
		runTwoNodesCrossMessage(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetTSPoW())
	})
}

func TestHC_BasicFlowWithMirInRootnet(t *testing.T) {
	t.Run("/root/mir-/subnet/delegated", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetDelegated())
	})
}

func TestHC_BasicFlowWithMirInSubnet(t *testing.T) {
	t.Run("/root/dummy-/subnet/mir", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetMir(), kit.MinValidators(1))
	})

	t.Run("/root/delegated-/subnet/mir", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetMir(), kit.MinValidators(1))
	})
}

func TestHC_MirReconfigurationViaSubnetActor(t *testing.T) {
	t.Run("/root/dummy-/subnet/mir", func(t *testing.T) {
		runMirReconfigurationTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetMir(), kit.MinValidators(2))
	})
}

func TestHC_BasicFlowWithLegacyConsensus(t *testing.T) {
	// Filecoin consensus in root

	t.Run("/root/filcns-/subnet/delegated", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootFilcns(), kit.SubnetDelegated())
	})

	t.Run("/root/filcns-/subnet/pow", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootFilcns(), kit.SubnetTSPoW())
	})

	t.Run("/root/delegated-/subnet/pow", func(t *testing.T) {
		runBasicFlowTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetTSPoW())
	})

	if os.Getenv("FULL_ITESTS") != "" {

		// PoW in Root

		t.Run("/root/pow-/subnet/pow", func(t *testing.T) {
			runBasicFlowTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTSPoW())
		})

		t.Run("/root/pow-/subnet/delegated", func(t *testing.T) {
			runBasicFlowTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetDelegated())
		})

		// Delegated consensus in root

		t.Run("/root/delegated-/subnet/delegated", func(t *testing.T) {
			runBasicFlowTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetDelegated())
		})
	}
}

func runBasicFlowTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testBasicSubnetFlow", ts.testBasicFlow)
}

type eudicoSubnetSuite struct {
	opts []interface{}
}

func (ts *eudicoSubnetSuite) testBasicFlow(t *testing.T) {
	var wg sync.WaitGroup

	full, rootMiner, subnetMinerType, ens := kit.EudicoEnsembleTwoMiners(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	n, valAddr := ens.ValidatorInfo()

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)
	t.Logf("[*] wallet addr: %s", addr)

	newAddr, err := full.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf("[*] wallet new addr: %s", newAddr)

	startTime := time.Now()

	// Start mining in root net: start Filecoin consensus or Eudico consensus.
	switch miner := rootMiner.(type) {
	case *kit.TestMiner:
		bm := kit.NewBlockMiner(t, miner)
		bm.MineBlocks(ctx, 1*time.Second)
	case kit.EudicoRootMiner:
		wg.Add(1)
		go func() {
			t.Log("[*] miner in root net starting")
			defer func() {
				t.Log("[*] miner in root net stopped")
				wg.Done()
			}()
			err := miner(ctx, addr, full)
			if err != nil {
				t.Error("root miner error:", err)
				cancel()
				return
			}
		}()
	default:
		t.Fatal("unsupported root consensus")
	}

	t.Log("[*] adding and joining subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	stake := abi.NewStoragePower(1e8)
	chp := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, addr, 12, full)
	require.NoError(t, err)

	balance, err := full.WalletBalance(ctx, addr)
	require.NoError(t, err)
	t.Logf("[*] %s balance: %d", addr, balance)

	cns := subnetMinerType
	subnetParams := &hierarchical.SubnetParams{
		Addr:             addr,
		Parent:           parent,
		Name:             subnetName,
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           cns,
			MinValidators: n,
			DelegMiner:    addr,
		},
	}
	actorAddr, err := full.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)

	networkName, err := full.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	t.Log("[*] subnet addr:", subnetAddr)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = full.StateLookupID(ctx, addr, types.EmptyTSK)
	require.NoError(t, err)

	sc, err := full.JoinSubnet(ctx, addr, big.Int(val), subnetAddr, valAddr)
	require.NoError(t, err)

	_, err = full.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] listing subnets")
	sn, err := full.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Subnet.Status)
	require.Equal(t, subnetMinerType, sn[0].Consensus)

	t.Log("[*] miner in subnet starting")
	smp := hierarchical.MiningParams{}
	err = full.MineSubnet(ctx, addr, subnetAddr, false, &smp)
	if err != nil {
		t.Error("subnet miner error:", err)
		cancel()
		return
	}

	// Inject new funds to the own address in the subnet.

	t.Log("[*] funding subnet")
	injectedFils := big.Int(types.MustParseFIL("3"))
	_, err = full.FundSubnet(ctx, addr, subnetAddr, injectedFils)
	require.NoError(t, err)

	// Send a message to the new address.
	t.Log("[*] sending a message")

	sentFils := big.Int(types.MustParseFIL("3"))
	require.NoError(t, err)

	var params lcli.SendParams
	params.Method = builtin.MethodSend
	params.To = newAddr
	params.From = addr
	params.Val = sentFils
	proto, err := kit.MessageForSend(ctx, full, params)
	require.NoError(t, err)

	crossParams := &sca.CrossMsgParams{
		Destination: subnetAddr,
		Msg:         proto.Message,
	}
	serparams, err := actors.SerializeParams(crossParams)
	require.NoError(t, err)

	msg, err := full.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   params.From,
		Value:  params.Val,
		Method: sca.Methods.SendCross,
		Params: serparams,
	}, nil)
	require.NoError(t, err)

	_, err = full.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	msg, err = full.MpoolPushMessage(ctx, &types.Message{
		To:    newAddr,
		From:  params.From,
		Value: sentFils,
	}, nil)
	require.NoError(t, err)

	_, err = full.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	_, err = kit.WaitSubnetActorBalance(ctx, subnetAddr, addr, injectedFils, full)
	require.NoError(t, err)

	a, err := full.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] %s addr balance: %d", addr, a.Balance)
	require.Equal(t, 0, big.Cmp(injectedFils, a.Balance))

	_, err = kit.WaitSubnetActorBalance(ctx, subnetAddr, newAddr, sentFils, full)
	require.NoError(t, err)

	a, err = full.SubnetStateGetActor(ctx, subnetAddr, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] %s new addr balance: %d", newAddr, a.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, a.Balance))

	// Release funds

	t.Log("[*] releasing funds")
	releasedFils := big.Int(types.MustParseFIL("2"))
	releaseCid, err := full.ReleaseFunds(ctx, newAddr, subnetAddr, releasedFils)
	require.NoError(t, err)

	_, err = full.SubnetStateWaitMsg(ctx, subnetAddr, releaseCid, 1, 100, false)
	require.NoError(t, err)

	_, err = kit.WaitSubnetActorBalance(ctx, parent, newAddr, big.Add(sentFils, releasedFils), full)
	require.NoError(t, err)

	a, err = full.StateGetActor(ctx, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] new addr: %s", newAddr)
	t.Logf("[*] new addr %s balance: %d", addr, a.Balance)
	require.Equal(t, 0, big.Cmp(a.Balance, big.Add(sentFils, releasedFils)))

	// Stop mining

	t.Log("[*] stop mining")
	mp := hierarchical.MiningParams{}
	err = full.MineSubnet(ctx, addr, subnetAddr, true, &mp)
	require.NoError(t, err)

	// Leaving the subnet.

	t.Log("[*] leaving subnet")
	_, err = full.LeaveSubnet(ctx, addr, subnetAddr)
	require.NoError(t, err)

	sn, err = full.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Subnet.Status)

	t.Logf("[*] test time: %v\n", time.Since(startTime).Seconds())
}

func runMirReconfigurationTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testMirReconfiguration", ts.testMirReconfiguration)
}

func (ts *eudicoSubnetSuite) testMirReconfiguration(t *testing.T) {
	var wg sync.WaitGroup

	nodeA, nodeB, nodeC, ens := kit.EudicoEnsembleThreeNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	t.Log("[*] connecting nodes")

	// Fail if genesis blocks are different

	gen1, err := nodeA.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := nodeB.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen3, err := nodeC.ChainGetGenesis(ctx)
	require.NoError(t, err)

	require.Equal(t, gen1.String(), gen2.String())
	require.Equal(t, gen2.String(), gen3.String())

	// Fail if no peers

	p, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node A has peers")

	p, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node B has peers")

	p, err = nodeC.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node C has peers")

	// Connect nodes with each other

	ens.Connect(nodeA, nodeB, nodeC)
	ens.Connect(nodeB, nodeC)

	peers, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 2, "node A doesn't have a peer")

	peers, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 2, "node B doesn't have a peer")

	peers, err = nodeC.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 2, "node C doesn't have a peer")

	l, err := nodeA.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("A's wallet key list is empty")
	}
	minerA := l[0]

	l, err = nodeB.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("B's wallet key list is empty")
	}
	minerB := l[0]

	l, err = nodeC.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("C's wallet key list is empty")
	}
	minerC := l[0]

	t.Log("[*] running Dummy consensus in root net")

	err = os.Setenv(dummy.ValidatorsEnv,
		fmt.Sprintf("%s,%s,%s", minerA.String(), minerB.String(), minerC.String()),
	)
	require.NoError(t, err)

	wg.Add(3)

	go func() {
		t.Log("[*] miner A in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner A in root net stopped")
		}()
		err := dummy.Mine(ctx, minerA, nodeA)
		if err != nil {
			t.Error("miner A error:", err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] miner B in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner B in root net stopped")
		}()
		err := dummy.Mine(ctx, minerB, nodeB)
		if err != nil {
			t.Error("miner B error:", err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] miner C in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner C in root net stopped")
		}()
		err := dummy.Mine(ctx, minerC, nodeC)
		if err != nil {
			t.Error("miner C error:", err)
			cancel()
			return
		}
	}()

	t.Log("[*] adding subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	stake := abi.NewStoragePower(1e8)
	chp := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, minerA, 20, nodeA)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerB, 20, nodeB)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerC, 20, nodeC)
	require.NoError(t, err)

	balanceA, err := nodeA.WalletBalance(ctx, minerA)
	require.NoError(t, err)
	t.Logf("[*] node A %s balance: %d", minerA, balanceA)

	balanceB, err := nodeB.WalletBalance(ctx, minerB)
	require.NoError(t, err)
	t.Logf("[*] node B %s balance: %d", minerB, balanceB)

	balanceC, err := nodeC.WalletBalance(ctx, minerC)
	require.NoError(t, err)
	t.Logf("[*] node C %s balance: %d", minerC, balanceC)

	os.Unsetenv(dummy.ValidatorsEnv) // nolint

	subnetParams := &hierarchical.SubnetParams{
		Addr:             minerA,
		Parent:           parent,
		Name:             subnetName,
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           hierarchical.Mir,
			MinValidators: 2,
			DelegMiner:    minerA,
		},
	}
	actorAddr, err := nodeA.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)
	t.Log("[*] subnet addr:", subnetAddr)

	networkName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	t.Log("[*] joining the subnet")

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	nodeASubnetLibp2pAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	nodeBSubnetLibp2pAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)
	nodeCSubnetLibp2pAddr, err := kit.NodeLibp2pAddr(nodeC)
	require.NoError(t, err)

	// Nodes A, B are joining the created subnet via the subnet actor.
	sc, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAddr, nodeASubnetLibp2pAddr.String())
	require.NoError(t, err)
	_, err = nodeA.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	sc, err = nodeB.JoinSubnet(ctx, minerB, big.Int(val), subnetAddr, nodeBSubnetLibp2pAddr.String())
	require.NoError(t, err)
	_, err = nodeB.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] listing subnets")

	subnets, err := nodeA.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(subnets))
	require.NotEqual(t, 0, subnets[0].Subnet.Status)
	require.Equal(t, hierarchical.Mir, subnets[0].Consensus)

	subnets, err = nodeB.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(subnets))
	require.NotEqual(t, 0, subnets[0].Subnet.Status)
	require.Equal(t, hierarchical.Mir, subnets[0].Consensus)

	t.Log("[*] miner A in subnet is starting")
	mp := hierarchical.MiningParams{}
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner A error:", err)
		cancel()
		return
	}

	t.Log("[*] miner B in subnet is starting")
	mp = hierarchical.MiningParams{}
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner B error:", err)
		cancel()
		return
	}

	t.Log("[*] miner A is mining in the subnet")
	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerA, nodeA)
	require.NoError(t, err)

	// Node C is joining the subnet
	sc, err = nodeC.JoinSubnet(ctx, minerC, big.Int(val), subnetAddr, nodeCSubnetLibp2pAddr.String())
	require.NoError(t, err)
	_, err = nodeC.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] miner C in subnet is starting")
	mp = hierarchical.MiningParams{}
	err = nodeC.MineSubnet(ctx, minerC, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner C error:", err)
		cancel()
		return
	}

	t.Log("[*] miners A and C are mining in the subnet")
	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerA, nodeA)
	require.NoError(t, err)
	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerC, nodeC)
	require.NoError(t, err)

	t.Log("[*] miners A and C are still mining in the subnet")
	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerA, nodeA)
	require.NoError(t, err)
	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerC, nodeC)
	require.NoError(t, err)

	// TODO: how to check that miner B is not mining blocks?
	// The problem is that calls are async and reconfiguration in Mir happens in +2 epochs.
}

func runDataRacesTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testMirReconfiguration", ts.testDataRaces)
}

// testDataRaces explores data-races on adding, joining and starting subnets in best-effort manner.
// This test use two dummy nodes in rootnet, 2 Mir nodes in subnet,
// and several additional sidenets with different consensus protocols.
// These special subnets are called sidenets. They are spawned concurrently.
// And their goal is to concurrently access subnet API.
func (ts *eudicoSubnetSuite) testDataRaces(t *testing.T) {
	var wg sync.WaitGroup // subnet wait group

	nodeA, nodeB, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	t.Log("[*] connecting nodes")

	// Fail if genesis blocks are different

	gen1, err := nodeA.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := nodeB.ChainGetGenesis(ctx)
	require.NoError(t, err)

	require.Equal(t, gen1.String(), gen2.String())

	// Fail if no peers

	p, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node A has peers")

	p, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node B has peers")

	// Connect nodes with each other

	ens.Connect(nodeA, nodeB)

	peers, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node A doesn't have a peer")

	peers, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node B doesn't have a peer")

	l, err := nodeA.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("A's wallet key list is empty")
	}
	minerA := l[0]

	l, err = nodeB.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("B's wallet key list is empty")
	}
	minerB := l[0]

	t.Log("[*] running Dummy consensus in root net")

	err = os.Setenv(dummy.ValidatorsEnv,
		fmt.Sprintf("%s,%s", minerA.String(), minerB.String()),
	)
	require.NoError(t, err)

	wg.Add(2)

	go func() {
		t.Log("[*] miner A in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner A in root net stopped")
		}()
		err := dummy.Mine(ctx, minerA, nodeA)
		if err != nil {
			t.Error("miner A error:", err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] miner B in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner B in root net stopped")
		}()
		err := dummy.Mine(ctx, minerB, nodeB)
		if err != nil {
			t.Error("miner B error:", err)
			cancel()
			return
		}
	}()

	t.Log("[*] adding subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	stake := abi.NewStoragePower(1e8)
	chp := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, minerA, 30, nodeA)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerB, 20, nodeB)
	require.NoError(t, err)

	balanceA, err := nodeA.WalletBalance(ctx, minerA)
	require.NoError(t, err)
	t.Logf("[*] node A %s balance: %d", minerA, balanceA)

	balanceB, err := nodeB.WalletBalance(ctx, minerB)
	require.NoError(t, err)
	t.Logf("[*] node B %s balance: %d", minerB, balanceB)

	os.Unsetenv(dummy.ValidatorsEnv) // nolint

	subnetParams := &hierarchical.SubnetParams{
		Addr:             minerA,
		Parent:           parent,
		Name:             subnetName,
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           hierarchical.Mir,
			MinValidators: 2,
			DelegMiner:    minerA,
		},
	}
	actorAddr, err := nodeA.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)
	t.Log("[*] subnet addr:", subnetAddr)

	networkName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	t.Log("[*] spawn testing sidenets")
	var sg sync.WaitGroup // sidenet wait group
	sg.Add(5)

	go kit.SpawnSideSubnet(ctx, cancel, t, &sg, minerA, parent, "sideSubnet1", stake, hierarchical.PoW, nodeA)
	go kit.SpawnSideSubnet(ctx, cancel, t, &sg, minerA, parent, "sideSubnet2", stake, hierarchical.Delegated, nodeA)
	go kit.SpawnSideSubnet(ctx, cancel, t, &sg, minerA, parent, "sideSubnet3", stake, hierarchical.Dummy, nodeA)
	go kit.SpawnSideSubnet(ctx, cancel, t, &sg, minerA, parent, "sideSubnet4", stake, hierarchical.PoW, nodeA)
	go kit.SpawnSideSubnet(ctx, cancel, t, &sg, minerA, parent, "sideSubnet5", stake, hierarchical.Dummy, nodeA)

	t.Log("[*] joining the subnet")

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	nodeASubnetLibp2pAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	nodeBSubnetLibp2pAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)

	// Nodes A, B, C are joining the created subnet via the subnet actor.
	sc, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAddr, nodeASubnetLibp2pAddr.String())
	require.NoError(t, err)
	_, err = nodeA.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	sc, err = nodeB.JoinSubnet(ctx, minerB, big.Int(val), subnetAddr, nodeBSubnetLibp2pAddr.String())
	require.NoError(t, err)
	_, err = nodeB.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] miner A in subnet is starting")
	mp := hierarchical.MiningParams{}
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner A error:", err)
		cancel()
		return
	}

	t.Log("[*] miner B in subnet is starting")
	mp = hierarchical.MiningParams{}
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner B error:", err)
		cancel()
		return
	}

	t.Log("[*] miners A, B are mining in the subnet")
	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerA, nodeA)
	require.NoError(t, err)

	err = kit.SubnetMinerMinesBlocks(ctx, 5, 20, subnetAddr, minerB, nodeB)
	require.NoError(t, err)

	sg.Wait()
}

func runTwoNodesTestsWithMir(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testBasicFlowOnTwoNodes", ts.testBasicFlowOnTwoNodes)
	t.Run("testStartStopOnTwoNodes", ts.testStartStopOnTwoNodes)
}

func (ts *eudicoSubnetSuite) testBasicFlowOnTwoNodes(t *testing.T) {
	var wg sync.WaitGroup

	nodeA, nodeB, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	t.Log("[*] connecting nodes")

	// Fail if genesis blocks are different

	gen1, err := nodeA.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := nodeB.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gen1.String(), gen2.String())

	// Fail if no peers

	p, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node A has peers")

	p, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node B has peers")

	ens.Connect(nodeA, nodeB)

	peers, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node A doesn't have a peer")

	peers, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node B doesn't have a peer")

	l, err := nodeA.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("A's wallet key list is empty")
	}
	minerA := l[0]

	l, err = nodeB.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("B's wallet key list is empty")
	}
	minerB := l[0]

	t.Log("[*] running Mir consensus in root net")

	startTime := time.Now()

	aAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	bAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)

	err = os.Setenv(mir.ValidatorsEnv, fmt.Sprintf("%s@%s,%s@%s",
		"/root:"+minerA.String(), aAddr.String(),
		"/root:"+minerB.String(), bAddr.String()))
	require.NoError(t, err)

	wg.Add(2)

	go func() {
		t.Log("[*] miner A in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner A in root net stopped")
		}()
		err := mir.Mine(ctx, minerA, nodeA)
		if err != nil {
			t.Error("miner A error:", err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] miner B in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner B in root net stopped")
		}()
		err := mir.Mine(ctx, minerB, nodeB)
		if err != nil {
			t.Error("miner B error:", err)
			cancel()
			return
		}
	}()

	t.Log("[*] adding subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	stake := abi.NewStoragePower(1e8)
	chp := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, minerA, 20, nodeA)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerB, 20, nodeB)
	require.NoError(t, err)

	balanceA, err := nodeA.WalletBalance(ctx, minerA)
	require.NoError(t, err)
	t.Logf("[*] node A %s balance: %d", minerA, balanceA)

	balanceB, err := nodeB.WalletBalance(ctx, minerB)
	require.NoError(t, err)
	t.Logf("[*] node B %s balance: %d", minerB, balanceB)

	os.Unsetenv(mir.ValidatorsEnv) // nolint

	subnetParams := &hierarchical.SubnetParams{
		Addr:             minerA,
		Parent:           parent,
		Name:             subnetName,
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           hierarchical.Mir,
			MinValidators: 2,
			DelegMiner:    minerA,
		},
	}
	actorAddr, err := nodeA.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)
	t.Log("[*] subnet addr:", subnetAddr)

	networkName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	t.Log("[*] joining the subnet")

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	nodeASubnetAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	nodeBSubnetAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)

	sc, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAddr, nodeASubnetAddr.String())
	require.NoError(t, err)
	_, err = nodeA.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	sc, err = nodeB.JoinSubnet(ctx, minerB, big.Int(val), subnetAddr, nodeBSubnetAddr.String())
	require.NoError(t, err)
	_, err = nodeB.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] listing subnets")
	sn1, err := nodeA.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn1))
	require.NotEqual(t, 0, sn1[0].Subnet.Status)
	require.Equal(t, hierarchical.Mir, sn1[0].Consensus)

	sn2, err := nodeB.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn2))
	require.NotEqual(t, 0, sn2[0].Subnet.Status)
	require.Equal(t, hierarchical.Mir, sn2[0].Consensus)

	t.Log("[*] miner A in subnet starting")
	mp := hierarchical.MiningParams{}
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner A error:", err)
		cancel()
		return
	}

	t.Log("[*] miner B in subnet starting")
	mp = hierarchical.MiningParams{}
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner B error:", err)
		cancel()
		return
	}

	// Inject new funds to the own address in the subnet.

	t.Log("[*] funding subnet")
	injectedFils := big.Int(types.MustParseFIL("3"))
	_, err = nodeA.FundSubnet(ctx, minerA, subnetAddr, injectedFils)
	require.NoError(t, err)

	// Send a message to the new address.
	t.Log("[*] sending a message")

	sentFils := big.Int(types.MustParseFIL("3"))
	require.NoError(t, err)

	subnetNewAddr, err := nodeA.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf("[*] subnet new addr: %s", subnetNewAddr)

	var params lcli.SendParams
	params.Method = builtin.MethodSend
	params.To = subnetNewAddr
	params.From = minerA
	params.Val = sentFils
	proto, err := kit.MessageForSend(ctx, nodeA, params)
	require.NoError(t, err)

	crossParams := &sca.CrossMsgParams{
		Destination: subnetAddr,
		Msg:         proto.Message,
	}
	serparams, err := actors.SerializeParams(crossParams)
	require.NoError(t, err)

	msg, err := nodeA.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   params.From,
		Value:  params.Val,
		Method: sca.Methods.SendCross,
		Params: serparams,
	}, nil)
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	msg, err = nodeA.MpoolPushMessage(ctx, &types.Message{
		To:    subnetNewAddr,
		From:  params.From,
		Value: sentFils,
	}, nil)
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	_, err = nodeB.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	_, err = kit.WaitSubnetActorBalance(ctx, subnetAddr, minerA, injectedFils, nodeA)
	require.NoError(t, err)

	a, err := nodeA.SubnetStateGetActor(ctx, subnetAddr, minerA, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] %s addr balance: %d", minerA, a.Balance)
	require.Equal(t, 0, big.Cmp(injectedFils, a.Balance))

	_, err = kit.WaitSubnetActorBalance(ctx, subnetAddr, subnetNewAddr, sentFils, nodeA)
	require.NoError(t, err)

	a, err = nodeA.SubnetStateGetActor(ctx, subnetAddr, subnetNewAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] node A %s new addr balance: %d", subnetNewAddr, a.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, a.Balance))

	_, err = kit.WaitSubnetActorBalance(ctx, subnetAddr, subnetNewAddr, sentFils, nodeB)
	require.NoError(t, err)

	b, err := nodeB.SubnetStateGetActor(ctx, subnetAddr, subnetNewAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] node B %s new addr balance: %d", subnetNewAddr, b.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, b.Balance))

	t.Log("[*] stop miner A in the subnet")
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, true, &mp)
	require.NoError(t, err)

	t.Log("[*] stop miner B in the subnet")
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, true, &mp)
	require.NoError(t, err)

	t.Logf("[*] test time: %v\n", time.Since(startTime).Seconds())
}

func (ts *eudicoSubnetSuite) testStartStopOnTwoNodes(t *testing.T) {
	var wg sync.WaitGroup

	nodeA, nodeB, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	t.Log("[*] connecting nodes")

	// Fail if genesis blocks are different

	gen1, err := nodeA.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := nodeB.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gen1.String(), gen2.String())

	// Fail if no peers

	p, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node A has peers")

	p, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node B has peers")

	ens.Connect(nodeA, nodeB)

	peers, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node A doesn't have a peer")

	peers, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node B doesn't have a peer")

	l, err := nodeA.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("A's wallet key list is empty")
	}
	minerA := l[0]

	l, err = nodeB.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("B's wallet key list is empty")
	}
	minerB := l[0]

	t.Log("[*] running consensus in root net")

	startTime := time.Now()

	aAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	bAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)

	err = os.Setenv(mir.ValidatorsEnv, fmt.Sprintf("%s@%s,%s@%s",
		"/root:"+minerA.String(), aAddr.String(),
		"/root:"+minerB.String(), bAddr.String()))
	require.NoError(t, err)

	wg.Add(2)

	go func() {
		t.Log("[*] miner A in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner A in root net stopped")
		}()
		err := mir.Mine(ctx, minerA, nodeA)
		if err != nil {
			t.Error("miner A error: ", err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] miner B in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner B in root net stopped")
		}()
		err := mir.Mine(ctx, minerB, nodeB)
		if err != nil {
			t.Error("miner B error: ", err)
			cancel()
			return
		}
	}()

	t.Log("[*] adding and joining subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	stake := abi.NewStoragePower(1e8)
	chp := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, minerA, 20, nodeA)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerB, 20, nodeB)
	require.NoError(t, err)

	balance1, err := nodeA.WalletBalance(ctx, minerA)
	require.NoError(t, err)
	t.Logf("[*] node A %s balance: %d", minerA, balance1)

	balance2, err := nodeB.WalletBalance(ctx, minerB)
	require.NoError(t, err)
	t.Logf("[*] node B %s balance: %d", minerB, balance2)

	os.Unsetenv(mir.ValidatorsEnv) // nolint

	subnetParams := &hierarchical.SubnetParams{
		Addr:             minerA,
		Parent:           parent,
		Name:             subnetName,
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           hierarchical.Mir,
			MinValidators: 2,
			DelegMiner:    minerA,
		},
	}
	actorAddr, err := nodeA.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)
	t.Log("[*] subnet addr:", subnetAddr)

	networkName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	saAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	sbAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)

	sc, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAddr, saAddr.String())
	require.NoError(t, err)
	_, err = nodeA.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	sc, err = nodeB.JoinSubnet(ctx, minerB, big.Int(val), subnetAddr, sbAddr.String())
	require.NoError(t, err)
	_, err = nodeB.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] listing subnets")
	sn1, err := nodeA.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn1))
	require.NotEqual(t, 0, sn1[0].Subnet.Status)
	require.Equal(t, hierarchical.Mir, sn1[0].Consensus)

	sn2, err := nodeB.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn2))
	require.NotEqual(t, 0, sn2[0].Subnet.Status)
	require.Equal(t, hierarchical.Mir, sn2[0].Consensus)

	t.Log("[*] miner A in subnet starting")
	mp := hierarchical.MiningParams{}
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner A error:", err)
		cancel()
		return
	}

	t.Log("[*] miner B in subnet starting")
	mp = hierarchical.MiningParams{}
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, false, &mp)
	if err != nil {
		t.Error("subnet miner B error:", err)
		cancel()
		return
	}

	t.Log("[*] stop miner A in the subnet")
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, true, &mp)
	require.NoError(t, err)

	t.Log("[*] stop miner B in the subnet")
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, true, &mp)
	require.NoError(t, err)

	time.Sleep(10 * time.Second)
	t.Logf("[*] test time: %v\n", time.Since(startTime).Seconds())
}

func runTwoNodesCrossMessage(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testCrossMessagesOnTwoNodesMirPow", ts.testCrossMessageOnTwoNodesMirPow)
}

func (ts *eudicoSubnetSuite) testCrossMessageOnTwoNodesMirPow(t *testing.T) {
	var wg sync.WaitGroup

	nodeA, nodeB, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	t.Log("[*] connecting nodes A and B")

	// Fail if genesis blocks are different
	genA, err := nodeA.ChainGetGenesis(ctx)
	require.NoError(t, err)
	genB, err := nodeB.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, genA.String(), genB.String())

	// Fail if no peers

	p, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node A has peers")

	p, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node B has peers")

	ens.Connect(nodeA, nodeB)

	peers, err := nodeA.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node A doesn't have B peer")

	peers, err = nodeB.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node B doesn't have A peer")

	la, err := nodeA.WalletList(ctx)
	require.NoError(t, err)
	if len(la) != 1 {
		t.Fatal("node A's wallet key list is empty")
	}
	minerA := la[0]

	lb, err := nodeB.WalletList(ctx)
	require.NoError(t, err)
	if len(lb) != 1 {
		t.Fatal("node B's wallet key list is empty")
	}
	minerB := lb[0]

	aAddr, err := kit.NodeLibp2pAddr(nodeA)
	require.NoError(t, err)
	bAddr, err := kit.NodeLibp2pAddr(nodeB)
	require.NoError(t, err)

	err = os.Setenv(mir.ValidatorsEnv, fmt.Sprintf("%s@%s,%s@%s",
		"/root:"+minerA.String(), aAddr,
		"/root:"+minerB.String(), bAddr))
	require.NoError(t, err)

	t.Log("[*] running consensus in root net")

	startTime := time.Now()

	wg.Add(2)

	go func() {
		t.Log("[*] miner A in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner A in root net stopped")
		}()
		err := mir.Mine(ctx, minerA, nodeA)
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] miner B in root net starting")
		defer func() {
			wg.Done()
			t.Log("[*] miner B in root net stopped")
		}()
		err := mir.Mine(ctx, minerB, nodeB)
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	t.Log("[*] adding and joining subnets")

	parent := address.RootSubnet
	stake := abi.NewStoragePower(1e8)
	chp := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, minerA, 20, nodeA)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerB, 20, nodeB)
	require.NoError(t, err)

	os.Unsetenv(mir.ValidatorsEnv) // nolint

	balance1, err := nodeA.WalletBalance(ctx, minerA)
	require.NoError(t, err)
	t.Logf("[*] miner %s balance: %d", minerA, balance1)

	balance2, err := nodeB.WalletBalance(ctx, minerB)
	require.NoError(t, err)
	t.Logf("[*] miner %s balance: %d", minerB, balance2)

	// First subnet created on node A.
	subnetParams := &hierarchical.SubnetParams{
		Addr:             minerA,
		Parent:           parent,
		Name:             "subnetA",
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           hierarchical.PoW,
			MinValidators: 0,
			DelegMiner:    minerA,
		},
	}
	subnetAActor, err := nodeA.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetAAddr := address.NewSubnetID(parent, subnetAActor)
	t.Log("[*] subnet A addr:", subnetAAddr)

	networkAName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkAName)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	sc1, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAAddr, "")
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, sc1, 1, 100, false)
	require.NoError(t, err)

	// Second subnet created on node B.
	subnetParams = &hierarchical.SubnetParams{
		Addr:             minerB,
		Parent:           parent,
		Name:             "subnetB",
		Stake:            stake,
		CheckpointPeriod: chp,
		Consensus: hierarchical.ConsensusParams{
			Alg:           hierarchical.PoW,
			MinValidators: 0,
			DelegMiner:    minerA,
		},
	}
	subnetBActor, err := nodeB.AddSubnet(ctx, subnetParams)
	require.NoError(t, err)

	subnetBAddr := address.NewSubnetID(parent, subnetBActor)
	t.Log("[*] subnet B addr:", subnetBAddr)

	networkBName, err := nodeB.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkBName)

	_, err = nodeB.StateLookupID(ctx, minerB, types.EmptyTSK)
	require.NoError(t, err)

	sc2, err := nodeB.JoinSubnet(ctx, minerB, big.Int(val), subnetBAddr, "")
	require.NoError(t, err)

	_, err = nodeB.StateWaitMsg(ctx, sc2, 1, 100, false)
	require.NoError(t, err)

	t.Log("[*] listing subnets")
	sn1, err := nodeA.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 2, len(sn1))

	sn2, err := nodeB.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 2, len(sn2))

	t.Log("[*] run PoW in each subnet")

	t.Log("[*] subnet A miner starting")
	mp := hierarchical.MiningParams{}
	err = nodeA.MineSubnet(ctx, minerA, subnetAAddr, false, &mp)
	if err != nil {
		t.Error(err)
		cancel()
		return
	}

	t.Log("[*] subnet B miner starting")
	mp = hierarchical.MiningParams{}
	err = nodeB.MineSubnet(ctx, minerB, subnetBAddr, false, &mp)
	if err != nil {
		t.Error(err)
		cancel()
		return
	}

	// Sending a message from subnet A to subnet B
	subnetBNewAddr, err := nodeB.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf("[*] subnet B new addr: %s", subnetBNewAddr)

	t.Log("[*] funding subnet A")
	injectedFils := big.Int(types.MustParseFIL("3"))
	_, err = nodeA.FundSubnet(ctx, minerA, subnetAAddr, injectedFils)
	require.NoError(t, err)

	_, err = kit.WaitSubnetActorBalance(ctx, subnetAAddr, minerA, injectedFils, nodeA)
	require.NoError(t, err)

	aa, err := nodeA.SubnetStateGetActor(ctx, subnetAAddr, minerA, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] subnet A %s addr balance: %d", subnetAAddr, aa.Balance)
	require.Equal(t, 0, big.Cmp(injectedFils, aa.Balance))

	// Send node A message to new address.
	t.Log("[*] sending a cross-subnet message")

	sentFils := big.Int(types.MustParseFIL("3"))
	require.NoError(t, err)

	var params lcli.SendParams
	params.Method = builtin.MethodSend
	params.To = subnetBNewAddr
	params.From = minerA
	params.Val = sentFils
	proto, err := kit.MessageForSend(ctx, nodeA, params)
	require.NoError(t, err)

	crossParams := &sca.CrossMsgParams{
		Destination: subnetBAddr,
		Msg:         proto.Message,
	}
	serparams, err := actors.SerializeParams(crossParams)
	require.NoError(t, err)

	msg, err := nodeA.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   params.From,
		Value:  params.Val,
		Method: sca.Methods.SendCross,
		Params: serparams,
	}, nil)
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	msg, err = nodeA.MpoolPushMessage(ctx, &types.Message{
		To:    subnetBNewAddr,
		From:  params.From,
		Value: sentFils,
	}, nil)
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)

	_, err = kit.WaitSubnetActorBalance(ctx, subnetBAddr, subnetBNewAddr, sentFils, nodeB)
	require.NoError(t, err)

	ba, err := nodeB.SubnetStateGetActor(ctx, subnetBAddr, subnetBNewAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] subnet B %s new addr balance: %d", subnetBNewAddr, ba.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, ba.Balance))

	t.Log("[*] stop miner A in the subnet")
	err = nodeA.MineSubnet(ctx, minerA, subnetAAddr, true, &mp)
	require.NoError(t, err)

	t.Log("[*] stop miner B in subnet")
	err = nodeB.MineSubnet(ctx, minerB, subnetBAddr, true, &mp)
	require.NoError(t, err)

	t.Logf("[*] test time: %v\n", time.Since(startTime).Seconds())
}
