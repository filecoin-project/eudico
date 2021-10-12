package shard_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/exitcode"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
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

// TODO: Tests have a lot of duplicated code. Consider aggregating them into
// independent functions to make tests more readable.
func TestAdd(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	owner := tutil.NewIDAddr(t, 101)

	addParams := &actor.AddParams{
		Name:       []byte("testShard"),
		Consensus:  actor.Delegated,
		DelegMiner: owner,
	}

	t.Log("create new shard successfully")
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
	// and stake has been assigned correctly.
	sh := h.getShard(rt, shid)
	require.Equal(t, len(sh.Miners), 1)
	require.Equal(t, sh.Status, actor.Active)
	require.Equal(t, sh.Consensus, actor.Delegated)
	require.Equal(t, h.getStake(rt, sh, owner), value)
	require.Equal(t, sh.TotalStake, value)

	t.Log("create new shard with ID of existing shard")
	// Check that it fails when we try to create a
	// shard with duplicate name.
	rt.ExpectValidateCallerAny()
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.ShardActor.Add, addParams)
	})

	t.Log("create new shard with ID of existing shard")
	addParams = &actor.AddParams{
		Name:       []byte("testShard2"),
		Consensus:  actor.Delegated,
		DelegMiner: owner,
	}

	// Send a small amount of stake
	value = abi.NewTokenAmount(2)
	rt.SetCaller(owner, builtin.AccountActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Call add function
	rt.ExpectValidateCallerAny()
	ret = rt.Call(h.ShardActor.Add, addParams)
	res, ok = ret.(*actor.AddShardReturn)
	require.True(t, ok)
	shid, err = actor.ShardID([]byte("testShard2"))
	require.NoError(t, err)
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid)
	rt.Verify()
	require.Equal(t, getState(rt).TotalShards, uint64(2))
	// Verify that the shard was added successfully.
	// and stake has been assigned correctly.
	sh = h.getShard(rt, shid)
	require.Equal(t, len(sh.Miners), 0)
	require.Equal(t, sh.Status, actor.Instantiated)
	require.Equal(t, sh.Consensus, actor.Delegated)
	require.Equal(t, h.getStake(rt, sh, owner), value)
	require.Equal(t, sh.TotalStake, value)
}

func TestJoin(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	owner := tutil.NewIDAddr(t, 101)
	joiner := tutil.NewIDAddr(t, 102)

	addParams := &actor.AddParams{
		Name:       []byte("testShard"),
		Consensus:  actor.PoW,
		DelegMiner: owner,
	}

	t.Log("create new shard")
	value := abi.NewTokenAmount(5e17)
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
	// Check that shard is instantiated but not active.
	sh := h.getShard(rt, shid)
	require.Equal(t, len(sh.Miners), 0)
	require.Equal(t, sh.Status, actor.Instantiated)
	require.Equal(t, h.getStake(rt, sh, owner), value)

	t.Log("new miner join the shard")
	joinParams := &actor.SelectParams{ID: shid.Bytes()}
	value = abi.NewTokenAmount(1e17)
	rt.ExpectValidateCallerAny()
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(joiner, builtin.AccountActorCodeID)
	rt.Call(h.ShardActor.Join, joinParams)
	// Still not active.
	sh = h.getShard(rt, shid)
	require.Equal(t, sh.Status, actor.Instantiated)
	require.Equal(t, h.getStake(rt, sh, joiner), value)
	require.Equal(t, len(sh.Miners), 0)

	// Joiner stakes enough to activate and become a miner
	value = abi.NewTokenAmount(9e17)
	rt.ExpectValidateCallerAny()
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(joiner, builtin.AccountActorCodeID)
	rt.Call(h.ShardActor.Join, joinParams)
	// Still not active.
	sh = h.getShard(rt, shid)
	require.Equal(t, sh.Status, actor.Active)
	require.Equal(t, h.getStake(rt, sh, joiner), abi.NewTokenAmount(1e18))
	require.Equal(t, sh.TotalStake, abi.NewTokenAmount(15e17))
	require.Equal(t, len(sh.Miners), 1)

	// Owner stakes enough to activate and become a miner
	value = abi.NewTokenAmount(5e17)
	rt.ExpectValidateCallerAny()
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(owner, builtin.AccountActorCodeID)
	rt.Call(h.ShardActor.Join, joinParams)
	// Still not active.
	sh = h.getShard(rt, shid)
	require.Equal(t, sh.Status, actor.Active)
	require.Equal(t, h.getStake(rt, sh, joiner), abi.NewTokenAmount(1e18))
	require.Equal(t, sh.TotalStake, abi.NewTokenAmount(2e18))
	require.Equal(t, len(sh.Miners), 2)

	// TODO: Add a test showing that delegated indeed accepts
	// only a single miner, and that even if you add enough stake
	// to become a miner you are not allowed to become a miner (this
	// is not the case for PoW)
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
	ret := rt.Call(h.ShardActor.Constructor, &initactor.ConstructorParams{NetworkName: "root"})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.ShardState

	rt.GetState(&st)
	assert.Equal(h.t, actor.MinShardStake, st.MinStake)
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

func (h *shActorHarness) getStake(rt *mock.Runtime, sh *actor.Shard, addr address.Address) abi.TokenAmount {
	state := h.getMinerState(rt, sh, addr)
	return state.InitialStake
}

func (h *shActorHarness) getMinerState(rt *mock.Runtime, sh *actor.Shard, addr address.Address) *actor.MinerState {
	stakes, err := adt.AsMap(adt.AsStore(rt), sh.Stake, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.MinerState
	found, err := stakes.Get(abi.AddrKey(addr), &out)
	require.NoError(h.t, err)
	require.True(h.t, found)

	return &out
}
