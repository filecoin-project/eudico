//stm: #integration
package itests

import (
	"context"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"testing"
	"time"

	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestEudicoSubnetConsensus(t *testing.T) {
	t.Run(":root/pow-subnet/pow", func(t *testing.T) {
		runEudicoSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTSPoW())
	})
	/*
		t.Run(":root/pow-subnet/tendermint", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTendermint())
		})

		t.Run(":root/pow-subnet/delegated", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetDelegated())
		})

		t.Run(":root/delegated-subnet/pow", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetTSPoW())
		})

		t.Run(":root/delegated-subnet/tendermint", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetTendermint())
		})

		t.Run(":root/delegated-subnet/delegated", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetDelegated())
		})

		t.Run(":root/tendermint-subnet/delegated", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTendermint(), kit.SubnetDelegated())
		})

		t.Run(":root/tendermint-subnet/pow", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTendermint(), kit.SubnetTSPoW())
		})

	*/
}

func TestFilcnsSubnetConsensus(t *testing.T) {
	t.Run(":root/filcns-subnet/delegated", func(t *testing.T) {
		runFilcnsSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootFilcns(), kit.SubnetDelegated())
	})
}

func runEudicoSubnetConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetConsensusSuite{opts: opts}

	//t.Run("testEudicoBasicFlow", ts.testBasicSubnetFlow)
	t.Run("testEudicoBasicFlow", ts.testBasicSubnetFlow)
}

func runFilcnsSubnetConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetConsensusSuite{opts: opts}

	//t.Run("testEudicoBasicFlow", ts.testBasicSubnetFlow)
	t.Run("testFilcnsRootBasicFlow", ts.testFilcnsBasicSubnetFlow)
}

type eudicoSubnetConsensusSuite struct {
	opts []interface{}
}

func (ts *eudicoSubnetConsensusSuite) testFilcnsBasicSubnetFlow(t *testing.T) {
	startTime := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup

	full, filcnsMiner, ens := kit.EudicoEnsembleMinimal(t, ts.opts...)

	_, subnetMinerType := kit.EudicoMiners(t, ts.opts...)

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)
	t.Logf("[*] Wallet addr: %s", addr)

	newAddr, err := full.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf("[*] Wallet new addr: %s", newAddr)

	// Start mining

	bm := kit.NewBlockMiner(t, filcnsMiner)
	bm.MineBlocks(ctx, 1*time.Second)

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

	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, uint64(subnetMinerType), minerStake, checkPeriod, addr)
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

	sc, err := full.JoinSubnet(ctx, addr, big.Int(val), subnetAddr)
	require.NoError(t, err)
	t1 := time.Now()
	c, err := full.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)
	t.Logf("the message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	// AddSubnet only deploys the subnet actor. The subnet will only be listed after joining the subnet

	var st sca.SCAState
	act, err := full.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	require.NoError(t, err)
	bs := blockstore.NewAPIBlockstore(full)
	cst := cbor.NewCborStore(bs)
	s := adt.WrapStore(ctx, cst)
	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)
	sn, err := sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Status)

	go full.MineSubnet(ctx, addr, subnetAddr, false)

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
	proto, err := messageForSend(ctx, full, params)
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
	t.Logf("the cross message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	msg, err = full.MpoolPushMessage(ctx, &types.Message{
		To:    newAddr,
		From:  params.From,
		Value: sentFils,
	}, nil)
	require.NoError(t, err)

	t1 = time.Now()
	c, err = full.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)
	t.Logf("the message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	t1 = time.Now()
	bl, err := kit.WaitSubnetActorBalance(ctx, subnetAddr, addr, injectedFils, 100, full)
	require.NoError(t, err)
	t.Logf("sent funds in %v sec and %d blocks", time.Since(t1).Seconds(), bl)

	a, err := full.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("%s addr balance: %d", addr, a.Balance)
	require.Equal(t, 0, big.Cmp(injectedFils, a.Balance))

	a, err = full.SubnetStateGetActor(ctx, subnetAddr, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("%s new addr balance: %d", newAddr, a.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, a.Balance))

	// Release funds

	t.Log("[*] Releasing funds")
	releasedFils := big.Int(types.MustParseFIL("2"))
	releaseCid, err := full.ReleaseFunds(ctx, newAddr, subnetAddr, releasedFils)
	require.NoError(t, err)

	c, err = full.SubnetStateWaitMsg(ctx, subnetAddr, releaseCid, 1, 100, false)
	require.NoError(t, err)
	t.Logf("the release message was found in %d epoch of subnet", c.Height)

	t1 = time.Now()
	bl, err = kit.WaitSubnetActorBalance(ctx, parent, newAddr, big.Add(sentFils, releasedFils), 100, full)
	require.NoError(t, err)
	t.Logf("released funds in %v sec and %d blocks", time.Since(t1).Seconds(), bl)

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

	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)
	sn, err = sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Status)

	err = ens.Stop()
	require.NoError(t, err)

	t.Logf("Test time: %v\n", time.Since(startTime).Seconds())
}

