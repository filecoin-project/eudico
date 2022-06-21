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
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func TestEudicoSubnetSmoke(t *testing.T) {
	t.Run("/root/dummy-/subnet/dummy", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetDummy())
	})
}

func TestEudicoSubnetTwoNodesBasic(t *testing.T) {
	t.Run("/root/mir-/subnet/mir", func(t *testing.T) {
		runSubnetTestsTwoNodes(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetMir())
	})
}

func TestEudicoSubnetTwoNodesCrossMessage(t *testing.T) {
	t.Run("/root/mir-/subnet/pow", func(t *testing.T) {
		runSubnetTwoNodesCrossMessage(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetTSPoW())
	})
}

func TestEudicoSubnetMir(t *testing.T) {
	a, err := kit.GetFreeLocalAddr()
	require.NoError(t, err)

	t.Run("/root/dummy-/subnet/mir", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetMir(), kit.MinValidators(1), kit.ValidatorAddress(a))
	})

	t.Run("/root/mir-/subnet/delegated", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetDelegated())
	})
}

func TestEudicoSubnetOneNodeBasic(t *testing.T) {
	// Filecoin consensus in root

	t.Run("/root/filcns-/subnet/delegated", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootFilcns(), kit.SubnetDelegated())
	})

	t.Run("/root/filcns-/subnet/pow", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootFilcns(), kit.SubnetTSPoW())
	})

	t.Run("/root/delegated-/subnet/pow", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetTSPoW())
	})

	if os.Getenv("TENDERMINT_ITESTS") != "" {
		t.Run("/root/filcns-/subnet/tendermint", func(t *testing.T) {
			runSubnetTests(t, kit.ThroughRPC(), kit.RootFilcns(), kit.SubnetTendermint())
		})
	}

	if os.Getenv("FULL_ITESTS") != "" {

		// PoW in Root

		t.Run("/root/pow-/subnet/pow", func(t *testing.T) {
			runSubnetTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTSPoW())
		})

		if os.Getenv("TENDERMINT_ITESTS") != "" {
			t.Run("/root/pow-/subnet/tendermint", func(t *testing.T) {
				runSubnetTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTendermint())
			})
		}

		t.Run("/root/pow-/subnet/delegated", func(t *testing.T) {
			runSubnetTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetDelegated())
		})

		// Delegated consensus in root

		t.Run("/root/delegated-/subnet/delegated", func(t *testing.T) {
			runSubnetTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetDelegated())
		})

		if os.Getenv("TENDERMINT_ITESTS") != "" {
			t.Run("/root/delegated-/subnet/tendermint", func(t *testing.T) {
				runSubnetTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetTendermint())
			})
		}

		// Tendermint in root

		if os.Getenv("TENDERMINT_ITESTS") != "" {
			t.Run("/root/tendermint-/subnet/delegated", func(t *testing.T) {
				runSubnetTests(t, kit.ThroughRPC(), kit.RootTendermint(), kit.SubnetDelegated())
			})

			t.Run("/root/tendermint-/subnet/pow", func(t *testing.T) {
				runSubnetTests(t, kit.ThroughRPC(), kit.RootTendermint(), kit.SubnetTSPoW())
			})
		}
	}
}

func runSubnetTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testBasicSubnetFlow", ts.testBasicSubnetFlow)
}

type eudicoSubnetSuite struct {
	opts []interface{}
}

func (ts *eudicoSubnetSuite) testBasicSubnetFlow(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, rootMiner, subnetMinerType, ens := kit.EudicoEnsembleTwoMiners(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
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
	minerStake := abi.NewStoragePower(1e8)
	checkPeriod := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, addr, 12, full)
	require.NoError(t, err)

	balance, err := full.WalletBalance(ctx, addr)
	require.NoError(t, err)
	t.Logf("[*] %s balance: %d", addr, balance)

	cns := uint64(subnetMinerType)
	p := &hierarchical.ConsensusParams{
		MinValidators: n,
		DelegMiner:    addr,
	}
	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, cns, minerStake, checkPeriod, p)
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

	t.Log("[*] stopping mining")
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

func runSubnetTestsTwoNodes(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testBasicSubnetFlowTwoNodes", ts.testBasicSubnetFlowTwoNodes)
}

