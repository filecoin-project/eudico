package subnet_test

import (
	"context"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	checkpoint "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/utils"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

func TestExports(t *testing.T) {
	mock.CheckActorExports(t, actor.SubnetActor{})
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

	t.Log("join new subnet without enough funds to register")
	value := abi.NewTokenAmount(5e17)
	rt.SetCaller(notMiner, builtin.AccountActorCodeID)
	rt.SetReceived(value)
	rt.SetBalance(value)
	// Anyone can call
	rt.ExpectValidateCallerAny()
	ret := rt.Call(h.SubnetActor.Join, nil)
	rt.Verify()
	assert.Nil(h.t, ret)
	// Check that the subnet is instantiated but not active.
	st := getState(rt)
	require.Equal(t, len(st.Miners), 0)
	require.Equal(t, st.Status, actor.Instantiated)
	require.Equal(t, getStake(t, rt, notMiner), value)
	totalStake = big.Add(totalStake, value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("new miner join the subnet and activates it")
	value = abi.NewTokenAmount(1e18)
	rt.SetReceived(value)
	totalStake = big.Add(totalStake, value)
	rt.SetBalance(totalStake)
	rt.SetCaller(miner, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.Register, nil, totalStake, nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Join, nil)
	rt.Verify()
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
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Join, nil)
	rt.Verify()
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
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.Register, nil, totalStake, nil, exitcode.Ok)
	ret := rt.Call(h.SubnetActor.Join, nil)
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
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Join, nil)
	rt.Verify()
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
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Join, nil)
	rt.Verify()
	// Check that we are active
	st = getState(rt)
	require.Equal(t, len(st.Miners), 2)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, getStake(t, rt, joiner3), value)
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("second joiner leaves the subnet")
	rt.ExpectValidateCallerAny()
	rt.SetCaller(joiner2, builtin.AccountActorCodeID)
	minerStake := getStake(t, rt, joiner2)
	totalStake = big.Sub(totalStake, minerStake)
	rt.SetBalance(minerStake)
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.ReleaseStake, &sca.FundParams{Value: minerStake}, big.Zero(), nil, exitcode.Ok)
	rt.ExpectSend(joiner2, builtin.MethodSend, nil, big.Div(minerStake, actor.LeavingFeeCoeff), nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Leave, nil)
	rt.Verify()
	st = getState(rt)
	require.Equal(t, st.Status, actor.Active)
	require.Equal(t, len(st.Miners), 1)
	require.Equal(t, getStake(t, rt, joiner2), big.Zero())
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("subnet can't be killed if there are still miners")
	rt.ExpectValidateCallerAny()
	rt.SetCaller(joiner2, builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.SubnetActor.Kill, nil)
	})

	t.Log("first joiner inactivates the subnet")
	rt.ExpectValidateCallerAny()
	rt.SetCaller(joiner, builtin.AccountActorCodeID)
	minerStake = getStake(t, rt, joiner)
	totalStake = big.Sub(totalStake, minerStake)
	rt.SetBalance(minerStake)
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.ReleaseStake, &sca.FundParams{Value: minerStake}, big.Zero(), nil, exitcode.Ok)
	rt.ExpectSend(joiner, builtin.MethodSend, nil, big.Div(minerStake, actor.LeavingFeeCoeff), nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Leave, nil)
	rt.Verify()
	st = getState(rt)
	require.Equal(t, st.Status, actor.Inactive)
	require.Equal(t, len(st.Miners), 0)
	require.Equal(t, getStake(t, rt, joiner), big.Zero())
	require.Equal(t, st.TotalStake, totalStake)

	t.Log("miner can't leave twice")
	rt.ExpectValidateCallerAny()
	rt.ExpectAbort(exitcode.ErrForbidden, func() {
		rt.Call(h.SubnetActor.Leave, nil)
	})

	t.Log("third kills the subnet, and takes its stake")
	minerStake = getStake(t, rt, joiner3)
	rt.SetCaller(joiner3, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerAny()
	rt.SetBalance(minerStake)
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.Kill, nil, big.Zero(), nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Kill, nil)
	rt.Verify()
	st = getState(rt)
	require.Equal(t, st.Status, actor.Terminating)

	t.Log("subnet can't be killed twice")
	rt.ExpectValidateCallerAny()
	rt.ExpectAbort(exitcode.ErrIllegalState, func() {
		rt.Call(h.SubnetActor.Kill, nil)
	})

	rt.ExpectValidateCallerAny()
	totalStake = big.Sub(totalStake, minerStake)
	rt.SetBalance(minerStake)
	rt.ExpectSend(joiner3, builtin.MethodSend, nil, big.Div(minerStake, actor.LeavingFeeCoeff), nil, exitcode.Ok)
	rt.Call(h.SubnetActor.Leave, nil)
	rt.Verify()
	st = getState(rt)
	require.Equal(t, st.Status, actor.Killed)
	require.Equal(t, len(st.Miners), 0)
	require.Equal(t, getStake(t, rt, joiner3), big.Zero())
	require.Equal(t, st.TotalStake.Abs(), totalStake.Abs())

}