func (ts *eudicoSubnetConsensusSuite) testBasicSubnetFlow(t *testing.T) {
	startTime := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup

	full, ens := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

	rootMiner, subnetMinerType := kit.EudicoMiners(t, ts.opts...)

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)
	t.Logf("wallet addr: %s", addr)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(l))

	newAddr, err := full.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf("wallet new addr: %s", newAddr)

	l, err = full.WalletList(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, len(l))

	// Start mining

	go rootMiner(ctx, addr, full)

	t.Log("[*] Adding and joining the subnet")

	parent := address.RootSubnet
	subnetName := "testSubnet"
	minerStake := abi.NewStoragePower(1e8)
	checkPeriod := abi.ChainEpoch(10)

	err = kit.WaitForBalance(ctx, addr, 10, full)
	require.NoError(t, err)

	balance, err := full.WalletBalance(ctx, addr)
	require.NoError(t, err)
	t.Logf("%s balance: %d", addr, balance)

	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, uint64(subnetMinerType), minerStake, checkPeriod, addr)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)

	t.Log("subnet addr:", subnetAddr)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = full.StateLookupID(ctx, addr, types.EmptyTSK)
	require.NoError(t, err)

	sc, err := full.JoinSubnet(ctx, addr, big.Int(val), subnetAddr)
	require.NoError(t, err)
	t1 := time.Now()
	c, err := full.StateWaitMsg(ctx, sc, 1, 100, false)
	require.NoError(t, err)
	t.Logf("the message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	// AddSubnet only deploys the subnet actor. The subnet will only be listed after joining the subnet

	var st sca.SCAState
	act, err := full.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	require.NoError(t, err)
	bs := blockstore.NewAPIBlockstore(full)
	cst := cbor.NewCborStore(bs)
	s := adt.WrapStore(ctx, cst)
	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)
	sn, err := sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Status)

	go full.MineSubnet(ctx, addr, subnetAddr, false)

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
	proto, err := messageForSend(ctx, full, params)
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
	t.Logf("the cross message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	msg, err = full.MpoolPushMessage(ctx, &types.Message{
		To:    newAddr,
		From:  params.From,
		Value: sentFils,
	}, nil)
	require.NoError(t, err)

	t1 = time.Now()
	c, err = full.StateWaitMsg(ctx, msg.Cid(), 1, 100, false)
	require.NoError(t, err)
	t.Logf("the message was found in %d epoch of root in %v sec", c.Height, time.Since(t1).Seconds())

	t1 = time.Now()
	bl, err := kit.WaitSubnetActorBalance(ctx, subnetAddr, addr, injectedFils, 100, full)
	require.NoError(t, err)
	t.Logf("sent funds in %v sec and %d blocks", time.Since(t1).Seconds(), bl)

	a, err := full.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("%s addr balance: %d", addr, a.Balance)
	require.Equal(t, 0, big.Cmp(injectedFils, a.Balance))

	a, err = full.SubnetStateGetActor(ctx, subnetAddr, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("%s new addr balance: %d", newAddr, a.Balance)
	require.Equal(t, 0, big.Cmp(sentFils, a.Balance))

	// Release funds

	t.Log("[*] Releasing funds")
	releasedFils := big.Int(types.MustParseFIL("2"))
	releaseCid, err := full.ReleaseFunds(ctx, newAddr, subnetAddr, releasedFils)
	require.NoError(t, err)

	c, err = full.SubnetStateWaitMsg(ctx, subnetAddr, releaseCid, 1, 100, false)
	require.NoError(t, err)
	t.Logf("the release message was found in %d epoch of subnet", c.Height)

	t1 = time.Now()
	bl, err = kit.WaitSubnetActorBalance(ctx, parent, newAddr, big.Add(sentFils, releasedFils), 100, full)
	require.NoError(t, err)
	t.Logf("released funds in %v sec and %d blocks", time.Since(t1).Seconds(), bl)

	a, err = full.StateGetActor(ctx, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("new wallet addr: %s", newAddr)
	t.Logf("%s new addr balance: %d", addr, a.Balance)
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
			t.Logf("mined a block: %d", b[0].Val.Height())
		default:
			t.Log("stopped the miner eventually")
			notStopped = false
		}
	}

	// Leaving the subnet

	t.Log("[*] Leaving the subnet")
	_, err = full.LeaveSubnet(ctx, addr, subnetAddr)
	require.NoError(t, err)

	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)
	sn, err = sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Status)

	err = ens.Stop()
	require.NoError(t, err)

	t.Logf("Test time: %v\n", time.Since(startTime).Seconds())
}

// TODO: use MessageForSend from cli package.
func messageForSend(ctx context.Context, s api.FullNode, params lcli.SendParams) (*api.MessagePrototype, error) {
	if params.From == address.Undef {
		defaddr, err := s.WalletDefaultAddress(ctx)
		if err != nil {
			return nil, err
		}
		params.From = defaddr
	}

	msg := types.Message{
		From:  params.From,
		To:    params.To,
		Value: params.Val,

		Method: params.Method,
		Params: params.Params,
	}

	if params.GasPremium != nil {
		msg.GasPremium = *params.GasPremium
	} else {
		msg.GasPremium = types.NewInt(0)
	}
	if params.GasFeeCap != nil {
		msg.GasFeeCap = *params.GasFeeCap
	} else {
		msg.GasFeeCap = types.NewInt(0)
	}
	if params.GasLimit != nil {
		msg.GasLimit = *params.GasLimit
	} else {
		msg.GasLimit = 0
	}
	validNonce := false
	if params.Nonce != nil {
		msg.Nonce = *params.Nonce
		validNonce = true
	}

	prototype := &api.MessagePrototype{
		Message:    msg,
		ValidNonce: validNonce,
	}
	return prototype, nil
}
