package mpower

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
)

type Runtime = runtime.Runtime

type Actor struct{}

// Mocked Power Actor address is t065 (arbitrarly choosen)
var PowerActorAddr = func() address.Address {
	a, err := address.NewIDAddress(65)
	if err != nil {
		panic(err)
	}
	return a
}()

func (a Actor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor, // Initialiazed the actor; always required
		2:                         a.AddMiners,    // Add a miner to the list (specificaly crafted for checkpointing)
		3:						   a.RemoveMiners, // Remove miners from the list
	}
}

func (a Actor) Code() cid.Cid {
	return actor.MpowerActorCodeID
}

func (a Actor) IsSingleton() bool {
	return true
}

func (a Actor) State() cbor.Er {
	return new(State)
}

var _ runtime.VMActor = Actor{}

////////////////////////////////////////////////////////////////////////////////
// Actor methods
////////////////////////////////////////////////////////////////////////////////

// see https://github.com/filecoin-project/specs-actors/blob/master/actors/builtin/power/power_actor.go#L83
func (a Actor) Constructor(rt Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)

	st, err := ConstructState(adt.AsStore(rt))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

// Add miners parameters structure (not in original power actor)
type AddMinerParams struct {
	Miners []string
}

// Adds claimed power for the calling actor.
// May only be invoked by a miner actor.
func (a Actor) AddMiners(rt Runtime, params *AddMinerParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	var st State
	rt.StateTransaction(&st, func() {
		// Miners list is replaced with the one passed as parameters
		st.MinerCount += int64(len(params.Miners))
		st.Miners = append(st.Miners,params.Miners...)
	})
	return nil
}

// Removes claimed power for the calling actor.
// May only be invoked by a miner actor.
func (a Actor) RemoveMiners(rt Runtime, params *AddMinerParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	var st State
	rt.StateTransaction(&st, func() {
		// Miners list is replaced with the one passed as parameters
		st.MinerCount -= int64(len(params.Miners))
		// TODO: change this function to remove the list instead
		st.Miners = append(st.Miners,params.Miners...)

		//we remove the duplicates from the list (if any)
		keys := make(map[string]bool)
    	list := []string{}	
    	for _, entry := range st.Miners {
        	if _, value := keys[entry]; !value {
            	keys[entry] = true
            	list = append(list, entry)
        }
        st.Miners = list
    }    
	})
	return nil
}

