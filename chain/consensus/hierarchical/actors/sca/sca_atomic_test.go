package sca_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	abi "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v7/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/actors"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	atomic "github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	types "github.com/filecoin-project/lotus/chain/types"
)

func TestAtomicExec(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	caller := tutil.NewIDAddr(t, 101)
	other := tutil.NewIDAddr(t, 102)
	callerSecp := tutil.NewSECP256K1Addr(h.t, caller.String())
	otherSecp := tutil.NewSECP256K1Addr(h.t, other.String())

	snAddr1 := tutil.NewIDAddr(t, 1000)
	sn1 := h.registerSubnet(rt, address.RootSubnet, snAddr1)
	snAddr2 := tutil.NewIDAddr(t, 1001)
	sn2 := h.registerSubnet(rt, address.RootSubnet, snAddr2)

	t.Log("init new atomic execution")
	params := &actor.AtomicExecParams{
		Msgs:   execMsgs(t, other),
		Inputs: lockedStates(t, sn1, sn2, caller, other),
	}
	// NOTE: The order of this expectSends can potentially make this test flaky according to the order
	// in which the map of addresses of params is read. This can be fix and is not critical.
	rt.ExpectSend(caller, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &callerSecp, exitcode.Ok)
	rt.ExpectSend(other, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &otherSecp, exitcode.Ok)
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	ret := rt.Call(h.SubnetCoordActor.InitAtomicExec, params)
	st := getState(rt)
	execCid := ret.(*atomic.LockedOutput).Cid
	exec, found, err := st.GetAtomicExec(adt.AsStore(rt), execCid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, &exec.Params, params)
	require.Equal(t, exec.Status, actor.ExecInitialized)

	t.Log("try initializing it again")
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.InitAtomicExec, params)
	})

	t.Log("caller submits output")
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	cidOut, _ := abi.CidBuilder.Sum([]byte("outputTest"))
	output, err := atomic.WrapLockableState(&replace.Owners{M: map[string]cid.Cid{other.String(): cidOut}})
	require.NoError(t, err)
	oparams := &actor.SubmitExecParams{
		Cid:    execCid.String(),
		Output: *output,
	}
	rt.ExpectSend(caller, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &callerSecp, exitcode.Ok)
	ret = rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	require.Equal(t, ret.(*actor.SubmitOutput).Status, actor.ExecInitialized)

	t.Log("fail if resubmission or caller not involved")
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(caller, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &callerSecp, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	})
	stranger := tutil.NewIDAddr(t, 103)
	strangerSecp := tutil.NewSECP256K1Addr(h.t, stranger.String())
	rt.SetCaller(stranger, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(stranger, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &strangerSecp, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	})

	t.Log("submitting the wrong output fails")
	rt.SetCaller(other, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(other, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &otherSecp, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		c, _ := abi.CidBuilder.Sum([]byte("test1"))
		output, err := atomic.WrapLockableState(&replace.Owners{M: map[string]cid.Cid{other.String(): c}})
		require.NoError(t, err)
		ps := &actor.SubmitExecParams{
			Cid:    execCid.String(),
			Output: *output,
		}
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, ps)
	})

	t.Log("execution succeeds and no new submissions accepted")
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(other, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &otherSecp, exitcode.Ok)
	ret = rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	require.Equal(t, ret.(*actor.SubmitOutput).Status, actor.ExecSuccess)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(other, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &otherSecp, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	})
	st = getState(rt)
	exec, found, err = st.GetAtomicExec(adt.AsStore(rt), execCid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, exec.Status, actor.ExecSuccess)

	t.Log("check propagation messages in top-down message")
	sh, found := h.getSubnet(rt, sn1)
	require.True(h.t, found)
	msg, found, err := sh.GetTopDownMsg(adt.AsStore(rt), 0)
	require.NoError(h.t, err)
	require.True(h.t, found)
	exp, err := address.NewHAddress(address.RootSubnet, builtin.SystemActorAddr)
	require.NoError(t, err)
	require.Equal(h.t, msg.From, exp)
	exp, err = address.NewHAddress(sn1, act1(t))
	require.NoError(t, err)
	require.Equal(h.t, msg.To, exp)
	require.Equal(h.t, msg.Method, atomic.MethodUnlock)

	sh, found = h.getSubnet(rt, sn2)
	require.True(h.t, found)
	msg, found, err = sh.GetTopDownMsg(adt.AsStore(rt), 0)
	require.NoError(h.t, err)
	require.True(h.t, found)
	exp, err = address.NewHAddress(address.RootSubnet, builtin.SystemActorAddr)
	require.NoError(t, err)
	require.Equal(h.t, msg.From, exp)
	exp, err = address.NewHAddress(sn2, act2(t))
	require.NoError(t, err)
	require.Equal(h.t, msg.To, exp)
	require.Equal(h.t, msg.Method, atomic.MethodUnlock)

	t.Log("check that we are propagating the right params")
	inputMsg := execMsgs(t, other)[0]
	lparams, err := atomic.WrapSerializedParams(inputMsg.Method, inputMsg.Params)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error wrapping serialized lock params")
	uparams, err := atomic.WrapSerializedUnlockParams(lparams, output.S)
	require.NoError(t, err)
	enc, err := actors.SerializeParams(uparams)
	require.NoError(t, err)
	require.Equal(t, enc, msg.Params)

	t.Log("Init new execution and list atomic executions for caller")
	params2 := &actor.AtomicExecParams{
		Msgs:   execMsgs(t, stranger),
		Inputs: lockedStates(t, sn1, sn2, caller, stranger),
	}
	// NOTE: The order of this expectSends can potentially make this test flaky according to the order
	// in which the map of addresses of params is read. This can be fix and is not critical.
	rt.ExpectSend(caller, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &callerSecp, exitcode.Ok)
	rt.ExpectSend(stranger, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &strangerSecp, exitcode.Ok)
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	ret = rt.Call(h.SubnetCoordActor.InitAtomicExec, params2)
	st = getState(rt)
	execCid2 := ret.(*atomic.LockedOutput).Cid
	exec, found, err = st.GetAtomicExec(adt.AsStore(rt), execCid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, &exec.Params, params)
	require.Equal(t, exec.Status, actor.ExecSuccess)
	exec2, found, err := st.GetAtomicExec(adt.AsStore(rt), execCid2)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, &exec2.Params, params2)
	require.Equal(t, exec2.Status, actor.ExecInitialized)

	m, err := st.ListExecs(rt.AdtStore(), callerSecp)
	require.NoError(t, err)
	require.Equal(t, len(m), 2)
	require.Equal(t, m[execCid].Params, exec.Params)
	require.Equal(t, m[execCid2].Params, exec2.Params)

}

