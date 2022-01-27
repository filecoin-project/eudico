package sca_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	abi "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestFund(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	SubnetActorAddr := tutil.NewIDAddr(t, 101)

	t.Log("register new subnet successfully")
	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret := rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	shid := address.SubnetID("/root/f0101")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(1))
	// Verify instantiated subnet
	sh, found := h.getSubnet(rt, shid)
	nn1 := sh.ID
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0101")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("inject some funds in subnet")
	funder := tutil.NewIDAddr(h.t, 1000)
	value = abi.NewTokenAmount(1e18)
	fund(h, rt, nn1, funder, value, 1, value, value)
	newfunder := tutil.NewIDAddr(h.t, 1001)
	fund(h, rt, nn1, newfunder, value, 2, big.Mul(big.NewInt(2), value), value)
	fund(h, rt, nn1, newfunder, value, 3, big.Mul(big.NewInt(3), value), big.Mul(big.NewInt(2), value))

	t.Log("get cross messages from nonce")
	sh, _ = h.getSubnet(rt, nn1)
	msgs, err := sh.TopDownMsgFromNonce(adt.AsStore(rt), 0)
	require.NoError(h.t, err)
	require.Equal(h.t, len(msgs), 3)
	msgs, err = sh.TopDownMsgFromNonce(adt.AsStore(rt), 2)
	require.NoError(h.t, err)
	require.Equal(h.t, len(msgs), 1)
}

func TestReleaseFunds(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	shid := address.SubnetID("/root/f0101")
	h.constructAndVerifyWithNetworkName(rt, shid)

	t.Log("release some funds from subnet")
	releaser := tutil.NewIDAddr(h.t, 1000)
	value := abi.NewTokenAmount(1e18)
	prev := release(h, rt, shid, releaser, value, 0, cid.Undef)
	release(h, rt, shid, releaser, value, 1, prev)

}

func TestCrossMsg(t *testing.T) {
	// TODO: Test sending a new cross-msg
}

func TestPathDown(t *testing.T) {
	// TODO: Test sending cross message down several subnets
}

func TestApplyMsg(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	h.registerSubnet(rt, address.RootSubnet)
	funder, err := address.NewHAddress(h.sn.Parent(), tutil.NewSECP256K1Addr(h.t, "asd"))
	require.NoError(h.t, err)
	funderID := tutil.NewIDAddr(h.t, 1000)

	// Inject some funds to test circSupply
	t.Log("inject some funds in subnet")
	init := abi.NewTokenAmount(1e18)
	fund(h, rt, h.sn, funderID, init, 1, init, init)
	value := abi.NewTokenAmount(1e17)

	t.Log("apply fund messages")
	for i := 0; i < 5; i++ {
		h.applyFundMsg(rt, funder, value, uint64(i), false)
	}
	// Applying already used nonces or non-subsequent should fail
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyFundMsg(rt, funder, value, 10, true)
	})
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyFundMsg(rt, funder, value, 1, true)
	})

	// Register subnet for update in circulating supply
	releaser, err := address.NewHAddress(h.sn.Parent(), tutil.NewSECP256K1Addr(h.t, "asd"))
	require.NoError(h.t, err)

	t.Log("apply release messages")
	// Three messages with the same nonce
	for i := 0; i < 3; i++ {
		h.applyReleaseMsg(rt, releaser, value, uint64(0))
	}
	// The following with increasing nonces
	for i := 0; i < 3; i++ {
		h.applyReleaseMsg(rt, releaser, value, uint64(i))
	}
	// Check that circ supply is updated successfully.
	sh, found := h.getSubnet(rt, h.sn)
	require.True(h.t, found)
	require.Equal(h.t, sh.CircSupply, big.Sub(init, big.Mul(big.NewInt(6), value)))
	// Trying to release over the circulating supply
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyReleaseMsg(rt, releaser, init, 2)
	})
	// Applying already used nonces or non-subsequent should fail
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyReleaseMsg(rt, releaser, value, 10)
	})
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyReleaseMsg(rt, releaser, value, 1)
	})

}

func (h *shActorHarness) applyFundMsg(rt *mock.Runtime, addr address.Address, value big.Int, nonce uint64, abort bool) {
	rt.SetCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	params := &actor.CrossMsgParams{
		Msg: ltypes.Message{
			To:         addr,
			From:       addr,
			Value:      value,
			Nonce:      nonce,
			Method:     builtin.MethodSend,
			GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
			GasFeeCap:  ltypes.NewInt(0),
			GasPremium: ltypes.NewInt(0),
			Params:     nil,
		},
	}

	rewParams := &reward.FundingParams{
		Addr:  hierarchical.SubnetCoordActorAddr,
		Value: value,
	}
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	if !abort {
		rt.ExpectSend(reward.RewardActorAddr, reward.Methods.ExternalFunding, rewParams, big.Zero(), nil, exitcode.Ok)
		raddr, err := addr.RawAddr()
		require.NoError(h.t, err)
		rt.ExpectSend(raddr, params.Msg.Method, nil, params.Msg.Value, nil, exitcode.Ok)
	}
	rt.Call(h.SubnetCoordActor.ApplyMessage, params)
	rt.Verify()
	st := getState(rt)
	require.Equal(h.t, st.AppliedTopDownNonce, nonce+1)
}

