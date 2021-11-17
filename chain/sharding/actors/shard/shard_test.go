package shard_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/filecoin-project/lotus/chain/sharding/actors/sca"
	actor "github.com/filecoin-project/lotus/chain/sharding/actors/shard"
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

	t.Run("simple construction", func(t *testing.T) {
		actor := newHarness(t)
		rt := getRuntime(t)
		actor.constructAndVerify(t, rt)
	})

}

func TestJoin(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	notMiner := tutil.NewIDAddr(t, 103)
	miner := tutil.NewIDAddr(t, 104)
	totalStake := abi.NewTokenAmount(0)

	t.Log("join new shard without enough funds to register")
	value := abi.NewTokenAmount(5e17)
	rt.SetCaller(notMiner, builtin.AccountActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Anyone can call
	rt.ExpectValidateCallerAny()
	ret := rt.Call(h.ShardActor.Join, nil)
	assert.Nil(h.t, ret)
	// Check that the subnet is instantiated but not active.
	st := getState(rt)
	require.Equal(t, len(st.Miners), 0)
	require.Equal(t, st.Status, actor.Instantiated)
	require.Equal(t, getStake(t, rt, notMiner), value)
	totalStake = big.Add(totalStake, value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("new miner join the shard and activates it")
	value = abi.NewTokenAmount(1e18)
	rt.SetReceived(value)
	totalStake = big.Add(totalStake, value)
	rt.SetBalance(totalStake)
	rt.SetCaller(miner, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.Register, nil, totalStake, nil, exitcode.Ok)
	rt.Call(h.ShardActor.Join, nil)
	// Check that we are active
	st = getState(rt)
	require.Equal(t, len(st.Miners), 1)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, getStake(t, rt, miner), value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("existing participant not mining tops-up to become miner")
	value = abi.NewTokenAmount(5e17)
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(notMiner, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	// Triggers a stake top-up in SCA
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
	rt.Call(h.ShardActor.Join, nil)
	// Check that the subnet is instantiated but not active.
	st = getState(rt)
	// If we use delegated consensus we only accept one miner.
	require.Equal(t, len(st.Miners), 2)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, getStake(t, rt, notMiner), big.Mul(abi.NewTokenAmount(2), value))
	totalStake = big.Add(totalStake, value)
	require.Equal(t, st.TotalStake, totalStake)
}

func TestLeaveAndKill(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	joiner := tutil.NewIDAddr(t, 102)
	joiner2 := tutil.NewIDAddr(t, 103)
	joiner3 := tutil.NewIDAddr(t, 104)
	totalStake := abi.NewTokenAmount(0)

	t.Log("first miner joins subnet")
	value := abi.NewTokenAmount(1e18)
	rt.SetCaller(joiner, builtin.AccountActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	totalStake = big.Add(totalStake, value)
	// Anyone can call
	rt.ExpectValidateCallerAny()
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.Register, nil, totalStake, nil, exitcode.Ok)
	ret := rt.Call(h.ShardActor.Join, nil)
	assert.Nil(h.t, ret)
	// Check that the subnet is instantiated but not active.
	st := getState(rt)
	require.Equal(t, len(st.Miners), 1)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, getStake(t, rt, joiner), value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("second miner joins subnet")
	value = abi.NewTokenAmount(1e18)
	rt.SetReceived(value)
	totalStake = big.Add(totalStake, value)
	rt.SetBalance(value)
	rt.SetCaller(joiner2, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
	rt.Call(h.ShardActor.Join, nil)
	// Check that we are active
	st = getState(rt)
	require.Equal(t, len(st.Miners), 2)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, getStake(t, rt, joiner2), value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("non-miner user joins subnet")
	value = abi.NewTokenAmount(1e17)
	rt.SetReceived(value)
	totalStake = big.Add(totalStake, value)
	rt.SetBalance(value)
	rt.SetCaller(joiner3, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
	rt.Call(h.ShardActor.Join, nil)
	// Check that we are active
	st = getState(rt)
	require.Equal(t, len(st.Miners), 2)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, getStake(t, rt, joiner3), value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("second joiner leaves the shard")
	rt.ExpectValidateCallerAny()
	rt.SetCaller(joiner2, builtin.AccountActorCodeID)
	minerStake := getStake(t, rt, joiner2)
	totalStake = big.Sub(totalStake, minerStake)
	rt.SetBalance(minerStake)
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.ReleaseStake, &sca.FundParams{Value: minerStake}, big.Zero(), nil, exitcode.Ok)
	rt.ExpectSend(joiner2, builtin.MethodSend, nil, big.Div(minerStake, actor.LeavingFeeCoeff), nil, exitcode.Ok)
	rt.Call(h.ShardActor.Leave, nil)
	st = getState(rt)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, len(st.Miners), 1)
	require.Equal(t, getStake(t, rt, joiner2), big.Zero())
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("subnet can't be killed if there are still miners")
	rt.ExpectValidateCallerAny()
	rt.SetCaller(joiner2, builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.ShardActor.Kill, nil)
	})

	t.Log("first joiner inactivates the subnet")
	rt.ExpectValidateCallerAny()
	rt.SetCaller(joiner, builtin.AccountActorCodeID)
	minerStake = getStake(t, rt, joiner)
	totalStake = big.Sub(totalStake, minerStake)
	rt.SetBalance(minerStake)
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.ReleaseStake, &sca.FundParams{Value: minerStake}, big.Zero(), nil, exitcode.Ok)
	rt.ExpectSend(joiner, builtin.MethodSend, nil, big.Div(minerStake, actor.LeavingFeeCoeff), nil, exitcode.Ok)
	rt.Call(h.ShardActor.Leave, nil)
	st = getState(rt)
	require.Equal(t, st.Status, actor.Inactive)
	require.Equal(t, len(st.Miners), 0)
	require.Equal(t, getStake(t, rt, joiner), big.Zero())
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("miner can't leave twice")
	rt.ExpectValidateCallerAny()
	rt.ExpectAbort(exitcode.ErrForbidden, func() {
		rt.Call(h.ShardActor.Leave, nil)
	})

	t.Log("third kills the subnet, and takes its stake")
	minerStake = getStake(t, rt, joiner3)
	rt.SetCaller(joiner3, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.SetBalance(minerStake)
	rt.ExpectSend(sca.ShardCoordActorAddr, sca.Methods.Kill, nil, big.Zero(), nil, exitcode.Ok)
	rt.Call(h.ShardActor.Kill, nil)
	st = getState(rt)
	require.Equal(t, st.Status, actor.Terminating)

	t.Log("subnet can't be killed twice")
	rt.ExpectValidateCallerAny()
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.ShardActor.Kill, nil)
	})

	rt.ExpectValidateCallerAny()
	totalStake = big.Sub(totalStake, minerStake)
	rt.SetBalance(minerStake)
	rt.ExpectSend(joiner3, builtin.MethodSend, nil, big.Div(minerStake, actor.LeavingFeeCoeff), nil, exitcode.Ok)
	rt.Call(h.ShardActor.Leave, nil)
	st = getState(rt)
	require.Equal(t, st.Status, actor.Killed)
	require.Equal(t, len(st.Miners), 0)
	require.Equal(t, getStake(t, rt, joiner3), big.Zero())
	require.Equal(t, st.TotalStake.Abs(), totalStake.Abs())

	// TODO: Check that a miner can't leave twice and get their stake twice.
	// TODO: Check killing states. Joiner 2 calls kill and then the other guy takes it stake.
	/*

		t.Log("adder leaves the shard")
		rt.ExpectValidateCallerAny()
		rt.SetCaller(owner, builtin.AccountActorCodeID)
		rt.ExpectSend(owner, builtin.MethodSend, nil, big.Div(addValue, actor.LeavingFeeCoeff), nil, exitcode.Ok)
		rt.Call(h.ShardActor.Leave, leaveParams)
		sh, found = h.getShard(rt, shid)
		require.True(h.t, found)
		require.Equal(t, sh.Status, actor.Terminating)
		// Not in stakes anymore.
		_, found = h.getMinerState(rt, sh, owner)
		require.False(h.t, found)
		_, found = h.getMinerState(rt, sh, joiner)
		require.True(h.t, found)
		// Also removed from miners list.
		require.Equal(t, len(sh.Miners), 0)

		t.Log("calling twice to get stake twice")
		rt.ExpectValidateCallerAny()
		rt.SetCaller(owner, builtin.AccountActorCodeID)
		rt.ExpectAbort(exitcode.ErrForbidden, func() {
			rt.Call(h.ShardActor.Leave, leaveParams)
		})

		t.Log("joiner leaves the shard")
		rt.ExpectValidateCallerAny()
		rt.SetCaller(joiner, builtin.AccountActorCodeID)
		rt.ExpectSend(joiner, builtin.MethodSend, nil, big.Div(joinValue, actor.LeavingFeeCoeff), nil, exitcode.Ok)
		rt.Call(h.ShardActor.Leave, leaveParams)
		// The shard is completely removed
		_, found = h.getShard(rt, shid)
		require.False(h.t, found)
		require.Equal(t, getState(rt).TotalShards, uint64(0))
	*/
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

