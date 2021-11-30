package sca_test

import (
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	cid "github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
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
	shid := hierarchical.SubnetID("/root/f0101")
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
	shid = hierarchical.SubnetID("/root/f0102")
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
	sh, found := h.getSubnet(rt, hierarchical.SubnetID(res.ID))
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
	sh, found := h.getSubnet(rt, hierarchical.SubnetID(res.ID))
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
	sh, found = h.getSubnet(rt, hierarchical.SubnetID(res.ID))
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

func TestCheckpoints(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	SubnetActorAddr := tutil.NewIDAddr(t, 101)
	SubnetActorAddr2 := tutil.NewIDAddr(t, 102)

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
	shid := hierarchical.SubnetID("/root/f0101")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(1))
	// Verify instantiated subnet
	sh, found := h.getSubnet(rt, shid)
	nn1 := sh.ID
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0101")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("Register second subnet")
	rt.SetCaller(SubnetActorAddr2, actors.SubnetActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	// Call Register function
	ret = rt.Call(h.SubnetCoordActor.Register, nil)
	res, ok = ret.(*actor.AddSubnetReturn)
	require.True(t, ok)
	shid = hierarchical.SubnetID("/root/f0102")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(2))
	// Verify instantiated subnet
	sh, found = h.getSubnet(rt, shid)
	nn2 := sh.ID
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0102")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("commit first checkpoint in first window for first subnet")
	epoch := abi.ChainEpoch(10)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch := newCheckpoint(nn1, epoch+9)
	b, err := ch.MarshalBinary()
	require.NoError(t, err)
	rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	windowCh := currWindowCheckpoint(rt, epoch)
	require.Equal(t, windowCh.Data.Epoch, 100)
	// Check that child was added.
	require.GreaterOrEqual(t, windowCh.HasChildSource(nn1), 0)
	// Check previous checkpoint added
	prevCh, found := h.getPrevChildCheckpoint(rt, nn1)
	require.True(t, found)
	eq, err := prevCh.Equals(ch)
	require.NoError(t, err)
	require.True(t, eq)

	t.Log("trying to commit a checkpoint from subnet twice")
	epoch = abi.ChainEpoch(11)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	require.NoError(t, err)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	})

	t.Log("appending child checkpoint for same source")
	epoch = abi.ChainEpoch(12)
	prevcid, err := ch.Cid()
	require.NoError(t, err)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(nn1, epoch+10)
	ch.SetPrevious(prevcid)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	windowCh = currWindowCheckpoint(rt, epoch)
	require.Equal(t, windowCh.Data.Epoch, 100)
	// Check that child was appended for subnet.
	require.GreaterOrEqual(t, windowCh.HasChildSource(nn1), 0)
	require.Equal(t, len(windowCh.GetSourceChilds(nn1).Checks), 2)
	// Check previous checkpoint added
	prevCh, found = h.getPrevChildCheckpoint(rt, nn1)
	require.True(t, found)
	eq, err = prevCh.Equals(ch)
	require.NoError(t, err)
	require.True(t, eq)

	t.Log("trying to commit from wrong subnet")
	epoch = abi.ChainEpoch(12)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(nn2, epoch+9)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	})

	t.Log("trying to commit a checkpoint from the past")
	epoch = abi.ChainEpoch(11)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(nn1, epoch)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	})

	t.Log("raw checkpoint on first window fails")
	addr := tutil.NewIDAddr(t, 10011)
	epoch = abi.ChainEpoch(12)
	rt.SetCaller(addr, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.SetEpoch(epoch)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.SubnetCoordActor.RawCheckpoint, nil)
	})

	t.Log("commit first checkpoint in first window for second subnet")
	rt.SetCaller(SubnetActorAddr2, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(nn2, epoch+10)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	windowCh = currWindowCheckpoint(rt, epoch)
	// Check that child was added.
	require.GreaterOrEqual(t, windowCh.HasChildSource(nn2), 0)
	// Check that there are two childs.
	require.Equal(t, windowCh.LenChilds(), 2)
	// Check previous checkpoint added
	prevCh, found = h.getPrevChildCheckpoint(rt, nn2)
	require.True(t, found)
	eq, err = prevCh.Equals(ch)
	require.NoError(t, err)
	require.True(t, eq)
	t.Log("raw checkpoint in next period includes childs")
	epoch = abi.ChainEpoch(120)
	rt.SetCaller(addr, builtin.AccountActorCodeID)
	rt.SetEpoch(epoch)
	rt.ExpectValidateCallerAny()
	ret = rt.Call(h.SubnetCoordActor.RawCheckpoint, nil)
	chret, ok := ret.(*actor.CheckpointParams)
	require.True(t, ok)
	raw := schema.Checkpoint{}
	err = raw.UnmarshalBinary(chret.Checkpoint)
	require.NoError(t, err)
	require.Equal(t, raw.Data.Epoch, 100)
	require.GreaterOrEqual(t, raw.HasChildSource(nn2), 0)
	require.Equal(t, raw.LenChilds(), 2)
	pr, err := raw.PreviousCheck()
	require.NoError(t, err)
	require.Equal(t, pr, schema.NoPreviousCheck)

	t.Log("trying to commit wrong checkpoint (wrong subnet/wrong epoch/wrong prev")
	// TODO: We need to populate this with more tests for different conditions.

}

