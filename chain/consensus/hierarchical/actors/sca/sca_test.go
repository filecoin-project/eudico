package sca_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
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
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	shid := address.SubnetID("/root/f0101")
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
	res, ok = ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	shid = address.SubnetID("/root/f0102")
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
	rt.Verify()
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
	rt.Verify()
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

	t.Log("raw checkpoint on first window is empty")
	epoch = abi.ChainEpoch(12)
	rt.SetEpoch(epoch)
	st := getState(rt)
	raw, err := actor.RawCheckpoint(st, adt.AsStore(rt), epoch)
	require.NoError(t, err)
	chEpoch := types.CheckpointEpoch(epoch, st.CheckPeriod)
	emptyRaw := schema.NewRawCheckpoint(st.NetworkName, chEpoch)
	require.NoError(t, err)
	eq, err = raw.Equals(emptyRaw)
	require.NoError(t, err)
	require.True(t, eq)

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

	st = getState(rt)
	raw, err = actor.RawCheckpoint(st, adt.AsStore(rt), epoch)
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

func TestCheckpointCrossMsgs(t *testing.T) {
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

	t.Log("commit checkpoint with cross msgs")
	epoch := abi.ChainEpoch(10)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch := newCheckpoint(sh.ID, epoch+9)
	// Add msgMeta directed to other subnets
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child1"), "rand1")
	// By not adding a random string we are checking that nothing fails when to MsgMeta
	// for different subnets are propagating the same CID. This will probably never be the
	// case for honest peers, but it is an attack vector.
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child2"), "")
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child3"), "")
	// And to this subnet
	addMsgMeta(ch, sh.ID, address.RootSubnet, "")
	addMsgMeta(ch, sh.ID, address.RootSubnet, "rand")
	prevcid, _ := ch.Cid()

	b, err := ch.MarshalBinary()
	require.NoError(t, err)
	rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	rt.Verify()
	windowCh := currWindowCheckpoint(rt, epoch)
	require.Equal(t, windowCh.Data.Epoch, 100)
	// Check that child was added.
	require.GreaterOrEqual(t, windowCh.HasChildSource(sh.ID), 0)
	// Check previous checkpoint added
	prevCh, found := h.getPrevChildCheckpoint(rt, sh.ID)
	require.True(t, found)
	eq, err := prevCh.Equals(ch)
	require.NoError(t, err)
	require.True(t, eq)

	// Check that the BottomUpMsgs to be applied are added
	st := getState(rt)
	_, found, err = st.GetBottomUpMsgMeta(adt.AsStore(rt), 0)
	require.NoError(t, err)
	require.True(t, found)
	_, found, err = st.GetBottomUpMsgMeta(adt.AsStore(rt), 1)
	require.NoError(t, err)
	require.True(t, found)

	// Check msgMeta to other subnets are aggregated
	m := windowCh.CrossMsgs()
	subs := []address.SubnetID{"/root/f0102/child1", "/root/f0102/child2", "/root/f0102/child3"}
	require.Equal(t, len(m), 3)
	prevs := make(map[string]schema.CrossMsgMeta)
	for i, mm := range m {
		// Check that from has been renamed
		require.Equal(t, mm.From, address.RootSubnet.String())
		// Check the to is kept
		require.Equal(t, len(windowCh.CrossMsgsTo(subs[i])), 1)
		// Append for the future
		prevs[mm.To] = mm
	}

	t.Log("commit checkpoint with more cross-msgs for subnet")
	epoch = abi.ChainEpoch(13)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(sh.ID, epoch+6)
	// Msgs to this subnet
	addMsgMeta(ch, sh.ID, address.RootSubnet, "r2")
	addMsgMeta(ch, sh.ID, address.RootSubnet, "r3")
	ch.SetPrevious(prevcid)
	prevcid, _ = ch.Cid()

	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	rt.Verify()

	// Check that the BottomUpMsgs to be applied are added with the right nonce.
	st = getState(rt)
	_, found, err = st.GetBottomUpMsgMeta(adt.AsStore(rt), 2)
	require.NoError(t, err)
	require.True(t, found)
	_, found, err = st.GetBottomUpMsgMeta(adt.AsStore(rt), 3)
	require.NoError(t, err)
	require.True(t, found)

	t.Log("commit second checkpoint with overlapping metas")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(sh.ID, epoch+9)
	ch.SetPrevious(prevcid)
	// Add msgMeta directed to other subnets
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child1"), "")
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child2"), "")
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child3"), "")
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child4"), "")

	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	rt.Call(h.SubnetCoordActor.CommitChildCheckpoint, &actor.CheckpointParams{b})
	rt.Verify()
	windowCh = currWindowCheckpoint(rt, epoch)
	require.Equal(t, windowCh.Data.Epoch, 100)
	// Check that child was added.
	require.GreaterOrEqual(t, windowCh.HasChildSource(sh.ID), 0)
	// Check previous checkpoint added
	prevCh, found = h.getPrevChildCheckpoint(rt, sh.ID)
	require.True(t, found)
	eq, err = prevCh.Equals(ch)
	require.NoError(t, err)
	require.True(t, eq)

	// Check msgMeta to other subnets are aggregated
	m = windowCh.CrossMsgs()
	subs = []address.SubnetID{"/root/f0102/child1", "/root/f0102/child2", "/root/f0102/child3", "/root/f0102/child4"}
	require.Equal(t, len(m), 4)
	for i, mm := range m {
		// Check that from has been renamed
		require.Equal(t, mm.From, address.RootSubnet.String())
		// Check the to is kept
		require.Equal(t, len(windowCh.CrossMsgsTo(subs[i])), 1)
		// Get current msgMetas
		mcid, _ := mm.Cid()
		msgmeta, found := h.getMsgMeta(rt, mcid)
		require.True(t, found)
		prev, ok := prevs[mm.To]
		if ok {
			prevCid, _ := prev.Cid()
			_, found := h.getMsgMeta(rt, prevCid)
			// The one subnet updated should have removed the previous
			if mm.To == subs[0].String() {
				require.False(h.t, found)
			} else {
				// The rest should stil be accessible
				require.True(h.t, found)
			}
		}
		// There should be one in every subnet (because its a new one,
		// or they were either equal except for the first one where the
		// cids of the msgMeta where different.
		if mm.To == subs[0].String() {
			require.Equal(t, len(msgmeta.Metas), 2)
		} else {
			require.Equal(t, len(msgmeta.Metas), 1)
		}
	}
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
	res, ok := ret.(*actor.SubnetIDParam)
	require.True(t, ok)
	shid := address.SubnetID("/root/f0101")
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
	rt.Verify()
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

func TestFund(t *testing.T) {
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
	nn1 := sh.ID
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), "/root/f0101")
	require.Equal(t, sh.ParentID.String(), "/root")
	require.Equal(t, sh.Status, actor.Active)

	t.Log("inject some funds in subnet")
	funder := tutil.NewIDAddr(h.t, 1000)
	value = abi.NewTokenAmount(1e18)
	fund(h, rt, nn1, funder, value, 1, value, value)
	newfunder := tutil.NewIDAddr(h.t, 1001)
	fund(h, rt, nn1, newfunder, value, 2, big.Mul(big.NewInt(2), value), value)
	fund(h, rt, nn1, newfunder, value, 3, big.Mul(big.NewInt(3), value), big.Mul(big.NewInt(2), value))

	t.Log("get cross messages from nonce")
	sh, _ = h.getSubnet(rt, nn1)
	msgs, err := sh.TopDownMsgFromNonce(adt.AsStore(rt), 0)
	require.NoError(h.t, err)
	require.Equal(h.t, len(msgs), 3)
	msgs, err = sh.TopDownMsgFromNonce(adt.AsStore(rt), 2)
	require.NoError(h.t, err)
	require.Equal(h.t, len(msgs), 1)
}

