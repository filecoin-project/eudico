//stm: #integration
package itests

import (
	"context"
	"testing"

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
	subnetmgr "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/manager"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestEudicoSubnetConsensus(t *testing.T) {

	t.Run(":root/pow-subnet/pow", func(t *testing.T) {
		runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTSPoW())
	})

	t.Run(":root/pow-subnet/tendermint", func(t *testing.T) {
		runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootTSPoW(), kit.SubnetTendermint())
	})

	/*
		t.Run(":root/delegated-subnet/pow", func(t *testing.T) {
			runSubnetConsensusTests(t, kit.ThroughRPC(), kit.RootDelegated(), kit.SubnetTSPoW())
		})

	*/
}

func runSubnetConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetConsensusSuite{opts: opts}

	t.Run("testBasicFlow", ts.testBasicSubnetFlow)
}

type eudicoSubnetConsensusSuite struct {
	opts []interface{}
}

func (ts *eudicoSubnetConsensusSuite) testBasicSubnetFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup

	full, _, ens := kit.EudicoEnsembleMinimal(t, ts.opts...)

	rootMiner, subnetMinerType := kit.EudicoMiners(t, ts.opts...)

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)
	t.Logf(">>>>> wallet addr: %s", addr)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(l))

	newAddr, err := full.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	t.Logf(">>>>> wallet new addr: %s", newAddr)

	l, err = full.WalletList(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, len(l))

	// Start mining

	go rootMiner(ctx, addr, full)

	parent := address.RootSubnet
	subnetName := "testSubnet"
	minerStake := abi.NewStoragePower(1e8)
	checkPeriod := abi.ChainEpoch(10)
	delegMiner := hierarchical.SubnetCoordActorAddr

	err = kit.WaitForBalance(ctx, addr, 10, full)
	require.NoError(t, err)

	balance, err := full.WalletBalance(ctx, addr)
	require.NoError(t, err)
	t.Logf("\t>>>>> %s balance: %d", addr, balance)

	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, uint64(subnetMinerType), minerStake, checkPeriod, delegMiner)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)

	t.Log("\t>>>>> subnet addr:", subnetAddr)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = full.StateLookupID(ctx, addr, types.EmptyTSK)
	require.NoError(t, err)

	_, err = full.JoinSubnet(ctx, addr, big.Int(val), subnetAddr)
	require.NoError(t, err)

	// AddSubnet only deploys the subnet actor. The subnet will only be listed after joining the subnet.
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

	newHeads, err := full.SubnetChainNotify(ctx, subnetAddr)
	require.NoError(t, err)

	err = kit.PerformHeightCheckForSubnetBlocks(ctx, 4, subnetAddr, newHeads, full)
	require.NoError(t, err)

	// Inject new funds to the own address in the subnet

	t.Log("\t>>>>> funding yourself")
	injectedFils := big.Int(types.MustParseFIL("3"))
	_, err = full.FundSubnet(ctx, addr, subnetAddr, injectedFils)
	require.NoError(t, err)

	// Send a message to the new address
	t.Log("\t>>>>> sending a message")

	sentFils := big.Int(types.MustParseFIL("3"))
	require.NoError(t, err)

	var params lcli.SendParams
	params.Method = abi.MethodNum(uint64(builtin.MethodSend))
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

	msg, err = full.MpoolPushMessage(ctx, &types.Message{
		To:    newAddr,
		From:  params.From,
		Value: big.Int(types.MustParseFIL("3")),
	}, nil)
	require.NoError(t, err)

	c, err := full.StateWaitMsg(ctx, msg.Cid(), 10, 500, false)
	require.NoError(t, err)
	t.Logf("\t>>>>> the cross message was found in %d epoch of root", c.Height)

	err = kit.WaitForFinality(ctx, subnetmgr.FinalityThreshold+20, newHeads)
	require.NoError(t, err)

	a, err := full.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("\t>>>>> %s addr balance: %d", addr, a.Balance)
	require.Equal(t, injectedFils, a.Balance)

	a, err = full.SubnetStateGetActor(ctx, subnetAddr, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("\t>>>>> %s new addr balance: %d", newAddr, a.Balance)
	require.Equal(t, sentFils, a.Balance)

	// Release funds

	t.Log("\t>>>>> release funds")
	releasedFils := big.Int(types.MustParseFIL("2"))
	releaseCid, err := full.ReleaseFunds(ctx, newAddr, subnetAddr, releasedFils)
	require.NoError(t, err)

	c, err = full.SubnetStateWaitMsg(ctx, subnetAddr, releaseCid, 10, 500, false)
	require.NoError(t, err)
	t.Logf("\t>>>>> the release message was found in %d epoch of subnet", c.Height)

	newHeads, err = full.ChainNotify(ctx)
	require.NoError(t, err)

	err = kit.WaitForFinality(ctx, subnetmgr.FinalityThreshold+20, newHeads)
	require.NoError(t, err)

	a, err = full.StateGetActor(ctx, newAddr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("\t>>>>> new wallet addr: %s", newAddr)
	t.Logf("\t>>>>> %s new addr balance: %d", addr, a.Balance)
	require.Equal(t, big.Add(sentFils, releasedFils), a.Balance)

	// Stop mining

	t.Log("\t>>>>> stop mining")
	err = full.MineSubnet(ctx, addr, subnetAddr, true)
	require.NoError(t, err)

	// TODO: is this correct?
	notStopped := true
	for notStopped {
		select {
		case b := <-newHeads:
			t.Logf("\t>>>>> mined a block: %d", b[0].Val.Height())
		default:
			t.Log("\t>>>>> stopped the miner eventually")
			notStopped = false
		}
	}

	// Leaving the subnet

	t.Log("\t>>>>> leaving the subnet")
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
