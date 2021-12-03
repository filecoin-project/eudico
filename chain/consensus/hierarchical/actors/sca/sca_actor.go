package sca

//go:generate go run ./gen/gen.go

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	builtin0 "github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
)

var _ runtime.VMActor = SubnetCoordActor{}

// SubnetCoordActorAddr is initialized in genesis with the
// address t064
var SubnetCoordActorAddr = func() address.Address {
	a, err := address.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()

var Methods = struct {
	Constructor           abi.MethodNum
	Register              abi.MethodNum
	AddStake              abi.MethodNum
	ReleaseStake          abi.MethodNum
	Kill                  abi.MethodNum
	CommitChildCheckpoint abi.MethodNum
}{builtin0.MethodConstructor, 2, 3, 4, 5, 6}

type FundParams struct {
	Value abi.TokenAmount
}

type AddSubnetReturn struct {
	ID string
}
type SubnetCoordActor struct{}

func (a SubnetCoordActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Register,
		3:                         a.AddStake,
		4:                         a.ReleaseStake,
		5:                         a.Kill,
		6:                         a.CommitChildCheckpoint,
		// -1:                         a.Fund,
		// -1:                         a.Release,
		// -1:                         a.XSubnetTx,
	}
}

func (a SubnetCoordActor) Code() cid.Cid {
	return actor.SubnetCoordActorCodeID
}

func (a SubnetCoordActor) IsSingleton() bool {
	return true
}

func (a SubnetCoordActor) State() cbor.Er {
	return new(SCAState)
}

type ConstructorParams struct {
	NetworkName      string
	CheckpointPeriod uint64
}

func (a SubnetCoordActor) Constructor(rt runtime.Runtime, params *ConstructorParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)
	st, err := ConstructSCAState(adt.AsStore(rt), params)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

// Register
//
// It registers a new subnet actor to the hierarchical consensus.
// In order for the registering of a subnet to be successful, the transaction
// needs to stake at least the minimum stake, if not it'll fail.
func (a SubnetCoordActor) Register(rt runtime.Runtime, _ *abi.EmptyValue) *AddSubnetReturn {
	// Register can only be called by an actor implementing the subnet actor interface.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	SubnetActorAddr := rt.Caller()

	var st SCAState
	var shid hierarchical.SubnetID
	rt.StateTransaction(&st, func() {
		var err error
		shid = hierarchical.NewSubnetID(st.NetworkName, SubnetActorAddr)
		// Check if the subnet with that ID already exists
		if _, has, _ := st.GetSubnet(adt.AsStore(rt), shid); has {
			rt.Abortf(exitcode.ErrIllegalArgument, "can't register a subnet that has been already registered")
		}
		// Check if the transaction has enough funds to register the subnet.
		value := rt.ValueReceived()
		if value.LessThanEqual(st.MinStake) {
			rt.Abortf(exitcode.ErrIllegalArgument, "call to register doesn't include enough funds to stake")
		}

		// We always initialize in instantiated state
		status := Active

		// Instatiate the subnet state
		emptyFundBalances, err := adt.StoreEmptyMap(adt.AsStore(rt), adt.BalanceTableBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty funds balance table")

		sh := &Subnet{
			ID:             shid,
			ParentID:       st.NetworkName,
			Stake:          value,
			Funds:          emptyFundBalances,
			Status:         status,
			PrevCheckpoint: *schema.EmptyCheckpoint,
		}

		// Increase the number of child subnets for the current network.
		st.TotalSubnets++

		// Flush subnet into subnetMap
		st.flushSubnet(rt, sh)
	})

	return &AddSubnetReturn{ID: shid.String()}
}

// AddStake
//
// Locks more stake from an actor. This needs to be triggered
// by the subnet actor with the subnet logic.
func (a SubnetCoordActor) AddStake(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Can only be called by an actor implementing the subnet actor interface.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	SubnetActorAddr := rt.Caller()

	var st SCAState
	rt.StateTransaction(&st, func() {
		// Check if the subnet for the actor exists
		sh, has, err := st.getSubnetFromActorAddr(adt.AsStore(rt), SubnetActorAddr)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for actor hasn't been registered yet")
		}

		// Check if the transaction includes funds
		value := rt.ValueReceived()
		if value.LessThanEqual(big.NewInt(0)) {
			rt.Abortf(exitcode.ErrIllegalArgument, "no funds included in transaction")
		}

		// Increment stake locked for subnet.
		sh.addStake(rt, &st, value)
	})

	return nil
}

// ReleaseStake
//
// Request from the subnet actor to release part of the stake locked for subnet.
// Is up to the subnet actor to do the corresponding verifications and
// distribute the funds to its owners.
func (a SubnetCoordActor) ReleaseStake(rt runtime.Runtime, params *FundParams) *abi.EmptyValue {
	// Can only be called by an actor implementing the subnet actor interface.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	SubnetActorAddr := rt.Caller()

	if params.Value.LessThanEqual(abi.NewTokenAmount(0)) {
		rt.Abortf(exitcode.ErrIllegalArgument, "no funds included in params")
	}
	var st SCAState
	rt.StateTransaction(&st, func() {
		// Check if the subnet for the actor exists
		sh, has, err := st.getSubnetFromActorAddr(adt.AsStore(rt), SubnetActorAddr)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for for actor hasn't been registered yet")
		}

		// Check if the subnet actor is allowed to release the amount of stake specified.
		if sh.Stake.LessThan(params.Value) {
			rt.Abortf(exitcode.ErrIllegalState, "subnet actor not allowed to release that many funds")
		}

		// This is a sanity check to ensure that there is enough balance in actor.
		if rt.CurrentBalance().LessThan(params.Value) {
			rt.Abortf(exitcode.ErrIllegalState, "yikes! actor doesn't have enough balance to release these funds")
		}

		// Decrement locked stake
		sh.addStake(rt, &st, params.Value.Neg())
	})

	// Send a transaction with the funds to the subnet actor.
	code := rt.Send(SubnetActorAddr, builtin.MethodSend, nil, params.Value, &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed sending released stake to subnet actor")
	}

	return nil
}

