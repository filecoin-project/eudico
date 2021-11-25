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
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
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
	RawCheckpoint         abi.MethodNum
	CommitChildCheckpoint abi.MethodNum
}{builtin0.MethodConstructor, 2, 3, 4, 5, 6, 7}

type FundParams struct {
	Value abi.TokenAmount
}

type AddSubnetReturn struct {
	Cid cid.Cid
}
type SubnetCoordActor struct{}

func (a SubnetCoordActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Register,
		3:                         a.AddStake,
		4:                         a.ReleaseStake,
		5:                         a.Kill,
		6:                         a.RawCheckpoint,
		7:                         a.CommitChildCheckpoint,
		// -1:                         a.Fund,
		// -1:                         a.Release,
		// -1:                         a.Checkpoint,
		// -1:                         a.RawCheckpoint,
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

func (a SubnetCoordActor) Constructor(rt runtime.Runtime, params ConstructorParams) *abi.EmptyValue {
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
	var shcid cid.Cid
	rt.StateTransaction(&st, func() {
		var err error
		shid := hierarchical.NewSubnetID(st.NetworkName, SubnetActorAddr)
		shcid, err = shid.Cid()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed computing CID from subnetID")
		// Check if the subnet with that ID already exists
		if _, has, _ := st.GetSubnet(adt.AsStore(rt), shcid); has {
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
			Cid:      shcid,
			ID:       shid,
			Parent:   st.Network,
			ParentID: st.NetworkName,
			Stake:    value,
			Funds:    emptyFundBalances,
			Status:   status,
		}

		// Increase the number of child subnets for the current network.
		st.TotalSubnets++

		// Flush subnet into subnetMap
		sh.flushSubnet(rt, &st)
	})

	return &AddSubnetReturn{Cid: shcid}
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

// RawCheckpoint
//
// XXX
func (a SubnetCoordActor) RawCheckpoint(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Anyone can request a Checkpoint template.
	rt.ValidateImmediateCallerAcceptAny()
	// Get current state
	var st SCAState
	rt.StateReadonly(&st)

	// Return the frozen checkpoint that is ready to sign.

	return nil
}

type CheckpointParams struct {
	Checkpoint schema.Checkpoint
}

// Checkpoint
//
// XXX
func (a SubnetCoordActor) CommitChildCheckpoint(rt runtime.Runtime, params *CheckpointParams) *abi.EmptyValue {
	// Only subnet actors are allowed to commit a chekcpoint after their
	// verification and aggregation.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)

	var st SCAState
	rt.StateTransaction(&st, func() {
		// Check the source of the checkpoint.

		// Get the checkpoint fo rthe current window.

		// Check that the previous checkpoint is correct.

	})
	// Store checkpoint for the current epoch.
	// Make some verifications that the previous one is the connected to the current one
	// and maybe that its epoch is the right one according to the period.
	// If there is already a checkpoint for that subnet do not accepet it.
	// We could probably even check if it is the same CID to allow disputed,
	// but lets leave that for the future.
	return nil
}

// Kill
//
// Unregisters a subnet from the hierarchical consensus
func (a SubnetCoordActor) Kill(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Can only be called by an actor implementing the subnet actor interface.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	SubnetActorAddr := rt.Caller()

	var st SCAState
	var sh *Subnet
	rt.StateTransaction(&st, func() {
		var has bool
		shid := hierarchical.NewSubnetID(st.NetworkName, SubnetActorAddr)
		shcid, err := shid.Cid()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed computing CID from subnetID")
		// Check if the subnet for the actor exists
		sh, has, err = st.GetSubnet(adt.AsStore(rt), shcid)
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
		err = subnets.Delete(abi.CidKey(shcid))
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

// addStake adds new funds to the stake of the subnet.
//
// This function also accepts negative values to substract, and checks
// if the funds are enough for the subnet to be active.
func (sh *Subnet) addStake(rt runtime.Runtime, st *SCAState, value abi.TokenAmount) {
	// Add stake to the subnet
	sh.Stake = big.Add(sh.Stake, value)

	// Check if subnet has still stake to be active
	if sh.Stake.LessThan(st.MinStake) {
		sh.Status = Inactive
	}

	// Flush subnet into subnetMap
	sh.flushSubnet(rt, st)

}

func (sh *Subnet) flushSubnet(rt runtime.Runtime, st *SCAState) {
	// Update subnet in the list of subnets.
	subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for subnets")
	err = subnets.Put(abi.CidKey(sh.Cid), sh)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new subnet in subnet map")
	// Flush subnets
	st.Subnets, err = subnets.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush subnets")
}

// currWindowCheckpoint gets the template of the checkpoint being
// populated in the current window.
//
// If it hasn't been instantiated, a template is created. From there on,
// the template is populated with every new xShard transaction and
// child checkpoint, until the windows passes that the template is frozen
// and is ready for miners to populate the rest and sign it.
func (st *SCAState) currWindowCheckpoint(rt runtime.Runtime) *schema.Checkpoint {
	chEpoch := types.WindowEpoch(rt.CurrEpoch(), st.CheckPeriod)
	ch, found, err := st.GetCheckpoint(adt.AsStore(rt), chEpoch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get checkpoint template for epoch")
	if !found {
		ch = schema.NewRawCheckpoint(st.NetworkName, chEpoch)
	}
	return ch
}