func TestCheckpoints(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerify(t, rt)
	w, err := wallet.NewWallet(wallet.NewMemKeyStore())
	if err != nil {
		t.Fatal(err)
	}
	miners := []address.Address{}
	for i := 0; i < 3; i++ {
		addr, err := w.WalletNew(ctx, types.KTSecp256k1)
		require.NoError(t, err)
		miners = append(miners, addr)
	}
	totalStake := abi.NewTokenAmount(0)

	t.Log("three miners join subnet")
	for i, m := range miners {
		value := abi.NewTokenAmount(1e18)
		rt.SetCaller(m, builtin.AccountActorCodeID)
		rt.SetReceived(value)
		rt.SetBalance(value)
		totalStake = big.Add(totalStake, value)
		// Anyone can call
		rt.ExpectValidateCallerAny()
		// The first miner triggers a register message to SCA
		if i == 0 {
			rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.Register, nil, totalStake, nil, exitcode.Ok)
		} else {
			rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.AddStake, nil, value, nil, exitcode.Ok)
		}
		ret := rt.Call(h.SubnetActor.Join, nil)
		rt.Verify()
		assert.Nil(h.t, ret)
	}
	st := getState(rt)
	require.Equal(t, len(st.Miners), 3)
	require.Equal(t, st.Status, actor.Active)

	ver := checkpoint.NewSingleSigner()
	addr := tutil.NewIDAddr(t, 100)
	shid := address.NewSubnetID(address.RootSubnet, addr)

	t.Log("checkpoint in first and second epoch from three miners")
	h.fullSignCheckpoint(t, rt, miners, w, st.CheckPeriod)
	h.fullSignCheckpoint(t, rt, miners, w, 2*st.CheckPeriod)

	t.Log("submit in next epoch")
	st = getState(rt)
	// Submit in the next epoch
	epoch := 3 * st.CheckPeriod
	ch := schema.NewRawCheckpoint(shid, epoch)
	// Add child checkpoints
	ch.AddListChilds(utils.GenRandChecks(3))
	// Sign
	err = ver.Sign(ctx, w, miners[0], ch)
	require.NoError(t, err)

	// Submit checkpoint from first miner in second period
	rt.SetCaller(miners[0], builtin.AccountActorCodeID)
	rt.SetEpoch(epoch + 20)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	b, err := ch.MarshalBinary()
	require.NoError(t, err)
	// The previous checkpoint fails
	params := &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	})
	// Set the right previous checkpoint and send
	// without re-signing so it will fail
	prevcid, err := st.PrevCheckCid(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	ch.SetPrevious(prevcid)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	})
	// Now sign and send and it should be correct
	err = ver.Sign(ctx, w, miners[0], ch)
	require.NoError(t, err)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	rt.Verify()
	st = getState(rt)
	chcid, err := ch.Cid()
	require.NoError(t, err)
	wch, found, err := st.GetWindowChecks(adt.AsStore(rt), chcid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, len(wch.Miners), 1)
	// No checkpoint committed for that epoch
	_, found, err = st.GetCheckpoint(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	require.False(t, found)

	t.Log("submit next epoch when previous was not committed")
	// Submit in the next epoch
	epoch = 4 * st.CheckPeriod
	ch = schema.NewRawCheckpoint(shid, epoch)
	prevcid, err = st.PrevCheckCid(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	ch.SetPrevious(prevcid)
	// Add child checkpoints
	ch.AddListChilds(utils.GenRandChecks(3))
	// Sign
	err = ver.Sign(ctx, w, miners[0], ch)
	require.NoError(t, err)

	// Submit checkpoint from first miner in third period
	rt.SetCaller(miners[0], builtin.AccountActorCodeID)
	rt.SetEpoch(epoch + 20)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	// Now sign and send and it should be correct
	err = ver.Sign(ctx, w, miners[0], ch)
	require.NoError(t, err)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	rt.Verify()
	st = getState(rt)
	chcid, err = ch.Cid()
	require.NoError(t, err)
	wch, found, err = st.GetWindowChecks(adt.AsStore(rt), chcid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, len(wch.Miners), 1)
	// No checkpoint committed for that epoch
	_, found, err = st.GetCheckpoint(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	require.False(t, found)

	// Submit checkpoint from second miner
	rt.SetCaller(miners[1], builtin.AccountActorCodeID)
	rt.SetEpoch(epoch + 22)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	err = ver.Sign(ctx, w, miners[1], ch)
	require.NoError(t, err)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.CommitChildCheckpoint, params, big.Zero(), nil, exitcode.Ok)
	rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	rt.Verify()
	st = getState(rt)
	chcid, err = ch.Cid()
	require.NoError(t, err)
	// WindowChecks cleaned
	_, found, err = st.GetWindowChecks(adt.AsStore(rt), chcid)
	require.NoError(t, err)
	require.False(t, found)
	ch, found, err = st.GetCheckpoint(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	require.True(t, found)
	comcid, err := ch.Cid()
	require.NoError(t, err)
	require.Equal(t, comcid, chcid)
}

func TestZeroCheckPeriod(t *testing.T) {
	h := newHarness(t)
	rt := getRuntime(t)
	h.constructAndVerifyZeroCheck(t, rt)
}

type shActorHarness struct {
	actor.SubnetActor
	t *testing.T
}

func newHarness(t *testing.T) *shActorHarness {
	return &shActorHarness{
		SubnetActor: actor.SubnetActor{},
		t:           t,
	}
}

func (h *shActorHarness) constructAndVerify(t *testing.T, rt *mock.Runtime) {
	rt.ExpectValidateCallerType(builtin.InitActorCodeID)
	ret := rt.Call(h.SubnetActor.Constructor,
		&actor.ConstructParams{
			NetworkName:   address.RootSubnet.String(),
			Name:          "myTestSubnet",
			Consensus:     hierarchical.PoW,
			MinMinerStake: actor.MinMinerStake,
			DelegMiner:    tutil.NewIDAddr(t, 101),
			CheckPeriod:   abi.ChainEpoch(100),
		})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.SubnetState

	rt.GetState(&st)
	assert.Equal(h.t, st.ParentID, address.RootSubnet)
	assert.Equal(h.t, st.Consensus, hierarchical.PoW)
	assert.Equal(h.t, st.MinMinerStake, actor.MinMinerStake)
	assert.Equal(h.t, st.Status, actor.Instantiated)
	assert.Equal(h.t, st.CheckPeriod, abi.ChainEpoch(100))
	// Verify that the genesis for the subnet has been generated.
	// TODO: Consider making some test verifications over genesis.
	assert.NotEqual(h.t, len(st.Genesis), 0)
	verifyEmptyMap(h.t, rt, st.Stake)
	verifyEmptyMap(h.t, rt, st.Checkpoints)
	verifyEmptyMap(h.t, rt, st.WindowChecks)
}

// Check what happens if we set a check period equal to zero.
// We should be assigning the default period.
func (h *shActorHarness) constructAndVerifyZeroCheck(t *testing.T, rt *mock.Runtime) {
	rt.ExpectValidateCallerType(builtin.InitActorCodeID)
	ret := rt.Call(h.SubnetActor.Constructor,
		&actor.ConstructParams{
			NetworkName:   address.RootSubnet.String(),
			Name:          "myTestSubnet",
			Consensus:     hierarchical.PoW,
			MinMinerStake: actor.MinMinerStake,
			DelegMiner:    tutil.NewIDAddr(t, 101),
		})
	assert.Nil(h.t, ret)
	rt.Verify()

	var st actor.SubnetState

	rt.GetState(&st)
	assert.Equal(h.t, st.CheckPeriod, sca.DefaultCheckpointPeriod)
}

func verifyEmptyMap(t testing.TB, rt *mock.Runtime, cid cid.Cid) {
	mapChecked, err := adt.AsMap(adt.AsStore(rt), cid, builtin.DefaultHamtBitwidth)
	assert.NoError(t, err)
	keys, err := mapChecked.CollectKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func getRuntime(t *testing.T) *mock.Runtime {
	SubnetActorAddr := tutil.NewIDAddr(t, 100)
	builder := mock.NewBuilder(SubnetActorAddr).WithCaller(builtin.InitActorAddr, builtin.InitActorCodeID)
	return builder.Build(t)
}

func getState(rt *mock.Runtime) *actor.SubnetState {
	var st actor.SubnetState
	rt.GetState(&st)
	return &st
}

func getStake(t *testing.T, rt *mock.Runtime, addr address.Address) abi.TokenAmount {
	var st actor.SubnetState
	rt.GetState(&st)
	stakes, err := adt.AsBalanceTable(adt.AsStore(rt), st.Stake)
	require.NoError(t, err)
	out, err := stakes.Get(addr)
	require.NoError(t, err)
	return out
}

func (h *shActorHarness) fullSignCheckpoint(t *testing.T, rt *mock.Runtime, miners []address.Address, w api.Wallet, epoch abi.ChainEpoch) {
	st := getState(rt)
	ctx := context.Background()
	var err error
	ver := checkpoint.NewSingleSigner()
	addr := tutil.NewIDAddr(t, 100)
	shid := address.NewSubnetID(address.RootSubnet, addr)
	ch := schema.NewRawCheckpoint(shid, epoch)
	prevcid, err := st.PrevCheckCid(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	ch.SetPrevious(prevcid)
	// Add child checkpoints
	ch.AddListChilds(utils.GenRandChecks(3))
	// Sign
	err = ver.Sign(ctx, w, miners[0], ch)
	require.NoError(t, err)
	// Submit checkpoint from first miner
	rt.SetCaller(miners[0], builtin.AccountActorCodeID)
	rt.SetEpoch(epoch + 20)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	b, err := ch.MarshalBinary()
	require.NoError(t, err)
	params := &sca.CheckpointParams{Checkpoint: b}
	rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	st = getState(rt)
	chcid, err := ch.Cid()
	require.NoError(t, err)
	wch, found, err := st.GetWindowChecks(adt.AsStore(rt), chcid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, len(wch.Miners), 1)
	// No checkpoint committed for that epoch
	_, found, err = st.GetCheckpoint(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	require.False(t, found)

	// Can't send checkpoint for the same miner twice
	rt.SetCaller(miners[0], builtin.AccountActorCodeID)
	rt.SetEpoch(epoch + 21)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	})

	// Check if the epoch is wrong.
	chbad := schema.NewRawCheckpoint(shid, epoch+1)
	b, err = chbad.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	err = ver.Sign(ctx, w, miners[0], ch)
	require.NoError(t, err)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	})

	// Check if the miner is wrong.
	nonminer, err := w.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)
	rt.SetCaller(nonminer, builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	err = ver.Sign(ctx, w, nonminer, ch)
	require.NoError(t, err)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectAbort(exitcode.ErrIllegalArgument, func() {
		rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	})

	// Submit checkpoint from second miner
	rt.SetCaller(miners[1], builtin.AccountActorCodeID)
	rt.ExpectValidateCallerType(builtin.AccountActorCodeID)
	err = ver.Sign(ctx, w, miners[1], ch)
	require.NoError(t, err)
	b, err = ch.MarshalBinary()
	require.NoError(t, err)
	params = &sca.CheckpointParams{Checkpoint: b}
	rt.ExpectSend(hierarchical.SubnetCoordActorAddr, sca.Methods.CommitChildCheckpoint, params, big.Zero(), nil, exitcode.Ok)
	rt.Call(h.SubnetActor.SubmitCheckpoint, params)
	st = getState(rt)
	chcid, err = ch.Cid()
	require.NoError(t, err)
	// WindowChecks cleaned
	_, found, err = st.GetWindowChecks(adt.AsStore(rt), chcid)
	require.NoError(t, err)
	require.False(t, found)
	// WindowChecks cleaned
	ch, found, err = st.GetCheckpoint(adt.AsStore(rt), epoch)
	require.NoError(t, err)
	require.True(t, found)
	comcid, err := ch.Cid()
	require.NoError(t, err)
	require.Equal(t, comcid, chcid)
}