// CheckpointParams handles in/out communication of checkpoints
// To accommodate arbitrary schemas (and even if it introduces and overhead)
// is easier to transmit a marshalled version of the checkpoint.
// NOTE: Consider in the future if there is a better approach.
type CheckpointParams struct {
	Checkpoint []byte
}

// CommitChildCheckpoint accepts a checkpoint from a subnet for commitment.
//
// The subnet is responsible for running all the deep verifications about the checkpoint,
// the SCA is only able to enforce some basic verifications.
func (a SubnetCoordActor) CommitChildCheckpoint(rt runtime.Runtime, params *CheckpointParams) *abi.EmptyValue {
	// Only subnet actors are allowed to commit a checkpoint after their
	// verification and aggregation.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	commit := &schema.Checkpoint{}
	err := commit.UnmarshalBinary(params.Checkpoint)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error unmarshalling checkpoint in params")
	subnetActorAddr := rt.Caller()

	// Check the source of the checkpoint.
	source, err := hierarchical.SubnetID(commit.Data.Source).Actor()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error getting checkpoint source")
	if source != subnetActorAddr {
		rt.Abortf(exitcode.ErrIllegalArgument, "checkpoint committed doesn't belong to source subnet")
	}

	// TODO: We could optionally check here if the checkpoint includes a valid signature. I don't
	// think this makes sense as in its current implementation the subnet actor receives an
	// independent signature for each miner and counts the number of "votes" for the checkpoint.
	var st SCAState
	rt.StateTransaction(&st, func() {
		// Check that the subnet is registered and active
		shid := hierarchical.NewSubnetID(st.NetworkName, subnetActorAddr)
		// Check if the subnet for the actor exists
		sh, has, err := st.GetSubnet(adt.AsStore(rt), shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for for actor hasn't been registered yet")
		}
		// Check that it is active. Only active shards can commit checkpoints.
		if sh.Status != Active {
			rt.Abortf(exitcode.ErrIllegalState, "can't commit a checkpoint for a subnet that is not active")
		}
		// Get the checkpoint for the current window.
		ch := st.currWindowCheckpoint(rt)

		// Verify that the submitted checkpoint has higher epoch and is
		// consistent with previous checkpoint before committing.
		prevCom := sh.PrevCheckpoint
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching previous child checkpoint from state")
		// If no previous checkpoint for child chain, it means this is the first one
		// and we can add it without additional verifications.
		if empty, _ := prevCom.IsEmpty(); empty {
			// Append the new checkpoint to the list of childs.
			err := ch.AddChild(commit)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "child already has committed a checkpoint this epoch")
			st.flushCheckpoint(rt, ch)
			// Update previous checkpoint for child.
			sh.PrevCheckpoint = *commit
			st.flushSubnet(rt, sh)
			return
		}

		// Check that the epoch is consistent.
		if prevCom.Data.Epoch > commit.Data.Epoch {
			rt.Abortf(exitcode.ErrIllegalArgument, "new checkpoint being committed belongs to the past")
		}

		// Check that the previous Cid is consistent with the committed one.
		prevCid, err := prevCom.Cid()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error computing checkpoint's Cid")
		if pr, _ := commit.PreviousCheck(); prevCid != pr {
			rt.Abortf(exitcode.ErrIllegalArgument, "new checkpoint not consistent with previous one")
		}

		// Checks passed, we can append the child.
		err = ch.AddChild(commit)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "child already has committed a checkpoint this epoch")
		st.flushCheckpoint(rt, ch)
		// Update previous checkpoint for child.
		sh.PrevCheckpoint = *commit
		st.flushSubnet(rt, sh)
	})

	return nil
}

// Kill unregisters a subnet from the hierarchical consensus
func (a SubnetCoordActor) Kill(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Can only be called by an actor implementing the subnet actor interface.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	SubnetActorAddr := rt.Caller()

	var st SCAState
	var sh *Subnet
	rt.StateTransaction(&st, func() {
		var has bool
		shid := hierarchical.NewSubnetID(st.NetworkName, SubnetActorAddr)
		// Check if the subnet for the actor exists
		var err error
		sh, has, err = st.GetSubnet(adt.AsStore(rt), shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for for actor hasn't been registered yet")
		}

		// This is a sanity check to ensure that there is enough balance in actor to return stakes
		if rt.CurrentBalance().LessThan(sh.Stake) {
			rt.Abortf(exitcode.ErrIllegalState, "yikes! actor doesn't have enough balance to release these funds")
		}

		// Remove subnet from subnet registry.
		subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for subnets")
		err = subnets.Delete(shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to remove miner stake in stake map")
		// Flush stakes adding miner stake.
		st.Subnets, err = subnets.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush subnets after removal")
	})

	// Send a transaction with the total stake to the subnet actor.
	code := rt.Send(SubnetActorAddr, builtin.MethodSend, nil, sh.Stake, &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed sending released stake to subnet actor")
	}

	return nil
}
