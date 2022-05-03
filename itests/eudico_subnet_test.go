// stm: #integration
package itests

import (
	"context"
	"os"
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

func TestEudicoSubnetMir(t *testing.T) {
	t.Setenv(mir.NodeIDEnv, "0")
	t.Setenv(mir.NodesEnv, "0@127.0.0.1:10000")

	t.Run("/root/mir-/subnet/dummy", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetDummy())
	})

	t.Run("/root/dummy-/subnet/mir", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetMir())
	})
}

func TestEudicoMirStartedViaActor(t *testing.T) {
	t.Run("/root/dummy-/subnet/mir", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetMir(), kit.ValidatorsNumber(1), kit.ValidatorAddress("127.0.0.1:11001"))
	})
}

func TestEudicoSubnet(t *testing.T) {
	t.Setenv(mir.NodeIDEnv, "0")
	t.Setenv(mir.NodesEnv, "0@127.0.0.1:10000")

	// Sanity test with Dummy consensus.

	t.Run("/root/dummy-/subnet/dummy", func(t *testing.T) {
		runSubnetTests(t, kit.ThroughRPC(), kit.RootDummy(), kit.SubnetDummy())
	})

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
		// Mir in Root

		t.Run("/root/mir-/subnet/delegated", func(t *testing.T) {
			runSubnetTests(t, kit.ThroughRPC(), kit.RootMir(), kit.SubnetDelegated())
		})

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

		t.Run("/root/delegated-/subnet/mir", func(t *testing.T) {
			runSubnetTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetMir())
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
	startTime := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stop test and cancel mining, if we receive at least one error from error channel.
	errChan := make(chan error, 1)
	go func() {
		<-errChan
		cancel()
	}()

	full, rootMiner, subnetMinerType, ens := kit.EudicoEnsembleTwoMiners(t, ts.opts...)
	n, valAddr := ens.ValidatorInfo()

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)
	t.Logf("[*] Wallet addr: %s", addr)

	newAddr, err := full.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf("[*] Wallet new addr: %s", newAddr)

	// Start mining in root net: start Filecoin consensus or Eudico consensus.
	switch miner := rootMiner.(type) {
	case *kit.TestMiner:
		bm := kit.NewBlockMiner(t, miner)
		bm.MineBlocks(ctx, 1*time.Second)
	case kit.EudicoRootMiner:
		go func() {
			err := miner(ctx, addr, full)
			if err != nil {
				t.Error(err)
				errChan <- err
				return
			}
		}()
	default:
		t.Fatal("unsupported root consensus")
	}

	t.Log("[*] Adding and joining the subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	minerStake := abi.NewStoragePower(1e8)
	checkPeriod := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, addr, 20, full)
	require.NoError(t, err)

	balance, err := full.WalletBalance(ctx, addr)
	require.NoError(t, err)
	t.Logf("[*] %s balance: %d", addr, balance)

	cns := uint64(subnetMinerType)
	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, cns, minerStake, checkPeriod, addr, n)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)

	networkName, err := full.StateNetworkName(ctx)
	require.NoError(t, err)
	require.Equal(t, dtypes.NetworkName("/root"), networkName)

	t.Log("[*] Subnet addr:", subnetAddr)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = full.StateLookupID(ctx, addr, types.EmptyTSK)
	require.NoError(t, err)

	sc, err := full.JoinSubnet(ctx, addr, big.Int(val), subnetAddr, valAddr)
	require.NoError(t, err)
	t1 := time.Now()
	c, err := full.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)
	t.Logf("[*] the message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	// AddSubnet only deploys the subnet actor. The subnet will only be listed after joining the subnet
	t.Log("[*] Listing subnets")
	sn, err := full.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Subnet.Status)
	require.Equal(t, subnetMinerType, sn[0].Consensus)

	go func() {
		err := full.MineSubnet(ctx, addr, subnetAddr, false)
		if err != nil {
			t.Error(err)
			errChan <- err
			return
		}
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 4, subnetAddr, full)
	require.NoError(t, err)

	// Inject new funds to the own address in the subnet

	t.Log("[*] Funding subnet")
	injectedFils := big.Int(types.MustParseFIL("3"))
	_, err = full.FundSubnet(ctx, addr, subnetAddr, injectedFils)
	require.NoError(t, err)

	// Send a message to the new address
	t.Log("[*] Sending a message")

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

	t1 = time.Now()
	c, err = full.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)
	t.Logf(" [*] the cross message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	msg, err = full.MpoolPushMessage(ctx, &types.Message{
		To:    newAddr,
		From:  params.From,
		Value: sentFils,
	}, nil)
	require.NoError(t, err)

	t1 = time.Now()
	c, err = full.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)
	t.Logf("[*] the message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	t1 = time.Now()
	bl, err := kit.WaitSubnetActorBalance(ctx, subnetAddr, addr, injectedFils, full)
	require.NoError(t, err)
	t.Logf(" [*] Sent funds in %v sec and %d blocks", time.Since(t1).Seconds(), bl)

	a, err := full.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] %s addr balance: %d", addr, a.Balance)
	require.Equal(t, 0, big.Cmp(injectedFils, a.Balance))

	a, err = full.SubnetStateGetActor(ctx, subnetAddr, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] %s new addr balance: %d", newAddr, a.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, a.Balance))

	// Release funds

	t.Log("[*] Releasing funds")
	releasedFils := big.Int(types.MustParseFIL("2"))
	releaseCid, err := full.ReleaseFunds(ctx, newAddr, subnetAddr, releasedFils)
	require.NoError(t, err)

	c, err = full.SubnetStateWaitMsg(ctx, subnetAddr, releaseCid, 1, 100, false)
	require.NoError(t, err)
	t.Logf("[*] The release message was found in %d epoch of subnet", c.Height)

	t1 = time.Now()
	bl, err = kit.WaitSubnetActorBalance(ctx, parent, newAddr, big.Add(sentFils, releasedFils), full)
	require.NoError(t, err)
	t.Logf("[*] Released funds in %v sec and %d blocks", time.Since(t1).Seconds(), bl)

	a, err = full.StateGetActor(ctx, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("[*] New addr: %s", newAddr)
	t.Logf("[*] New addr %s balance: %d", addr, a.Balance)
	require.Equal(t, 0, big.Cmp(a.Balance, big.Add(sentFils, releasedFils)))

	// Stop mining

	t.Log("[*] Stop mining")
	err = full.MineSubnet(ctx, addr, subnetAddr, true)
	require.NoError(t, err)

	newHeads, err := full.SubnetChainNotify(ctx, subnetAddr)
	require.NoError(t, err)

	notStopped := true
	for notStopped {
		select {
		case b := <-newHeads:
			t.Logf("[*] Stopping mining: mined a block: %d", b[0].Val.Height())
		default:
			t.Log("[*] Stopped the miner eventually")
			notStopped = false
		}
	}

	// Leaving the subnet

	t.Log("[*] Leaving the subnet")
	_, err = full.LeaveSubnet(ctx, addr, subnetAddr)
	require.NoError(t, err)

	sn, err = full.ListSubnets(ctx, address.RootSubnet)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Subnet.Status)

	err = ens.Stop()
	require.NoError(t, err)

	t.Logf("[*] Test time: %v\n", time.Since(startTime).Seconds())
}
