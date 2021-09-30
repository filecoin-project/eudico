package actor_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	actor "github.com/filecoin-project/lotus/chain/sharding/actors"
	"github.com/filecoin-project/specs-actors/v5/actors/builtin"
	"github.com/filecoin-project/specs-actors/v5/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v5/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v5/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, actor.ShardActor{})
}

func TestConstruction(t *testing.T) {
	actor := newHarness(t)

	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)

	t.Run("simple construction", func(t *testing.T) {
		rt := builder.Build(t)
		actor.constructAndVerify(rt)
	})

}

func TestAdd(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	owner := tutil.NewIDAddr(t, 101)

	addParams := &actor.AddParams{
		Name:      []byte("testShard"),
		Consensus: actor.Delegated,
	}

	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetCaller(owner, builtin.AccountActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Anyone can call
	rt.ExpectValidateCallerAny()
	// Call add function
	ret := rt.Call(h.ShardActor.Add, addParams)
	res, ok := ret.(*actor.AddShardReturn)
	require.True(t, ok)
	shid, err := actor.ShardID([]byte("testShard"))
	require.NoError(t, err)
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid)
	rt.Verify()
	require.Equal(t, getState(rt).TotalShards, uint64(1))

	// Verify that the shard was added successfully.
	sh := h.getShard(rt, shid)
	require.Equal(t, len(sh.Miners), 1)
	require.Equal(t, sh.Status, actor.Active)
	require.Equal(t, sh.Consensus, actor.Delegated)
	stake := h.getStakes(rt, sh, owner)
	require.Equal(t, stake.InitialStake, value)
	// TODO: Verify the rest of teh state of the shard in
	// the next iteration when exhaustive testing in done.
	// For now it is enough to start checking if it works with
	// the state update and actual spawning of the shard.

	// TODO: Check that it fails when we try to create a
	// shard with duplicate name.
	// TODO: Check status if not enough stake is sent.
}

type shActorHarness struct {
	actor.ShardActor
	t *testing.T
}

func newHarness(t *testing.T) *shActorHarness {

	return &shActorHarness{
		ShardActor: actor.ShardActor{},
		t:          t,
	}
}
func (h *shActorHarness) constructAndVerify(rt *mock.Runtime) {
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	ret := rt.Call(h.ShardActor.Constructor, nil)
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.ShardState

	rt.GetState(&st)
	assert.Equal(h.t, actor.MinShardStake, st.MinStake)
	// TODO: This will fail once we introduce configurable network names
	shid, err := actor.ShardID([]byte("root"))
	require.NoError(h.t, err)
	assert.Equal(h.t, st.Network, shid)
	verifyEmptyMap(h.t, rt, st.Shards)
}

func verifyEmptyMap(t testing.TB, rt *mock.Runtime, cid cid.Cid) {
	mapChecked, err := adt.AsMap(adt.AsStore(rt), cid, builtin.DefaultHamtBitwidth)
	assert.NoError(t, err)
	keys, err := mapChecked.CollectKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func getState(rt *mock.Runtime) *actor.ShardState {
	var st actor.ShardState
	rt.GetState(&st)
	return &st
}

func (h *shActorHarness) getShard(rt *mock.Runtime, id cid.Cid) *actor.Shard {
	var st actor.ShardState
	rt.GetState(&st)

	shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.Shard
	found, err := shards.Get(abi.CidKey(id), &out)
	require.NoError(h.t, err)
	require.True(h.t, found)

	return &out
}

func (h *shActorHarness) getStakes(rt *mock.Runtime, sh *actor.Shard, addr address.Address) *actor.MinerState {
	stakes, err := adt.AsMap(adt.AsStore(rt), sh.Stake, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.MinerState
	found, err := stakes.Get(abi.AddrKey(addr), &out)
	require.NoError(h.t, err)
	require.True(h.t, found)

	return &out
}
