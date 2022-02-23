package replace_test

import (
	"testing"

	"github.com/filecoin-project/go-state-types/exitcode"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	atomic "github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, replace.ReplaceActor{})
}

func TestConstruction(t *testing.T) {
	t.Run("simple construction", func(t *testing.T) {
		actor := newHarness(t)
		rt := getRuntime(t)
		actor.constructAndVerify(t, rt)
	})

}

func TestOwn(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	caller := tutil.NewIDAddr(t, 1000)
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test"})
	rt.Verify()

	st := getState(rt)
	owners := st.UnwrapOwners(rt)
	_, ok := owners.M[caller.String()]
	require.True(t, ok)

	rt.ExpectValidateCallerAny()
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test"})
	})

	caller = tutil.NewIDAddr(t, 1001)
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test2"})
	rt.Verify()

}

func TestReplace(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	caller := tutil.NewIDAddr(t, 1000)
	target := tutil.NewIDAddr(t, 1001)

	rt.SetCaller(target, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test1"})

	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test2"})

	st := getState(rt)
	owners := st.UnwrapOwners(rt)
	prev1, ok := owners.M[caller.String()]
	prev2, ok := owners.M[target.String()]

	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Replace, &replace.ReplaceParams{Addr: target})
	rt.Verify()

	st = getState(rt)
	owners = st.UnwrapOwners(rt)
	own1, ok := owners.M[caller.String()]
	require.True(t, ok)
	require.Equal(t, own1, prev2)
	own2, ok := owners.M[target.String()]
	require.True(t, ok)
	require.Equal(t, own2, prev1)

}

func TestLockAbort(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	caller := tutil.NewIDAddr(t, 1000)
	target := tutil.NewIDAddr(t, 1001)

	rt.SetCaller(target, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test1"})

	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test2"})

	st := getState(rt)
	owners := st.UnwrapOwners(rt)
	prev1, ok := owners.M[caller.String()]
	prev2, ok := owners.M[target.String()]

	lockparams, err := atomic.WrapLockParams(5, &replace.ReplaceParams{Addr: target})
	require.NoError(t, err)
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Lock, lockparams)
	rt.Verify()

	// It'll fail because state is locked.
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.ExpectValidateCallerAny()
		rt.Call(h.ReplaceActor.Replace, &replace.ReplaceParams{Addr: target})
	})

	// Abort
	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Abort, lockparams)

	rt.ExpectValidateCallerAny()
	rt.Call(h.ReplaceActor.Replace, &replace.ReplaceParams{Addr: target})
	rt.Verify()

	st = getState(rt)
	owners = st.UnwrapOwners(rt)
	own1, ok := owners.M[caller.String()]
	require.True(t, ok)
	require.Equal(t, own1, prev2)
	own2, ok := owners.M[target.String()]
	require.True(t, ok)
	require.Equal(t, own2, prev1)
}

type shActorHarness struct {
	replace.ReplaceActor
	t *testing.T
}

func newHarness(t *testing.T) *shActorHarness {
	return &shActorHarness{
		ReplaceActor: replace.ReplaceActor{},
		t:            t,
	}
}

func (h *shActorHarness) constructAndVerify(t *testing.T, rt *mock.Runtime) {
	rt.ExpectValidateCallerType(builtin.InitActorCodeID)
	ret := rt.Call(h.ReplaceActor.Constructor, nil)
	assert.Nil(h.t, ret)
	rt.Verify()
}

func getRuntime(t *testing.T) *mock.Runtime {
	replaceActorAddr := tutil.NewIDAddr(t, 100)
	builder := mock.NewBuilder(replaceActorAddr).WithCaller(builtin.InitActorAddr, builtin.InitActorCodeID)
	return builder.Build(t)
}

func getState(rt *mock.Runtime) *replace.ReplaceState {
	var st replace.ReplaceState
	rt.GetState(&st)
	return &st
}
