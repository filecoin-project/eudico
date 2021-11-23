package sca_test

import (
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, actor.SubnetCoordActor{})
}

func TestConstruction(t *testing.T) {
	builder := mock.NewBuilder(actor.SubnetCoordActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)

	actor := newHarness(t)
	t.Run("simple construction", func(t *testing.T) {
		rt := builder.Build(t)
		actor.constructAndVerify(rt)
	})

}

func TestRegister(t *testing.T) {
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
	res, ok := ret.(*actor.AddSubnetReturn)
	require.True(t, ok)
	shid, err := hierarchical.SubnetID("/root/t0101").Cid()
	require.NoError(t, err)
	// Verify the return value is correct.
	require.Equal(t, res.Cid, shid)
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(1))

	// Verify instantiated subnet
	sh, found := h.getSubnet(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/t0101")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("try registering existing subnet")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.Register, nil)
	})

	t.Log("try registering without staking enough funds")
	SubnetActorAddr = tutil.NewIDAddr(t, 102)
	lowVal := abi.NewTokenAmount(2)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(lowVal)
	rt.SetBalance(lowVal)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.Register, nil)
	})

	t.Log("Register second subnet")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret = rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok = ret.(*actor.AddSubnetReturn)
	require.True(t, ok)
	shid, err = hierarchical.SubnetID("/root/t0102").Cid()
	require.NoError(t, err)
	// Verify the return value is correct.
	require.Equal(t, res.Cid, shid)
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(2))
	// Verify instantiated subnet
	sh, found = h.getSubnet(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/t0102")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)
}

func TestAddStake(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	SubnetActorAddr := tutil.NewIDAddr(t, 101)

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret := rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok := ret.(*actor.AddSubnetReturn)
	require.True(t, ok)

	t.Log("add to unregistered subnet")
	newActorAddr := tutil.NewIDAddr(t, 102)
	rt.SetCaller(newActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(big.Add(value, value))
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.AddStake, nil)
	})

	t.Log("add some stake")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(big.Add(value, value))
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.Call(h.SubnetCoordActor.AddStake, nil)
	sh, found := h.getSubnet(rt, res.Cid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Add(value, value))
	require.Equal(t, sh.Status, actor.Active)

	t.Log("add with no funds")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(abi.NewTokenAmount(0))
	rt.SetBalance(big.Add(value, value))
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.AddStake, nil)
	})
}

func TestReleaseStake(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	SubnetActorAddr := tutil.NewIDAddr(t, 101)

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret := rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok := ret.(*actor.AddSubnetReturn)
	require.True(t, ok)

	releaseVal := abi.NewTokenAmount(1e18)
	params := &actor.FundParams{Value: releaseVal}

	t.Log("release to unregistered subnet")
	newActorAddr := tutil.NewIDAddr(t, 102)
	rt.SetCaller(newActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	})

	t.Log("release some stake")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	sh, found := h.getSubnet(rt, res.Cid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Sub(value, releaseVal))
	require.Equal(t, sh.Status, actor.Active)

	t.Log("release to inactivate")
	currStake := sh.Stake
	releaseVal = abi.NewTokenAmount(1e8)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	sh, found = h.getSubnet(rt, res.Cid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Sub(currStake, releaseVal))
	require.Equal(t, sh.Status, actor.Inactive)

	t.Log("not enough funds to release")
	releaseVal = abi.NewTokenAmount(1e18)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	})

	t.Log("params with zero funds to release")
	releaseVal = abi.NewTokenAmount(0)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	})

	t.Log("not enough balance to release")
	releaseVal = abi.NewTokenAmount(1e18)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetBalance(abi.NewTokenAmount(1e5))
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	})
}

func TestKill(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	SubnetActorAddr := tutil.NewIDAddr(t, 101)

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret := rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok := ret.(*actor.AddSubnetReturn)
	require.True(t, ok)

	t.Log("kill subnet")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.Kill, nil)
	// The subnet has been removed.
	_, found := h.getSubnet(rt, res.Cid)
	require.False(h.t, found)
}

type shActorHarness struct {
	actor.SubnetCoordActor
	t *testing.T
}

func newHarness(t *testing.T) *shActorHarness {
	return &shActorHarness{
		SubnetCoordActor: actor.SubnetCoordActor{},
		t:                t,
	}
}

func (h *shActorHarness) constructAndVerify(rt *mock.Runtime) {
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	ret := rt.Call(h.SubnetCoordActor.Constructor, &initactor.ConstructorParams{NetworkName: "/root"})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.SCAState

	rt.GetState(&st)
	assert.Equal(h.t, actor.MinSubnetStake, st.MinStake)
	shid := hierarchical.RootSubnet
	shcid, err := shid.Cid()
	require.NoError(h.t, err)
	assert.Equal(h.t, st.Network, shcid)
	assert.Equal(h.t, st.NetworkName, shid)
	verifyEmptyMap(h.t, rt, st.Subnets)
}

func verifyEmptyMap(t testing.TB, rt *mock.Runtime, cid cid.Cid) {
	mapChecked, err := adt.AsMap(adt.AsStore(rt), cid, builtin.DefaultHamtBitwidth)
	assert.NoError(t, err)
	keys, err := mapChecked.CollectKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func getState(rt *mock.Runtime) *actor.SCAState {
	var st actor.SCAState
	rt.GetState(&st)
	return &st
}

func (h *shActorHarness) getSubnet(rt *mock.Runtime, id cid.Cid) (*actor.Subnet, bool) {
	var st actor.SCAState
	rt.GetState(&st)

	subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.Subnet
	found, err := subnets.Get(abi.CidKey(id), &out)
	require.NoError(h.t, err)

	return &out, found
}
