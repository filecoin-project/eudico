//stm: #integration
package itests

import (
	"context"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	lcli "github.com/filecoin-project/lotus/cli"
	"testing"
	"time"

	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestEudicoSubnetConsensus(t *testing.T) {
	t.Run("subnet", func(t *testing.T) {
		runSubnetConsensusTests(t, kit.ThroughRPC(), kit.TSPoW())
	})
}

type eudicoSubnetConsensusSuite struct {
	opts []interface{}
}

func runSubnetConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetConsensusSuite{opts: opts}

	t.Run("testBasicInitialization", ts.testBasicSubnetFlow)
}

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

func (ts *eudicoSubnetConsensusSuite) testBasicSubnetFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, _, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	go tspow.Mine(ctx, l[0], full)

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)

	parent := address.RootSubnet
	subnetName := "testSubnet"
	consensus := hierarchical.PoW
	minerStake := abi.NewStoragePower(1e8) // TODO: Make this value configurable in a flag/argument
	checkPeriod := abi.ChainEpoch(10)
	delegMiner := hierarchical.SubnetCoordActorAddr

	wait := true
	for wait {
		balance, err := full.WalletBalance(ctx, addr)
		require.NoError(t, err)
		t.Log("\t>>>>> balance:", balance)
		time.Sleep(time.Second * 3)
		m := types.FromFil(3)
		if big.Cmp(balance, m) == 1 {
			wait = false
		}

	}

	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, uint64(consensus), minerStake, checkPeriod, delegMiner)
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
	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.SubnetChainHead(ctx, subnetAddr)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	<-newHeads

	h2, err := full.SubnetChainHead(ctx, subnetAddr)
	require.NoError(t, err)
	require.Greater(t, int64(h2.Height()), int64(h1.Height()))

	<-newHeads

	h3, err := full.SubnetChainHead(ctx, subnetAddr)
	require.NoError(t, err)
	require.Greater(t, int64(h3.Height()), int64(h2.Height()))

	// Inject new funds to the address in a subnet

	t.Log("\t>>>>> funding yourself")
	injectedFils, err := types.ParseFIL("3")
	require.NoError(t, err)

	_, err = full.FundSubnet(ctx, addr, subnetAddr, big.Int(injectedFils))
	require.NoError(t, err)

	// Send a message to yourself
	t.Log("\t>>>>> sending a message")

	sentFils, err := types.ParseFIL("2")
	require.NoError(t, err)

	var params lcli.SendParams
	params.Method = abi.MethodNum(uint64(builtin.MethodSend))
	params.To = addr
	params.From = addr
	params.Val = abi.TokenAmount(sentFils)
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

	c, err := full.StateWaitMsg(ctx, msg.Cid(), 10, 100, false)
	require.NoError(t, err)
	t.Logf("\t>>>>> the message was found in %d epoch:", c.Height)

	a, err := full.SubnetGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	require.NoError(t, err)
	t.Logf("\t>>>>> balance: %d", a.Balance)
	//require.Equal(t, types.BigAdd(types.BigInt(injectedFils), types.BigInt(sentFils)), a.Balance)
	require.Equal(t, abi.TokenAmount(types.MustParseFIL("5")), a.Balance)

	// Stop mining

	t.Log("\t>>>>> stop mining")
	err = full.MineSubnet(ctx, addr, subnetAddr, true)
	require.NoError(t, err)

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
}