func TestReleaseFunds(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	shid := address.SubnetID("/root/f0101")
	h.constructAndVerifyWithNetworkName(rt, shid)

	t.Log("release some funds from subnet")
	releaser := tutil.NewIDAddr(h.t, 1000)
	value := abi.NewTokenAmount(1e18)
	prev := release(h, rt, shid, releaser, value, 0, cid.Undef)
	release(h, rt, shid, releaser, value, 1, prev)

}

func TestApplyMsg(t *testing.T) {
	h := newHarness(t)
	builder := mock.NewBuilder(builtin.StoragePowerActorAddr).WithCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt := builder.Build(t)
	h.constructAndVerify(rt)
	h.registerSubnet(rt, address.RootSubnet)
	funder := tutil.NewIDAddr(h.t, 1000)

	// Inject some funds to test circSupply
	t.Log("inject some funds in subnet")
	init := abi.NewTokenAmount(1e18)
	fund(h, rt, h.sn, funder, init, 1, init, init)
	value := abi.NewTokenAmount(1e17)

	t.Log("apply fund messages")
	for i := 0; i < 5; i++ {
		h.applyFundMsg(rt, funder, value, uint64(i), false)
	}
	// Applying already used nonces or non-subsequent should fail
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyFundMsg(rt, funder, value, 10, true)
	})
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyFundMsg(rt, funder, value, 1, true)
	})

	// Register subnet for update in circulating supply
	releaser, err := address.NewHAddress(h.sn.Parent(), funder)
	require.NoError(t, err)

	t.Log("apply release messages")
	// Three messages with the same nonce
	for i := 0; i < 3; i++ {
		h.applyReleaseMsg(rt, releaser, value, uint64(0))
	}
	// The following with increasing nonces
	for i := 0; i < 3; i++ {
		h.applyReleaseMsg(rt, releaser, value, uint64(i))
	}
	// Check that circ supply is updated successfully.
	sh, found := h.getSubnet(rt, h.sn)
	require.True(h.t, found)
	require.Equal(h.t, sh.CircSupply, big.Sub(init, big.Mul(big.NewInt(6), value)))
	// Trying to release over the circulating supply
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyReleaseMsg(rt, releaser, init, 2)
	})
	// Applying already used nonces or non-subsequent should fail
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyReleaseMsg(rt, releaser, value, 10)
	})
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		h.applyReleaseMsg(rt, releaser, value, 1)
	})

}