func TestAbort(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	caller := tutil.NewIDAddr(t, 101)
	other := tutil.NewIDAddr(t, 102)
	callerSecp := tutil.NewSECP256K1Addr(h.t, caller.String())
	otherSecp := tutil.NewSECP256K1Addr(h.t, other.String())

	snAddr1 := tutil.NewIDAddr(t, 1000)
	sn1 := h.registerSubnet(rt, address.RootSubnet, snAddr1)
	snAddr2 := tutil.NewIDAddr(t, 1001)
	sn2 := h.registerSubnet(rt, address.RootSubnet, snAddr2)

	t.Log("init new atomic execution")
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	params := &actor.AtomicExecParams{
		Msgs:   execMsgs(t, other),
		Inputs: lockedStates(t, sn1, sn2, caller, other),
	}
	rt.ExpectSend(caller, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &callerSecp, exitcode.Ok)
	rt.ExpectSend(other, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &otherSecp, exitcode.Ok)
	ret := rt.Call(h.SubnetCoordActor.InitAtomicExec, params)
	st := getState(rt)
	execCid := ret.(*atomic.LockedOutput).Cid
	exec, found, err := st.GetAtomicExec(adt.AsStore(rt), execCid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, &exec.Params, params)
	require.Equal(t, exec.Status, actor.ExecInitialized)

	t.Log("caller aborts execution, no more submissions allowed")
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	oparams := &actor.SubmitExecParams{
		Cid:   execCid.String(),
		Abort: true,
	}
	rt.ExpectSend(caller, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &callerSecp, exitcode.Ok)
	ret = rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	st = getState(rt)
	require.Equal(t, ret.(*actor.SubmitOutput).Status, actor.ExecAborted)
	exec, found, err = st.GetAtomicExec(adt.AsStore(rt), execCid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, &exec.Params, params)
	require.Equal(t, exec.Status, actor.ExecAborted)

	t.Log("check propagation messages in top-down message")
	sh, found := h.getSubnet(rt, sn1)
	require.True(h.t, found)
	msg, found, err := sh.GetTopDownMsg(adt.AsStore(rt), 0)
	require.NoError(h.t, err)
	require.True(h.t, found)
	exp, err := address.NewHAddress(address.RootSubnet, builtin.SystemActorAddr)
	require.NoError(t, err)
	require.Equal(h.t, msg.From, exp)
	exp, err = address.NewHAddress(sn1, act1(t))
	require.NoError(t, err)
	require.Equal(h.t, msg.To, exp)
	require.Equal(h.t, msg.Method, atomic.MethodAbort)

	sh, found = h.getSubnet(rt, sn2)
	require.True(h.t, found)
	msg, found, err = sh.GetTopDownMsg(adt.AsStore(rt), 0)
	require.NoError(h.t, err)
	require.True(h.t, found)
	exp, err = address.NewHAddress(address.RootSubnet, builtin.SystemActorAddr)
	require.NoError(t, err)
	require.Equal(h.t, msg.From, exp)
	exp, err = address.NewHAddress(sn2, act2(t))
	require.NoError(t, err)

	require.Equal(h.t, msg.To, exp)
	require.Equal(h.t, msg.Method, atomic.MethodAbort)
}

func execMsgs(t *testing.T, addr address.Address) []types.Message {
	return []types.Message{
		{
			From:       addr,
			To:         addr,
			Value:      abi.NewTokenAmount(0),
			Method:     replace.MethodReplace,
			Params:     nil,
			GasPremium: big.Zero(),
			GasFeeCap:  big.Zero(),
			GasLimit:   0,
		},
		{
			From:       addr,
			To:         addr,
			Value:      abi.NewTokenAmount(0),
			Method:     replace.MethodReplace,
			Params:     nil,
			GasPremium: big.Zero(),
			GasFeeCap:  big.Zero(),
			GasLimit:   0,
		},
	}
}

func act1(t *testing.T) address.Address {
	return tutil.NewIDAddr(t, 900)
}

func act2(t *testing.T) address.Address {
	return tutil.NewIDAddr(t, 901)
}

func lockedStates(t *testing.T, sn1, sn2 address.SubnetID, caller, other address.Address) map[string]actor.LockedState {
	c1, _ := abi.CidBuilder.Sum([]byte("test1"))
	c2, _ := abi.CidBuilder.Sum([]byte("test2"))
	addr1, err := address.NewHAddress(sn1, caller)
	require.NoError(t, err)
	addr2, err := address.NewHAddress(sn2, other)
	require.NoError(t, err)
	return map[string]actor.LockedState{
		addr1.String(): {Cid: c1.String(), Actor: act1(t)},
		addr2.String(): {Cid: c2.String(), Actor: act2(t)},
	}
}
