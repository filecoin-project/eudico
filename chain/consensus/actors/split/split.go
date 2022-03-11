package split

import (
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	builtin6 "github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	cid "github.com/ipfs/go-cid"
)

// example "Split" actor, sending it's balance split to beneficiaries

var _ runtime.VMActor = SplitActor{}

type SplitState struct {
	Beneficiaries []addr.Address
}

func ConstructState(b []addr.Address) *SplitState {
	return &SplitState{Beneficiaries: b}
}

type SplitActor struct{}

func (a SplitActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Split,
	}
}

func (a SplitActor) Code() cid.Cid {
	return actor.SplitActorCodeID
}

func (a SplitActor) IsSingleton() bool {
	return false
}

func (a SplitActor) State() cbor.Er {
	return new(SplitState)
}

func (a SplitActor) Constructor(rt runtime.Runtime, params *SplitState) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	rt.StateCreate(ConstructState(params.Beneficiaries))
	return nil
}

func (a SplitActor) Split(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()

	var st SplitState
	rt.StateReadonly(&st)

	bal := rt.CurrentBalance()
	toSend := big.Div(bal, big.NewInt(int64(len(st.Beneficiaries))))

	for _, beneficiary := range st.Beneficiaries {
		code := rt.Send(beneficiary, 0, nil, toSend, &builtin6.Discard{})
		builtin6.RequireSuccess(rt, code, "send failed")
	}

	return nil
}