func (h *shActorHarness) constructAndVerify(t *testing.T, rt *mock.Runtime) {
	rt.ExpectValidateCallerType(builtin.InitActorCodeID)
	ret := rt.Call(h.ShardActor.Constructor,
		&actor.ConstructParams{
			NetworkName:   "root",
			Name:          "myTestSubnet",
			Consensus:     actor.PoW,
			MinMinerStake: actor.MinMinerStake,
			DelegMiner:    tutil.NewIDAddr(t, 101),
		})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.ShardState

	rt.GetState(&st)
	parentcid, err := naming.ShardCid("root")
	require.NoError(h.t, err)
	assert.Equal(h.t, st.ParentID, "root")
	assert.Equal(h.t, st.ParentCid, parentcid)
	assert.Equal(h.t, st.Consensus, actor.PoW)
	assert.Equal(h.t, st.MinMinerStake, actor.MinMinerStake)
	assert.Equal(h.t, st.Status, actor.Instantiated)
	// Verify that the genesis for the subnet has been generated.
	// TODO: Consider making some test verifications over genesis.
	assert.NotEqual(h.t, len(st.Genesis), 0)
	verifyEmptyMap(h.t, rt, st.Stake)
}

func verifyEmptyMap(t testing.TB, rt *mock.Runtime, cid cid.Cid) {
	mapChecked, err := adt.AsMap(adt.AsStore(rt), cid, builtin.DefaultHamtBitwidth)
	assert.NoError(t, err)
	keys, err := mapChecked.CollectKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func getRuntime(t *testing.T) *mock.Runtime {
	shardActorAddr := tutil.NewIDAddr(t, 100)
	builder := mock.NewBuilder(shardActorAddr).WithCaller(builtin.InitActorAddr, builtin.InitActorCodeID)
	return builder.Build(t)
}

func getState(rt *mock.Runtime) *actor.ShardState {
	var st actor.ShardState
	rt.GetState(&st)
	return &st
}

func getStake(t *testing.T, rt *mock.Runtime, addr address.Address) abi.TokenAmount {
	var st actor.ShardState
	rt.GetState(&st)
	stakes, err := adt.AsBalanceTable(adt.AsStore(rt), st.Stake)
	require.NoError(t, err)
	out, err := stakes.Get(addr)
	require.NoError(t, err)
	return out
}
