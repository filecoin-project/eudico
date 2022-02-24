package mpower_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"

	mpower "github.com/filecoin-project/lotus/chain/consensus/actors/mpower"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, mpower.Actor{})
}

func TestConstruction(t *testing.T) {
	t.Run("simple construction", func(t *testing.T) {
		actor := newHarness(t)
		rt := getRuntime(t)
		actor.constructAndVerify(t, rt)
	})

}

func TestAddMiner(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	caller := tutil.NewIDAddr(t, 1000)
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.Actor.AddMiners, &mpower.AddMinerParams{Miners: []address.Address{caller}})
	rt.Verify()

	st := getState(rt)
	require.Equal(t, len(st.Miners), 1)
}

func TestAddPkey(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	caller := tutil.NewIDAddr(t, 1000)
	rt.SetCaller(caller, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.Call(h.Actor.UpdateTaprootAddress, &mpower.NewTaprootAddressParam{PublicKey: []byte("test")})
	rt.Verify()

	st := getState(rt)
	require.Equal(t, st.PublicKey, []byte("test"))
}

type shActorHarness struct {
	mpower.Actor
	t *testing.T
}

func newHarness(t *testing.T) *shActorHarness {
	return &shActorHarness{
		Actor: mpower.Actor{},
		t:     t,
	}
}

func (h *shActorHarness) constructAndVerify(t *testing.T, rt *mock.Runtime) {
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	ret := rt.Call(h.Actor.Constructor, nil)
	assert.Nil(h.t, ret)
	rt.Verify()
}
func getRuntime(t *testing.T) *mock.Runtime {
	mpowerActorAddr := tutil.NewIDAddr(t, 65)
	builder := mock.NewBuilder(mpowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	return builder.Build(t)
}

func getState(rt *mock.Runtime) *mpower.State {
	var st mpower.State
	rt.GetState(&st)
	return &st
}