func (h *shActorHarness) applyReleaseMsg(rt *mock.Runtime, addr address.Address, value big.Int, nonce uint64) {
	rt.SetCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt.SetBalance(value)
	from, err := address.NewHAddress(h.sn, builtin.BurntFundsActorAddr)
	require.NoError(h.t, err)
	params := &actor.CrossMsgParams{
		Msg: ltypes.Message{
			To:         addr,
			From:       from,
			Value:      value,
			Nonce:      nonce,
			GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
			GasFeeCap:  ltypes.NewInt(0),
			GasPremium: ltypes.NewInt(0),
			Params:     nil,
		},
	}

	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	rto, err := addr.RawAddr()
	require.NoError(h.t, err)
	rt.ExpectSend(rto, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ApplyMessage, params)
	rt.Verify()
	st := getState(rt)
	require.Equal(h.t, st.AppliedBottomUpNonce, nonce)
}

func release(h *shActorHarness, rt *mock.Runtime, shid address.SubnetID, releaser address.Address, value big.Int, nonce uint64, prevMeta cid.Cid) cid.Cid {
	// Test SECP to use for calling
	testSecp := tutil.NewSECP256K1Addr(h.t, "asd")
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(releaser, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(builtin.BurntFundsActorAddr, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	// Expect a send to get pkey
	rt.ExpectSend(releaser, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &testSecp, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.Release, nil)
	rt.Verify()

	// Check that msgMeta included in checkpoint
	windowCh := currWindowCheckpoint(rt, 0)
	_, chmeta := windowCh.CrossMsgMeta(shid, shid.Parent())
	require.NotNil(h.t, chmeta)
	cidmeta, err := chmeta.Cid()
	require.NoError(h.t, err)
	meta, found := h.getMsgMeta(rt, cidmeta)
	require.True(h.t, found)
	require.Equal(h.t, len(meta.Msgs), int(nonce+1))
	msg := meta.Msgs[nonce]

	// Comes from child
	from, err := address.NewHAddress(shid, builtin.BurntFundsActorAddr)
	require.NoError(h.t, err)
	// Goes to parent
	to, err := address.NewHAddress(shid.Parent(), testSecp)
	require.NoError(h.t, err)
	require.Equal(h.t, msg.From, from)
	// The "to" should have been updated to the secp addr
	require.Equal(h.t, msg.To, to)
	require.Equal(h.t, msg.Value, value)
	require.Equal(h.t, msg.Nonce, nonce)
	// check previous meta is removed
	if prevMeta != cid.Undef {
		_, found := h.getMsgMeta(rt, prevMeta)
		require.False(h.t, found)
	}
	// return cid of meta
	return cidmeta

}

func fund(h *shActorHarness, rt *mock.Runtime, sn address.SubnetID, funder address.Address, value abi.TokenAmount,
	expectedNonce uint64, expectedCircSupply big.Int, expectedAddrFunds abi.TokenAmount) {
	testSecp := tutil.NewSECP256K1Addr(h.t, funder.String())
	rt.SetReceived(value)
	params := &actor.SubnetIDParam{ID: sn.String()}
	rt.SetCaller(funder, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	// Expect a send to get pkey
	rt.ExpectSend(funder, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &testSecp, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.Fund, params)
	rt.Verify()
	sh, found := h.getSubnet(rt, sn)
	require.True(h.t, found)
	require.Equal(h.t, sh.CircSupply, expectedCircSupply)
	require.Equal(h.t, sh.Nonce, expectedNonce)
	require.Equal(h.t, getFunds(h.t, rt, sh, testSecp), expectedAddrFunds)
	msg, found, err := sh.GetTopDownMsg(adt.AsStore(rt), expectedNonce-1)
	require.NoError(h.t, err)
	require.True(h.t, found)
	// TODO: Add additional checks over msg?
	require.Equal(h.t, msg.Value, value)
	// Comes from parent network.
	from, err := address.NewHAddress(sh.ID.Parent(), testSecp)
	require.NoError(h.t, err)
	// Goes to subnet with same address
	to, err := address.NewHAddress(sh.ID, testSecp)
	require.NoError(h.t, err)
	require.Equal(h.t, msg.From, from)
	require.Equal(h.t, msg.To, to)
	require.Equal(h.t, msg.Nonce, expectedNonce-1)
}
