package delegcns

import (
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	builtin5 "github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

//go:generate go run ./gen

var (
	SplitActorCodeID cid.Cid
)

var builtinActors map[cid.Cid]*actorInfo

type actorInfo struct {
	name string
}

func init() {
	builder := cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}
	builtinActors = make(map[cid.Cid]*actorInfo)

	for id, info := range map[*cid.Cid]*actorInfo{ //nolint:nomaprange
		&SplitActorCodeID: {name: "deleg/0/split"},
	} {
		c, err := builder.Sum([]byte(info.name))
		if err != nil {
			panic(err)
		}
		*id = c
		builtinActors[c] = info
	}
}

// example "Split" actor, sending it's balance split to beneficiaries

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
	return SplitActorCodeID
}

func (a SplitActor) IsSingleton() bool {
	return false
}

func (a SplitActor) State() cbor.Er {
	return new(SplitState)
}

var _ runtime.VMActor = SplitActor{}

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
		code := rt.Send(beneficiary, 0, nil, toSend, &builtin5.Discard{})
		builtin5.RequireSuccess(rt, code, "send failed")
	}

	return nil
}
