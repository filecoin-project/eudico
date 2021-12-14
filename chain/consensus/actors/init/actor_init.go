package init

import (
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	init0 "github.com/filecoin-project/specs-actors/actors/builtin/init"
	init6 "github.com/filecoin-project/specs-actors/v6/actors/builtin/init"
	cid "github.com/ipfs/go-cid"

	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
)

// copied init6 actor but allows the SplitActor to be constructed

// The init actor uniquely has the power to create new actors.
// It maintains a table resolving pubkey and temporary actor addresses to the canonical ID-addresses.
type InitActor struct{}

func (a InitActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Exec,
	}
}

func (a InitActor) Code() cid.Cid {
	return builtin.InitActorCodeID
}

func (a InitActor) IsSingleton() bool {
	return true
}

func (a InitActor) State() cbor.Er { return new(init6.State) }

var _ runtime.VMActor = InitActor{}

//type ConstructorParams struct {
//	NetworkName string
//}
type ConstructorParams = init0.ConstructorParams

func (a InitActor) Constructor(rt runtime.Runtime, params *ConstructorParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)
	st, err := init6.ConstructState(adt.AsStore(rt), params.NetworkName)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

//type ExecParams struct {
//	CodeCID           cid.Cid `checked:"true"` // invalid CIDs won't get committed to the state tree
//	ConstructorParams []byte
//}
type ExecParams = init0.ExecParams

//type ExecReturn struct {
//	IDAddress     addr.Address // The canonical ID-based address for the actor.
//	RobustAddress addr.Address // A more expensive but re-org-safe address for the newly created actor.
//}
type ExecReturn = init0.ExecReturn

func (a InitActor) Exec(rt runtime.Runtime, params *ExecParams) *ExecReturn {
	rt.ValidateImmediateCallerAcceptAny()
	callerCodeCID, ok := rt.GetActorCodeCID(rt.Caller())
	builtin.RequireState(rt, ok, "no code for caller at %s", rt.Caller())
	if !canExec(callerCodeCID, params.CodeCID) {
		rt.Abortf(exitcode.ErrForbidden, "caller type %v cannot exec actor type %v", callerCodeCID, params.CodeCID)
	}

	// Compute a re-org-stable address.
	// This address exists for use by messages coming from outside the system, in order to
	// stably address the newly created actor even if a chain re-org causes it to end up with
	// a different ID.
	uniqueAddress := rt.NewActorAddress()

	// Allocate an ID for this actor.
	// Store mapping of pubkey or actor address to actor ID
	var st init6.State
	var idAddr addr.Address
	rt.StateTransaction(&st, func() {
		var err error
		idAddr, err = st.MapAddressToNewID(adt.AsStore(rt), uniqueAddress)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to allocate ID address")
	})

	// Create an empty actor.
	rt.CreateActor(params.CodeCID, idAddr)

	// Invoke constructor.
	code := rt.Send(idAddr, builtin.MethodConstructor, builtin.CBORBytes(params.ConstructorParams), rt.ValueReceived(), &builtin.Discard{})
	builtin.RequireSuccess(rt, code, "constructor failed")

	return &ExecReturn{IDAddress: idAddr, RobustAddress: uniqueAddress}
}

func canExec(callerCodeID cid.Cid, execCodeID cid.Cid) bool {
	switch execCodeID {
	case builtin.StorageMinerActorCodeID:
		if callerCodeID == builtin.StoragePowerActorCodeID {
			return true
		}
		return false
	// List of actors user-deployable actors.
	case builtin.PaymentChannelActorCodeID,
		builtin.MultisigActorCodeID,
		actor.SplitActorCodeID,
		actor.SubnetActorCodeID,
		actor.MpowerActorCodeID:
		return true
	default:
		return false
	}
}
