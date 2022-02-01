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
	types "github.com/filecoin-project/lotus/chain/types"
	builtin0 "github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
)

var _ runtime.VMActor = SubnetCoordActor{}

var Methods = struct {
	Constructor           abi.MethodNum
	Register              abi.MethodNum
	AddStake              abi.MethodNum
	ReleaseStake          abi.MethodNum
	Kill                  abi.MethodNum
	CommitChildCheckpoint abi.MethodNum
	Fund                  abi.MethodNum
	Release               abi.MethodNum
	SendCross             abi.MethodNum
	ApplyMessage          abi.MethodNum
}{builtin0.MethodConstructor, 2, 3, 4, 5, 6, 7, 8, 9, 10}

type SubnetIDParam struct {
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
		7:                         a.Fund,
		8:                         a.Release,
		9:                         a.SendCross,
		10:                        a.ApplyMessage,
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
func (a SubnetCoordActor) Register(rt runtime.Runtime, _ *abi.EmptyValue) *SubnetIDParam {
	// Register can only be called by an actor implementing the subnet actor interface.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	SubnetActorAddr := rt.Caller()

	var st SCAState
	var shid address.SubnetID
	rt.StateTransaction(&st, func() {
		shid = address.NewSubnetID(st.NetworkName, SubnetActorAddr)
		// Check if the subnet with that ID already exists
		if _, has, _ := st.GetSubnet(adt.AsStore(rt), shid); has {
			rt.Abortf(exitcode.ErrIllegalArgument, "can't register a subnet that has been already registered")
		}
		// Check if the transaction has enough funds to register the subnet.
		value := rt.ValueReceived()
		if value.LessThanEqual(st.MinStake) {
			rt.Abortf(exitcode.ErrIllegalArgument, "call to register doesn't include enough funds to stake")
		}

		// Create the new subnet and register in SCA
		st.registerSubnet(rt, shid, value)
	})

	return &SubnetIDParam{ID: shid.String()}
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

type FundParams struct {
	Value abi.TokenAmount
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
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for actor hasn't been registered yet")
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
// the SCA is only able to enforce some basic consistency verifications.
func (a SubnetCoordActor) CommitChildCheckpoint(rt runtime.Runtime, params *CheckpointParams) *abi.EmptyValue {
	// Only subnet actors are allowed to commit a checkpoint after their
	// verification and aggregation.
	rt.ValidateImmediateCallerType(actor.SubnetActorCodeID)
	commit := &schema.Checkpoint{}
	err := commit.UnmarshalBinary(params.Checkpoint)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error unmarshalling checkpoint in params")
	subnetActorAddr := rt.Caller()

	// Check the source of the checkpoint.
	source, err := address.SubnetID(commit.Data.Source).Actor()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error getting checkpoint source")
	if source != subnetActorAddr {
		rt.Abortf(exitcode.ErrIllegalArgument, "checkpoint committed doesn't belong to source subnet")
	}

	// TODO: We could optionally check here if the checkpoint includes a valid signature. I don't
	// think this makes sense as in its current implementation the subnet actor receives an
	// independent signature for each miner and counts the number of "votes" for the checkpoint.
	var st SCAState
	// burnValue keeps track of the funds that are leaving the subnet in msgMeta and
	// that need to be burnt.
	burnValue := abi.NewTokenAmount(0)
	rt.StateTransaction(&st, func() {
		// Check that the subnet is registered and active
		shid := address.NewSubnetID(st.NetworkName, subnetActorAddr)
		// Check if the subnet for the actor exists
		sh, has, err := st.GetSubnet(adt.AsStore(rt), shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for actor hasn't been registered yet")
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

		// If no previous checkpoint for child chain, it means this is the first one
		// and we can add it without additional verifications.
		if empty, _ := prevCom.IsEmpty(); empty {
			// Apply cross messages from child checkpoint
			burnValue = st.applyCheckMsgs(rt, ch, commit)
			// Append the new checkpoint to the list of childs.
			err := ch.AddChild(commit)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error committing checkpoint to this epoch")
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

		// Apply cross messages from child checkpoint
		burnValue = st.applyCheckMsgs(rt, ch, commit)
		// Checks passed, we can append the child.
		err = ch.AddChild(commit)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error committing checkpoint to this epoch")
		st.flushCheckpoint(rt, ch)
		// Update previous checkpoint for child.
		sh.PrevCheckpoint = *commit
		st.flushSubnet(rt, sh)
	})

	// Burn funds leaving in metas the subnet
	if burnValue.GreaterThan(abi.NewTokenAmount(0)) {
		code := rt.Send(builtin.BurntFundsActorAddr, builtin.MethodSend, nil, burnValue, &builtin.Discard{})
		if !code.IsSuccess() {
			rt.Abortf(exitcode.ErrIllegalState,
				"failed to burn funds from msgmeta, code: %v", code)
		}
	}
	return nil
}

// applyCheckMsgs prepares messages to trigger their execution or propagate cross-messages
// coming from a checkpoint of a child subnet.
func (st *SCAState) applyCheckMsgs(rt runtime.Runtime, windowCh *schema.Checkpoint, childCh *schema.Checkpoint) abi.TokenAmount {

	burnValue := abi.NewTokenAmount(0)
	// aux map[to]CrossMsg
	aux := make(map[string][]schema.CrossMsgMeta)
	for _, mm := range childCh.CrossMsgs() {
		// if it is directed to this subnet, or another child of the subnet,
		// add it to top-down messages
		// for the consensus algorithm in the subnet to pick it up.
		if mm.To == st.NetworkName.String() ||
			!hierarchical.IsBottomUp(st.NetworkName, address.SubnetID(mm.To)) {
			// Add to BottomUpMsgMeta
			st.storeBottomUpMsgMeta(rt, mm)
		} else {
			// Check if it comes from a valid child, i.e. we are their parent.
			if address.SubnetID(mm.From).Parent() != st.NetworkName {
				// Someone is trying to forge a cross-msgs into the checkpoint
				// from a network from which we are not a parent.
				continue
			}
			// If not add to the aux structure to update the checkpoint when we've
			// gone through all crossMsgs
			_, ok := aux[mm.To]
			if !ok {
				aux[mm.To] = []schema.CrossMsgMeta{mm}
			} else {
				aux[mm.To] = append(aux[mm.To], mm)
			}

		}
		// Value leaving in a crossMsgMeta needs to be burnt to update circ.supply
		v, err := mm.GetValue()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting value from meta")
		burnValue = big.Add(burnValue, v)
		st.releaseCircSupply(rt, address.SubnetID(mm.From), v)
	}

	// Aggregate all the msgsMeta directed to other subnets in the hierarchy
	// into the checkpoint
	st.aggChildMsgMeta(rt, windowCh, aux)

	return burnValue
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
		shid := address.NewSubnetID(st.NetworkName, SubnetActorAddr)
		// Check if the subnet for the actor exists
		var err error
		sh, has, err = st.GetSubnet(adt.AsStore(rt), shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "subnet for actor hasn't been registered yet")
		}

		// This is a sanity check to ensure that there is enough balance in actor to return stakes
		if rt.CurrentBalance().LessThan(sh.Stake) {
			rt.Abortf(exitcode.ErrIllegalState, "yikes! actor doesn't have enough balance to release these funds")
		}

		// TODO: We should prevent a subnet from being killed if it still has user funds in circulation.
		// We haven't figured out how to handle this yet, so in the meantime we just prevent from being able to kill
		// the subnet when there are pending funds
		if sh.CircSupply.GreaterThan(big.Zero()) {
			rt.Abortf(exitcode.ErrForbidden, "you can't kill a subnet where users haven't released their funds yet")
		}

		// Remove subnet from subnet registry.
		subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for subnets")
		err = subnets.Delete(hierarchical.SubnetKey(shid))
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

// Fund injects new funds from an account of the parent chain to a subnet.
//
// This functions receives a transaction with the FILs that want to be injected in the subnet.
// - Funds injected are frozen.
// - A new fund cross-message is created and stored to propagate it to the subnet. It will be
// picked up by miners to include it in the next possible block.
// - The cross-message nonce is updated.
func (a SubnetCoordActor) Fund(rt runtime.Runtime, params *SubnetIDParam) *abi.EmptyValue {
	// Only account actors can inject funds to a subnet (for now).
	rt.ValidateImmediateCallerType(builtin.AccountActorCodeID)

	// Check if the transaction includes funds
	value := rt.ValueReceived()
	if value.LessThanEqual(big.NewInt(0)) {
		rt.Abortf(exitcode.ErrIllegalArgument, "no funds included in transaction")
	}

	// Get SECP/BLS publickey to know the specific actor ID in the target subnet to
	// whom the funds need to be sent.
	// Funds are sent to the ID that controls the actor account in the destination subnet.
	secpAddr := SecpBLSAddr(rt, rt.Caller())

	// Increment stake locked for subnet.
	var st SCAState
	rt.StateTransaction(&st, func() {
		msg := fundMsg(rt, address.SubnetID(params.ID), secpAddr, value)
		commitTopDownMsg(rt, &st, msg)

	})
	return nil
}

func commitTopDownMsg(rt runtime.Runtime, st *SCAState, msg types.Message) {
	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error getting subnet from address")

	// Get the next subnet to which the message needs to be sent.
	sh, has, err := st.GetSubnet(adt.AsStore(rt), sto.Down(st.NetworkName))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
	if !has {
		rt.Abortf(exitcode.ErrIllegalArgument, "subnet for actor hasn't been registered yet")
	}

	// Set nonce for message
	msg.Nonce = sh.Nonce

	// Freeze funds
	rfrom, err := msg.From.RawAddr()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting raw address")
	sh.freezeFunds(rt, rfrom, msg.Value)

	// Store in the list of cross messages.
	sh.storeTopDownMsg(rt, &msg)

	// Increase nonce.
	incrementNonce(rt, &sh.Nonce)

	// Flush subnet.
	st.flushSubnet(rt, sh)
}

// Release creates a new check message to release funds in parent chain
//
// This function burns the funds that will be released in the current subnet
// and propagates a new checkpoint message to the parent chain to signal
// the amount of funds that can be released for a specific address.
func (a SubnetCoordActor) Release(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Only account actors can release funds from a subnet (for now).
	rt.ValidateImmediateCallerType(builtin.AccountActorCodeID)

	// Check if the transaction includes funds
	value := rt.ValueReceived()
	if value.LessThanEqual(big.NewInt(0)) {
		rt.Abortf(exitcode.ErrIllegalArgument, "no funds included in transaction")
	}

	code := rt.Send(builtin.BurntFundsActorAddr, builtin.MethodSend, nil, rt.ValueReceived(), &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState,
			"failed to send release funds to the burnt funds actor, code: %v", code)
	}

	// Get SECP/BLS publickey to know the specific actor ID in the target subnet to
	// whom the funds need to be sent.
	// Funds are sent to the ID that controls the actor account in the destination subnet.
	secpAddr := SecpBLSAddr(rt, rt.Caller())

	var st SCAState
	rt.StateTransaction(&st, func() {
		// Create releaseMsg and include in currentwindow checkpoint
		msg := st.releaseMsg(rt, value, secpAddr, st.Nonce)
		commitBottomUpMsg(rt, &st, msg)
	})
	return nil
}

func commitBottomUpMsg(rt runtime.Runtime, st *SCAState, msg types.Message) {
	// Store msg in registry, update msgMeta and include in checkpoint
	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting subnet from address")
	st.storeCheckMsg(rt, msg, st.NetworkName, sto)

	// Increase nonce.
	incrementNonce(rt, &st.Nonce)
}

func SecpBLSAddr(rt runtime.Runtime, raw address.Address) address.Address {
	resolved, ok := rt.ResolveAddress(raw)
	if !ok {
		rt.Abortf(exitcode.ErrIllegalArgument, "unable to resolve address %v", raw)
	}
	var pubkey address.Address
	code := rt.Send(resolved, builtin.MethodsAccount.PubkeyAddress, nil, big.Zero(), &pubkey)
	builtin.RequireSuccess(rt, code, "failed to fetch account pubkey from %v", resolved)
	return pubkey
}

// SendCross sends an arbitrary cross-message to other subnet in the hierarchy.
//
// If the message includes any funds they need to be burnt (like in Release)
// before being propagated to the corresponding subnet.
// The circulating supply in each subnet needs to be updated as the message passes through them.
func (a SubnetCoordActor) SendCross(rt runtime.Runtime, params *CrossMsgParams) *abi.EmptyValue {
	// Only support account addresses to send cross-messages for now.
	rt.ValidateImmediateCallerType(builtin.AccountActorCodeID)
	msg := params.Msg
	var err error

	if params.Destination == address.UndefSubnetID {
		rt.Abortf(exitcode.ErrIllegalArgument, "no desination subnet specified in cross-net message")
	}
	// Get SECP/BLS publickey to know the specific actor ID in the target subnet to
	// whom the funds need to be sent.
	// Funds are sent to the ID that controls the actor account in the destination subnet.
	// FIXME: Additional processing may be required if we want to
	// support cross-messages sent by actors.
	secp := SecpBLSAddr(rt, rt.Caller())

	var (
		st SCAState
		tp hierarchical.MsgType
	)

	rt.StateTransaction(&st, func() {
		if params.Destination == address.UndefSubnetID {
			rt.Abortf(exitcode.ErrIllegalArgument, "destination subnet is current one. You are better of sending a good ol' msg")
		}
		// Transform to hierarchical-supported addresses
		// NOTE: There is no address translation in msg.To. We could add additional
		// checks to see the type of address and handle it accordingly.
		msg.To, err = address.NewHAddress(params.Destination, msg.To)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")
		msg.From, err = address.NewHAddress(st.NetworkName, secp)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")

		tp = hierarchical.GetMsgType(&msg)
		// Check the type of message.
		switch tp {
		case hierarchical.TopDown:
			commitTopDownMsg(rt, &st, msg)
		case hierarchical.BottomUp:
			// Burn the funds before doing anything else.
			commitBottomUpMsg(rt, &st, msg)
		default:
			rt.Abortf(exitcode.ErrIllegalArgument, "cross-message doesn't have the right type")
		}
	})

	// For bottom-up messages with value, we need to burn the funds before propagating.
	if tp == hierarchical.BottomUp && msg.Value.GreaterThan(big.Zero()) {
		code := rt.Send(builtin.BurntFundsActorAddr, builtin.MethodSend, nil, rt.ValueReceived(), &builtin.Discard{})
		if !code.IsSuccess() {
			rt.Abortf(exitcode.ErrIllegalState,
				"failed to send release funds to the burnt funds actor, code: %v", code)
		}
	}
	return nil
}

// CrossMsgParams determines the cross message to apply.
type CrossMsgParams struct {
	Msg         types.Message
	Destination address.SubnetID
}

// ApplyMessage triggers the execution of a cross-subnet message validated through the consensus.
//
// This function can only be triggered using `ApplyImplicitMessage`, and the source needs to
// be the SystemActor. Cross messages are applied similarly to how rewards are applied once
// a block has been validated. This function:
// - Determines the type of cross-message.
// - Performs the corresponding state changes.
// - And updated the latest nonce applied for future checks.
func (a SubnetCoordActor) ApplyMessage(rt runtime.Runtime, params *CrossMsgParams) *abi.EmptyValue {
	// Only system actor can trigger this function.
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)

	var st SCAState
	rt.StateReadonly(&st)
	buApply, err := hierarchical.ApplyAsBottomUp(st.NetworkName, &params.Msg)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error processing type to apply")

	if buApply {
		applyBottomUp(rt, params.Msg)
		return nil
	}

	applyTopDown(rt, params.Msg)
	return nil
}
