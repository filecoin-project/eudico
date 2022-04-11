package sca_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v7/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, actor.SubnetCoordActor{})
}

func TestConstruction(t *testing.T) {
	builder := mock.NewBuilder(hierarchical.SubnetCoordActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)

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
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	shid := address.SubnetID("/root/f0101")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(1))

	// Verify instantiated subnet
	sh, found := h.getSubnet(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0101")
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
	rt.Verify()

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
	rt.Verify()

	t.Log("Register second subnet")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret = rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok = ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	shid = address.SubnetID("/root/f0102")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(2))
	// Verify instantiated subnet
	sh, found = h.getSubnet(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0102")
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
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	rt.Verify()

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
	rt.Verify()

	t.Log("add some stake")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(big.Add(value, value))
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.Call(h.SubnetCoordActor.AddStake, nil)
	sh, found := h.getSubnet(rt, address.SubnetID(res.ID))
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
	rt.Verify()
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
	res, ok := ret.(*actor.SubnetIDParam)
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
	rt.Verify()

	t.Log("release some stake")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	sh, found := h.getSubnet(rt, address.SubnetID(res.ID))
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Sub(value, releaseVal))
	require.Equal(t, sh.Status, actor.Active)
	rt.Verify()

	t.Log("release to inactivate")
	currStake := sh.Stake
	releaseVal = abi.NewTokenAmount(1e8)
	params = &actor.FundParams{Value: releaseVal}
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	sh, found = h.getSubnet(rt, address.SubnetID(res.ID))
	require.True(h.t, found)
	require.Equal(t, sh.Stake, big.Sub(currStake, releaseVal))
	require.Equal(t, sh.Status, actor.Inactive)
	rt.Verify()

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
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(t, ok)

	t.Log("kill subnet")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.Kill, nil)
	rt.Verify()
	// The subnet has been removed.
	_, found := h.getSubnet(rt, address.SubnetID(res.ID))
	require.False(h.t, found)
}

type shActorHarness struct {
	actor.SubnetCoordActor
	t  *testing.T
	sn address.SubnetID
}

func newHarness(t *testing.T) *shActorHarness {
	return &shActorHarness{
		SubnetCoordActor: actor.SubnetCoordActor{},
		t:                t,
	}
}

func (h *shActorHarness) constructAndVerify(rt *mock.Runtime) {
	shid := address.RootSubnet
	h.constructAndVerifyWithNetworkName(rt, shid)
}

func (h *shActorHarness) constructAndVerifyWithNetworkName(rt *mock.Runtime, shid address.SubnetID) {
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	ret := rt.Call(h.SubnetCoordActor.Constructor, &actor.ConstructorParams{NetworkName: shid.String(), CheckpointPeriod: 100})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.SCAState

	rt.GetState(&st)
	assert.Equal(h.t, actor.MinSubnetStake, st.MinStake)
	assert.Equal(h.t, st.NetworkName, shid)
	assert.Equal(h.t, st.CheckPeriod, abi.ChainEpoch(100))
	verifyEmptyMap(h.t, rt, st.Subnets)
	verifyEmptyMap(h.t, rt, st.CheckMsgsRegistry)
	verifyEmptyMap(h.t, rt, st.Checkpoints)
}

func verifyEmptyMap(t testing.TB, rt *mock.Runtime, cid cid.Cid) {
	mapChecked, err := adt.AsMap(adt.AsStore(rt), cid, builtin.DefaultHamtBitwidth)
	assert.NoError(t, err)
	keys, err := mapChecked.CollectKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func (h *shActorHarness) registerSubnet(rt *mock.Runtime, parent address.SubnetID, snAddr address.Address) address.SubnetID {
	h.t.Log("register new subnet successfully")
	// Send 2FIL of stake
	value := abi.NewTokenAmount(2e18)
	rt.SetCaller(snAddr, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret := rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(h.t, ok)
	shid := address.NewSubnetID(parent, snAddr)
	// Verify the return value is correct.
	require.Equal(h.t, res.ID, shid.String())
	rt.Verify()
	h.sn = shid
	return shid
}

func getState(rt *mock.Runtime) *actor.SCAState {
	var st actor.SCAState
	rt.GetState(&st)
	return &st
}

func (h *shActorHarness) getSubnet(rt *mock.Runtime, id address.SubnetID) (*actor.Subnet, bool) {
	var st actor.SCAState
	rt.GetState(&st)

	subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.Subnet
	found, err := subnets.Get(hierarchical.SubnetKey(id), &out)
	require.NoError(h.t, err)

	return &out, found
}

func (h *shActorHarness) getPrevChildCheckpoint(rt *mock.Runtime, source address.SubnetID) (*schema.Checkpoint, bool) {
	sh, found := h.getSubnet(rt, source)
	if !found {
		return nil, false
	}
	return &sh.PrevCheckpoint, true
}

func currWindowCheckpoint(rt *mock.Runtime, epoch abi.ChainEpoch) *schema.Checkpoint {
	st := getState(rt)
	chEpoch := types.WindowEpoch(epoch, st.CheckPeriod)
	ch, found, err := st.GetCheckpoint(adt.AsStore(rt), chEpoch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get checkpoint template for epoch")
	if !found {
		ch = schema.NewRawCheckpoint(st.NetworkName, chEpoch)
	}
	return ch
}

func newCheckpoint(source address.SubnetID, epoch abi.ChainEpoch) *schema.Checkpoint {
	cb := cid.V1Builder{Codec: cid.DagCBOR, MhType: multihash.BLAKE2B_MIN + 31}
	ch := schema.NewRawCheckpoint(source, epoch)
	c1, _ := cb.Sum([]byte("a"))
	c2, _ := cb.Sum([]byte("b"))
	c3, _ := cb.Sum([]byte("c"))
	ts := ltypes.NewTipSetKey(c1, c2, c3)
	ch.SetTipsetKey(ts)
	return ch
}

func addMsgMeta(t *testing.T, ch *schema.Checkpoint, from, to address.SubnetID, rand string, value abi.TokenAmount) *schema.CrossMsgMeta {
	cb := cid.V1Builder{Codec: cid.DagCBOR, MhType: multihash.BLAKE2B_MIN + 31}
	c, _ := cb.Sum([]byte(from.String() + rand))
	m := schema.NewCrossMsgMeta(from, to)
	m.SetCid(c)
	err := m.AddValue(value)
	require.NoError(t, err)
	ch.AppendMsgMeta(m)
	return m

}

func (h *shActorHarness) getMsgMeta(rt *mock.Runtime, c cid.Cid) (*actor.CrossMsgs, bool) {
	var st actor.SCAState
	rt.GetState(&st)

	metas, err := adt.AsMap(adt.AsStore(rt), st.CheckMsgsRegistry, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.CrossMsgs
	found, err := metas.Get(abi.CidKey(c), &out)
	require.NoError(h.t, err)

	return &out, found
}
