package sca_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	abi "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	atomic "github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	types "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v7/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestAtomicExec(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	caller := tutil.NewIDAddr(t, 101)
	other := tutil.NewIDAddr(t, 102)

	t.Log("init new atomic execution")
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	params := &actor.AtomicExecParams{
		Msgs:   execMsgs(t, other),
		Inputs: lockedStates(t, caller, other),
	}
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
		ID:     execCid.String(),
		Output: *output,
	}
	ret = rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	require.Equal(t, ret.(*actor.SubmitOutput).Status, actor.ExecInitialized)

	t.Log("fail if resubmission or caller not involved")
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	})
	stranger := tutil.NewIDAddr(t, 103)
	rt.SetCaller(stranger, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	})

	t.Log("submitting the wrong output fails")
	rt.SetCaller(other, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		c, _ := abi.CidBuilder.Sum([]byte("test1"))
		output, err := atomic.WrapLockableState(&replace.Owners{M: map[string]cid.Cid{other.String(): c}})
		require.NoError(t, err)
		ps := &actor.SubmitExecParams{
			ID:     execCid.String(),
			Output: *output,
		}
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, ps)
	})

	t.Log("execution succeeds and no new submissions accepted")
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	ret = rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	require.Equal(t, ret.(*actor.SubmitOutput).Status, actor.ExecSuccess)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	})
}

func TestAbort(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	caller := tutil.NewIDAddr(t, 101)
	other := tutil.NewIDAddr(t, 102)

	t.Log("init new atomic execution")
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	params := &actor.AtomicExecParams{
		Msgs:   execMsgs(t, other),
		Inputs: lockedStates(t, caller, other),
	}
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
		ID:    execCid.String(),
		Abort: true,
	}
	ret = rt.Call(h.SubnetCoordActor.SubmitAtomicExec, oparams)
	require.Equal(t, ret.(*actor.SubmitOutput).Status, actor.ExecAborted)
}
func execMsgs(t *testing.T, addr address.Address) []types.Message {
	return []types.Message{
		{
			From:       addr,
			To:         addr,
			Value:      abi.NewTokenAmount(0),
			Method:     replace.MethodOwn,
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

func lockedStates(t *testing.T, caller, other address.Address) map[string]atomic.LockedState {
	c1, _ := abi.CidBuilder.Sum([]byte("test1"))
	c2, _ := abi.CidBuilder.Sum([]byte("test2"))
	own1 := &replace.Owners{M: map[string]cid.Cid{caller.String(): c1}}
	own2 := &replace.Owners{M: map[string]cid.Cid{other.String(): c2}}
	wown1, err := atomic.WrapLockableState(own1)
	require.NoError(t, err)
	wown2, err := atomic.WrapLockableState(own2)
	require.NoError(t, err)
	return map[string]atomic.LockedState{
		caller.String(): *wown1,
		other.String():  *wown2,
	}
}