func (h *shActorHarness) applyFundMsg(rt *mock.Runtime, addr address.Address, value big.Int, nonce uint64, abort bool) {
	rt.SetCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	params := &actor.ApplyParams{
		Msg: ltypes.Message{
			To:         addr,
			From:       addr,
			Value:      value,
			Nonce:      nonce,
			GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
			GasFeeCap:  ltypes.NewInt(0),
			GasPremium: ltypes.NewInt(0),
			Params:     nil,
		},
	}

	rewParams := &reward.FundingParams{
		Addr:  addr,
		Value: value,
	}
	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	if !abort {
		rt.ExpectSend(reward.RewardActorAddr, reward.Methods.ExternalFunding, rewParams, big.Zero(), nil, exitcode.Ok)
	}
	rt.Call(h.SubnetCoordActor.ApplyMessage, params)
	rt.Verify()
	st := getState(rt)
	require.Equal(h.t, st.AppliedTopDownNonce, nonce+1)
}

func (h *shActorHarness) applyReleaseMsg(rt *mock.Runtime, addr address.Address, value big.Int, nonce uint64) {
	rt.SetCaller(builtin.SystemActorAddr, builtin.SystemActorCodeID)
	rt.SetBalance(value)
	from, err := address.NewHAddress(h.sn, builtin.BurntFundsActorAddr)
	require.NoError(h.t, err)
	testSecp := tutil.NewSECP256K1Addr(h.t, "asd")
	params := &actor.ApplyParams{
		Msg: ltypes.Message{
			To:         testSecp,
			From:       from,
			Value:      value,
			Nonce:      nonce,
			GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
			GasFeeCap:  ltypes.NewInt(0),
			GasPremium: ltypes.NewInt(0),
			Params:     nil,
		},
	}

	rt.ExpectValidateCallerAddr(builtin.SystemActorAddr)
	rt.ExpectSend(testSecp, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.ApplyMessage, params)
	rt.Verify()
	st := getState(rt)
	require.Equal(h.t, st.AppliedBottomUpNonce, nonce)
}

