package sca_test

import (
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	actor "github.com/filecoin-project/lotus/chain/sharding/actors/sca"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, actor.ShardCoordActor{})
}

func TestConstruction(t *testing.T) {
	builder := mock.NewBuilder(actor.ShardCoordActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)

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
	shardActorAddr := tutil.NewIDAddr(t, 101)

	t.Log("register new shard successfully")
	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	ret := rt.Call(h.ShardCoordActor.Register, nil)
	res, ok := ret.(*actor.AddShardReturn)
	require.True(t, ok)
	shid, err := naming.SubnetID("/root/t0101").Cid()
	require.NoError(t, err)
	// Verify the return value is correct.
	require.Equal(t, res.Cid, shid)
	rt.Verify()
	require.Equal(t, getState(rt).TotalShards, uint64(1))

	// Verify instantiated shard
	sh, found := h.getShard(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/t0101")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("try registering existing shard")
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardCoordActor.Register, nil)
	})

	t.Log("try registering without staking enough funds")
	shardActorAddr = tutil.NewIDAddr(t, 102)
	lowVal := abi.NewTokenAmount(2)
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(lowVal)
	rt.SetBalance(lowVal)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardCoordActor.Register, nil)
	})

	t.Log("Register second subnet")
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	ret = rt.Call(h.ShardCoordActor.Register, nil)
	res, ok = ret.(*actor.AddShardReturn)
	require.True(t, ok)
	shid, err = naming.SubnetID("/root/t0102").Cid()
	require.NoError(t, err)
	// Verify the return value is correct.
	require.Equal(t, res.Cid, shid)
	rt.Verify()
	require.Equal(t, getState(rt).TotalShards, uint64(2))
	// Verify instantiated shard
	sh, found = h.getShard(rt, shid)
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
	shardActorAddr := tutil.NewIDAddr(t, 101)

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	ret := rt.Call(h.ShardCoordActor.Register, nil)
	res, ok := ret.(*actor.AddShardReturn)
	require.True(t, ok)

	t.Log("add to unregistered subnet")
	newActorAddr := tutil.NewIDAddr(t, 102)
	rt.SetCaller(newActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(big.Add(value, value))
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardCoordActor.AddStake, nil)
	})

	t.Log("add some stake")
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(big.Add(value, value))
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.Call(h.ShardCoordActor.AddStake, nil)
	sh, found := h.getShard(rt, res.Cid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Add(value, value))
	require.Equal(t, sh.Status, actor.Active)

	t.Log("add with no funds")
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetReceived(abi.NewTokenAmount(0))
	rt.SetBalance(big.Add(value, value))
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardCoordActor.AddStake, nil)
	})
}

func TestReleaseStake(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	shardActorAddr := tutil.NewIDAddr(t, 101)

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	ret := rt.Call(h.ShardCoordActor.Register, nil)
	res, ok := ret.(*actor.AddShardReturn)
	require.True(t, ok)

	releaseVal := abi.NewTokenAmount(1e18)
	params := &actor.FundParams{Value: releaseVal}

	t.Log("release to unregistered subnet")
	newActorAddr := tutil.NewIDAddr(t, 102)
	rt.SetCaller(newActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardCoordActor.ReleaseStake, params)
	})

	t.Log("release some stake")
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.ExpectSend(shardActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.ShardCoordActor.ReleaseStake, params)
	sh, found := h.getShard(rt, res.Cid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Sub(value, releaseVal))
	require.Equal(t, sh.Status, actor.Active)

	t.Log("release to inactivate")
	currStake := sh.Stake
	releaseVal = abi.NewTokenAmount(1e8)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.ExpectSend(shardActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.ShardCoordActor.ReleaseStake, params)
	sh, found = h.getShard(rt, res.Cid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Sub(currStake, releaseVal))
	require.Equal(t, sh.Status, actor.Inactive)

	t.Log("not enough funds to release")
	releaseVal = abi.NewTokenAmount(1e18)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.ExpectSend(shardActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.ShardCoordActor.ReleaseStake, params)
	})

	t.Log("params with zero funds to release")
	releaseVal = abi.NewTokenAmount(0)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.ExpectSend(shardActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardCoordActor.ReleaseStake, params)
	})

	t.Log("not enough balance to release")
	releaseVal = abi.NewTokenAmount(1e18)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	rt.SetBalance(abi.NewTokenAmount(1e5))
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.ExpectSend(shardActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.ShardCoordActor.ReleaseStake, params)
	})
}

func TestKill(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	shardActorAddr := tutil.NewIDAddr(t, 101)

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	// Call Register function
	ret := rt.Call(h.ShardCoordActor.Register, nil)
	res, ok := ret.(*actor.AddShardReturn)
	require.True(t, ok)

	t.Log("kill shard")
	rt.SetCaller(shardActorAddr, actors.ShardActorCodeID)
	// Only shard actors can call.
	rt.ExpectValidateCallerType(actors.ShardActorCodeID)
	rt.ExpectSend(shardActorAddr, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	rt.Call(h.ShardCoordActor.Kill, nil)
	// The shard has been removed.
	_, found := h.getShard(rt, res.Cid)
	require.False(h.t, found)
}

type shActorHarness struct {
	actor.ShardCoordActor
	t *testing.T
}

func newHarness(t *testing.T) *shActorHarness {
	return &shActorHarness{
		ShardCoordActor: actor.ShardCoordActor{},
		t:               t,
	}
}

func (h *shActorHarness) constructAndVerify(rt *mock.Runtime) {
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	ret := rt.Call(h.ShardCoordActor.Constructor, &initactor.ConstructorParams{NetworkName: "/root"})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.SCAState

	rt.GetState(&st)
	assert.Equal(h.t, actor.MinShardStake, st.MinStake)
	shid := naming.Root
	shcid, err := shid.Cid()
	require.NoError(h.t, err)
	assert.Equal(h.t, st.Network, shcid)
	assert.Equal(h.t, st.NetworkName, shid)
	verifyEmptyMap(h.t, rt, st.Shards)
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

func (h *shActorHarness) getShard(rt *mock.Runtime, id cid.Cid) (*actor.Shard, bool) {
	var st actor.SCAState
	rt.GetState(&st)

	shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.Shard
	found, err := shards.Get(abi.CidKey(id), &out)
	require.NoError(h.t, err)

	return &out, found
}
