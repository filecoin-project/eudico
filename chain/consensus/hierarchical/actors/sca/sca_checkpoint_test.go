package sca_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	abi "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	actors "github.com/filecoin-project/lotus/chain/consensus/actors"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	schema "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/support/mock"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	"github.com/stretchr/testify/require"
)

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
	netName := "/root/f01"
	h.constructAndVerifyWithNetworkName(rt, address.SubnetID(netName))
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
	shid := address.SubnetID(netName + "/f0101")
	// Verify the return value is correct.
	require.Equal(t, res.ID, shid.String())
	rt.Verify()
	require.Equal(t, getState(rt).TotalSubnets, uint64(1))
	// Verify instantiated subnet
	sh, found := h.getSubnet(rt, shid)
	require.True(h.t, found)
	require.Equal(t, sh.Stake, value)
	require.Equal(t, sh.ID.String(), netName+"/f0101")
	require.Equal(t, sh.ParentID.String(), netName)
	require.Equal(t, sh.Status, actor.Active)

	t.Log("commit checkpoint with cross msgs")
	epoch := abi.ChainEpoch(10)
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch := newCheckpoint(sh.ID, epoch+9)
	// Add msgMeta directed to other subnets
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child1"), "rand1", big.Zero())
	// By not adding a random string we are checking that nothing fails when to MsgMeta
	// for different subnets are propagating the same CID. This will probably never be the
	// case for honest peers, but it is an attack vector.
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child2"), "", big.Zero())
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child3"), "", big.Zero())
	// And to this subnet
	addMsgMeta(ch, sh.ID, address.SubnetID(netName), "", big.Zero())
	addMsgMeta(ch, sh.ID, address.SubnetID(netName), "rand", big.Zero())
	// And to a child from other branch (with cross-net messages)
	addMsgMeta(ch, sh.ID, address.SubnetID(netName+"/f02"), "rand", big.Zero())
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
	for i := uint64(0); i < 3; i++ {
		_, found, err = st.GetBottomUpMsgMeta(adt.AsStore(rt), i)
		require.NoError(t, err)
		require.True(t, found)
	}

	// Check msgMeta to other subnets are aggregated
	m := windowCh.CrossMsgs()
	subs := []address.SubnetID{"/root/f0102/child1", "/root/f0102/child2", "/root/f0102/child3"}
	require.Equal(t, len(m), 3)
	prevs := make(map[string]schema.CrossMsgMeta)
	for i, mm := range m {
		// Check that from has been renamed
		require.Equal(t, mm.From, netName)
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
	addMsgMeta(ch, sh.ID, address.SubnetID(netName), "r2", big.Zero())
	addMsgMeta(ch, sh.ID, address.SubnetID(netName), "r3", big.Zero())
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

	// Funding subnet so it can propagate some funds
	funder := tutil.NewIDAddr(h.t, 1000)
	value = abi.NewTokenAmount(1e18)
	fund(h, rt, sh.ID, funder, value, 1, value, value)

	t.Log("commit second checkpoint with overlapping metas and funds")
	rt.SetCaller(SubnetActorAddr, actors.SubnetActorCodeID)
	// Only subnet actors can call.
	rt.ExpectValidateCallerType(actors.SubnetActorCodeID)
	rt.SetEpoch(epoch)
	ch = newCheckpoint(sh.ID, epoch+9)
	ch.SetPrevious(prevcid)
	// Add msgMeta directed to other subnets
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child1"), "", big.Zero())
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child2"), "", big.Zero())
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child3"), "", abi.NewTokenAmount(100))
	addMsgMeta(ch, sh.ID, address.SubnetID("/root/f0102/child4"), "", abi.NewTokenAmount(100))

	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	// Expect burning some funds
	rt.ExpectSend(builtin.BurntFundsActorAddr, builtin.MethodSend, nil, abi.NewTokenAmount(200), nil, exitcode.Ok)
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
		require.Equal(t, mm.From, netName)
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