func TestCheckpointInactive(t *testing.T) {

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
	shid := hierarchical.SubnetID("/root/f0101")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(1))
	// Verify instantiated subnet
	sh, found := h.getSubnet(rt, shid)
	nn1 := sh.ID
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0101")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("release some stake to inactivate")
	releaseVal := abi.NewTokenAmount(2e18)
	params := &actor.FundParams{Value: releaseVal}
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.ExpectSend(SubnetActorAddr, builtin.MethodSend, nil, releaseVal, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ReleaseStake, params)
	sh, found = h.getSubnet(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Status, actor.Inactive)

	t.Log("trying to commit checkpoint for inactive subnet")
	epoch := abi.ChainEpoch(32)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch := newCheckpoint(nn1, epoch+20)
	b, err := ch.MarshalBinary()
	require.NoError(t, err)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
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
	_, found := h.getSubnet(rt, hierarchical.SubnetID(res.ID))
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
	ret := rt.Call(h.SubnetCoordActor.Constructor, &actor.ConstructorParams{NetworkName: "/root", CheckpointPeriod: 100})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.SCAState

	rt.GetState(&st)
	assert.Equal(h.t, actor.MinSubnetStake, st.MinStake)
	shid := hierarchical.RootSubnet
	assert.Equal(h.t, st.NetworkName, shid)
	assert.Equal(h.t, st.CheckPeriod, abi.ChainEpoch(100))
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

func (h *shActorHarness) getSubnet(rt *mock.Runtime, id hierarchical.SubnetID) (*actor.Subnet, bool) {
	var st actor.SCAState
	rt.GetState(&st)

	subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
	require.NoError(h.t, err)
	var out actor.Subnet
	found, err := subnets.Get(id, &out)
	require.NoError(h.t, err)

	return &out, found
}

func (h *shActorHarness) getPrevChildCheckpoint(rt *mock.Runtime, source hierarchical.SubnetID) (*schema.Checkpoint, bool) {
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

func newCheckpoint(source hierarchical.SubnetID, epoch abi.ChainEpoch) *schema.Checkpoint {
	cb := cid.V1Builder{Codec: cid.DagCBOR, MhType: multihash.BLAKE2B_MIN + 31}
	ch := schema.NewRawCheckpoint(source, epoch)
	c1, _ := cb.Sum([]byte("a"))
	c2, _ := cb.Sum([]byte("b"))
	c3, _ := cb.Sum([]byte("c"))
	ts := ltypes.NewTipSetKey(c1, c2, c3)
	ch.SetTipsetKey(ts)
	return ch
}