func release(h *shActorHarness, rt *mock.Runtime, shid address.SubnetID, releaser address.Address, value big.Int, nonce uint64, prevMeta cid.Cid) cid.Cid {
	// Test SECP to use for calling
	testSecp := tutil.NewSECP256K1Addr(h.t, "asd")
	rt.SetReceived(value)
	rt.SetBalance(value)
	rt.SetCaller(releaser, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectSend(builtin.BurntFundsActorAddr, builtin.MethodSend, nil, value, nil, exitcode.Ok)
	// Expect a send to get pkey
	rt.ExpectSend(releaser, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &testSecp, exitcode.Ok)
	rt.Call(h.SubnetCoordActor.Release, nil)
	rt.Verify()

	// Check that msgMeta included in checkpoint
	windowCh := currWindowCheckpoint(rt, 0)
	_, chmeta := windowCh.CrossMsgMeta(shid, shid.Parent())
	require.NotNil(h.t, chmeta)
	cidmeta, err := chmeta.Cid()
	require.NoError(h.t, err)
	meta, found := h.getMsgMeta(rt, cidmeta)
	require.True(h.t, found)
	require.Equal(h.t, len(meta.Msgs), int(nonce+1))
	msg := meta.Msgs[nonce]

	// Comes from child
	from, err := address.NewHAddress(shid, builtin.BurntFundsActorAddr)
	require.NoError(h.t, err)
	// Goes to parent
	to, err := address.NewHAddress(shid.Parent(), testSecp)
	require.NoError(h.t, err)
	require.Equal(h.t, msg.From, from)
	// The "to" should have been updated to the secp addr
	require.Equal(h.t, msg.To, to)
	require.Equal(h.t, msg.Value, value)
	require.Equal(h.t, msg.Nonce, nonce)
	// check previous meta is removed
	if prevMeta != cid.Undef {
		_, found := h.getMsgMeta(rt, prevMeta)
		require.False(h.t, found)
	}
	// return cid of meta
	return cidmeta

}

func fund(h *shActorHarness, rt *mock.Runtime, sn address.SubnetID, funder address.Address, value abi.TokenAmount,
	expectedNonce uint64, expectedCircSupply big.Int, expectedAddrFunds abi.TokenAmount) {
	rt.SetReceived(value)
	params := &actor.SubnetIDParam{ID: sn.String()}
	rt.SetCaller(funder, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.Call(h.SubnetCoordActor.Fund, params)
	rt.Verify()
	sh, found := h.getSubnet(rt, sn)
	require.True(h.t, found)
	require.Equal(h.t, sh.CircSupply, expectedCircSupply)
	require.Equal(h.t, sh.Nonce, expectedNonce)
	require.Equal(h.t, getFunds(h.t, rt, sh, funder), expectedAddrFunds)
	msg, found, err := sh.GetTopDownMsg(adt.AsStore(rt), expectedNonce-1)
	require.NoError(h.t, err)
	require.True(h.t, found)
	// TODO: Add additional checks over msg?
	require.Equal(h.t, msg.Value, value)
	// Comes from parent network.
	from, err := address.NewHAddress(sh.ID.Parent(), funder)
	require.NoError(h.t, err)
	// Goes to subnet with same address
	to, err := address.NewHAddress(sh.ID, funder)
	require.NoError(h.t, err)
	require.Equal(h.t, msg.From, from)
	require.Equal(h.t, msg.To, to)
	require.Equal(h.t, msg.Nonce, expectedNonce-1)
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

func (h *shActorHarness) registerSubnet(rt *mock.Runtime, parent address.SubnetID) {
	SubnetActorAddr := tutil.NewIDAddr(h.t, 101)
	h.t.Log("register new subnet successfully")
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
	require.True(h.t, ok)
	shid := address.NewSubnetID(parent, SubnetActorAddr)
	// Verify the return value is correct.
	require.Equal(h.t, res.ID, shid.String())
	rt.Verify()
	require.Equal(h.t, getState(rt).TotalSubnets, uint64(1))
	h.sn = shid
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

func addMsgMeta(ch *schema.Checkpoint, from, to address.SubnetID, rand string) *schema.CrossMsgMeta {
	cb := cid.V1Builder{Codec: cid.DagCBOR, MhType: multihash.BLAKE2B_MIN + 31}
	c, _ := cb.Sum([]byte(from.String() + rand))
	m := schema.NewCrossMsgMeta(from, to)
	m.SetCid(c)
	ch.AppendMsgMeta(m)
	return m

}
func getFunds(t *testing.T, rt *mock.Runtime, sh *actor.Subnet, addr address.Address) abi.TokenAmount {
	funds, err := adt.AsBalanceTable(adt.AsStore(rt), sh.Funds)
	require.NoError(t, err)
	out, err := funds.Get(addr)
	require.NoError(t, err)
	return out
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
