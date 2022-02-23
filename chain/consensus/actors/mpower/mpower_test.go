package mpower_test

import (
	//"fmt"
	"testing"

	mpower "github.com/filecoin-project/lotus/chain/consensus/actors/mpower"
	"github.com/filecoin-project/specs-actors/v7/support/mock"
	//"github.com/stretchr/testify/assert"
	//"github.com/stretchr/testify/require"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, mpower.Actor{})
}

// func TestConstruction(t *testing.T) {
// 	t.Run("simple construction", func(t *testing.T) {
// 		actor := newHarness(t)
// 		rt := getRuntime(t)
// 		actor.constructAndVerify(t, rt)
// 	})

// }

// func TestOwn(t *testing.T) {
// 	h := newHarness(t)
// 	rt := getRuntime(t)
// 	h.constructAndVerify(t, rt)
// 	caller := tutil.NewIDAddr(t, 65)
// 	rt.SetCaller(caller, builtin.AccountActorCodeID)
// 	rt.ExpectValidateCallerAny()
// 	rt.Call(h.ReplaceActor.Own, &replace.OwnParams{Seed: "test"})
// 	rt.Verify()

// 	st := getState(rt)
// 	owners := st.Owners.State().(*replace.Owners)
// 	_, ok := owners.M[caller.String()]
// 	require.True(t, ok)
// 	fmt.Println(owners.M)
// }

// type shActorHarness struct {
// 	mpower.ReplaceActor
// 	t *testing.T
// }

// func newHarness(t *testing.T) *shActorHarness {
// 	return &shActorHarness{
// 		ReplaceActor: replace.ReplaceActor{},
// 		t:            t,
// 	}
// }

// func (h *shActorHarness) constructAndVerify(t *testing.T, rt *mock.Runtime) {
// 	rt.ExpectValidateCallerType(builtin.InitActorCodeID)
// 	ret := rt.Call(h.ReplaceActor.Constructor, nil)
// 	assert.Nil(h.t, ret)
// 	rt.Verify()
// }
// func getRuntime(t *testing.T) *mock.Runtime {
// 	mpowerActorAddr := tutil.NewIDAddr(t, 65)
// 	builder := mock.NewBuilder(mpowerActorAddr).WithCaller(builtin.InitActorAddr, builtin.InitActorCodeID)
// 	return builder.Build(t)
// }

// func getState(rt *mock.Runtime) *mpower.State {
// 	var st mpower.State
// 	rt.GetState(&st)
// 	return &st
// }
