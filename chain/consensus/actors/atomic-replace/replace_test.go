package replace_test

import (
	"testing"

	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	"github.com/stretchr/testify/assert"
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