func (ts *eudicoSubnetSuite) testBasicSubnetFlowTwoNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	nodeA, nodeB, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
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

	aAddr, err := kit.GetFreeLocalAddr()
	require.NoError(t, err)
	bAddr, err := kit.GetFreeLocalAddr()
	require.NoError(t, err)

	err = os.Setenv(mir.ValidatorsEnv, fmt.Sprintf("%s@%s,%s@%s",
		"/root:"+minerA.String(), aAddr,
		"/root:"+minerB.String(), bAddr))
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

	t.Log("[*] adding and joining subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	minerStake := abi.NewStoragePower(1e8)
	checkPeriod := abi.ChainEpoch(10)

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

	hp := &hierarchical.ConsensusParams{
		MinValidators: 2,
		DelegMiner:    minerA,
	}
	actorAddr, err := nodeA.AddSubnet(ctx, minerA, parent, subnetName, uint64(hierarchical.Mir), minerStake, checkPeriod, hp)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)
	t.Log("[*] subnet addr:", subnetAddr)

	networkName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = nodeA.StateLookupID(ctx, minerA, types.EmptyTSK)
	require.NoError(t, err)

	saAddr, err := kit.GetFreeLocalAddr()
	require.NoError(t, err)
	sbAddr, err := kit.GetFreeLocalAddr()
	require.NoError(t, err)

	sc, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAddr, saAddr)
	require.NoError(t, err)
	_, err = nodeA.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)

	sc, err = nodeB.JoinSubnet(ctx, minerB, big.Int(val), subnetAddr, sbAddr)
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, sc, 1, 100, false)
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

	err = kit.WaitForBalance(ctx, minerA, 20, nodeA)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, minerB, 20, nodeB)
	require.NoError(t, err)

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

	t.Log("[*] miner A in subnet stopping")
	err = nodeA.MineSubnet(ctx, minerA, subnetAddr, true, &mp)
	require.NoError(t, err)

	t.Log("[*] miner B in subnet stopping")
	err = nodeB.MineSubnet(ctx, minerB, subnetAddr, true, &mp)
	require.NoError(t, err)

	t.Logf("[*] test time: %v\n", time.Since(startTime).Seconds())
}

func runSubnetTwoNodesCrossMessage(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetSuite{opts: opts}

	t.Run("testBasicSubnetFlowTwoNodes", ts.testSubnetTwoNodesCrossMessage)
}

func (ts *eudicoSubnetSuite) testSubnetTwoNodesCrossMessage(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	nodeA, nodeB, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
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

	aAddr, err := kit.GetFreeLocalAddr()
	require.NoError(t, err)
	bAddr, err := kit.GetFreeLocalAddr()
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
	minerStake := abi.NewStoragePower(1e8)
	checkPeriod := abi.ChainEpoch(10)

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

	hp := &hierarchical.ConsensusParams{
		MinValidators: 0,
		DelegMiner:    minerA,
	}

	// First subnet created on node A.
	subnetA := "testSubnetA"
	subnetAActor, err := nodeA.AddSubnet(ctx, minerA, parent, subnetA, uint64(hierarchical.PoW), minerStake, checkPeriod, hp)
	require.NoError(t, err)

	subnetAAddr := address.NewSubnetID(parent, subnetAActor)
	t.Log("[*] subnet A addr:", subnetAAddr)

	networkAName, err := nodeA.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkAName)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = nodeA.StateLookupID(ctx, minerA, types.EmptyTSK)
	require.NoError(t, err)

	sc1, err := nodeA.JoinSubnet(ctx, minerA, big.Int(val), subnetAAddr, "")
	require.NoError(t, err)

	_, err = nodeA.StateWaitMsg(ctx, sc1, 1, 100, false)
	require.NoError(t, err)

	// Second subnet created on node B.
	subnetBName := "testSubnetB"
	subnetBActor, err := nodeB.AddSubnet(ctx, minerB, parent, subnetBName, uint64(hierarchical.PoW), minerStake, checkPeriod, hp)
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

	_, err = nodeA.StateWaitMsg(ctx, sc2, 1, 100, false)
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

	t.Log("[*] miner A in subnet stopping")
	err = nodeA.MineSubnet(ctx, minerA, subnetAAddr, true, &mp)
	require.NoError(t, err)

	t.Log("[*] miner B in subnet stopping")
	err = nodeB.MineSubnet(ctx, minerB, subnetBAddr, true, &mp)
	require.NoError(t, err)

	t.Logf("[*] test time: %v\n", time.Since(startTime).Seconds())
}
