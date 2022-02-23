package mpower

import (
	"fmt"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"

	//"github.com/Zondax/multi-party-sig/pkg/taproot"

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
		4: 						   a.UpdateTaprootAddress, // Update the taproot address
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
		st.Miners = append(st.Miners,params.Miners...)
    	st.Miners = unique(st.Miners)
    	st.MinerCount = int64(len(st.Miners))
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

		for _, minerToRemove := range params.Miners{
			for i, oldMiner := range st.Miners{
				if minerToRemove == oldMiner{
					st.Miners = append(st.Miners[:i], st.Miners[i+1:]...)
					break
				}
			}

		} 
		fmt.Println("New list of miners after removal: ",st.Miners)
		st.MinerCount = int64(len(st.Miners))
	})
	return nil
}


type NewTaprootAddressParam struct {
	PublicKey []byte
}

func (a Actor) UpdateTaprootAddress(rt Runtime, addr *NewTaprootAddressParam) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	var st State
	rt.StateTransaction(&st, func() {
		// Miners list is replaced with the one passed as parameters
		st.PublicKey = addr.PublicKey
		fmt.Println("address updated",st.PublicKey)
	})
	return nil
}

// func (a Actor) UpdateTaprootAddress(rt Runtime, addr *AddMinerParams) *abi.EmptyValue {
// 	rt.ValidateImmediateCallerAcceptAny()
// 	var st State
// 	rt.StateTransaction(&st, func() {
// 		// Miners list is replaced with the one passed as parameters
// 		fmt.Println("actor address before",st.PublicKey)
// 		st.PublicKey = addr.Miners
// 		fmt.Println("address updated",st.PublicKey)
// 	})
// 	return nil
// }

func unique(strSlice []string) []string {
    keys := make(map[string]bool)
    list := []string{}	
    for _, entry := range strSlice {
        if _, value := keys[entry]; !value {
            keys[entry] = true
            list = append(list, entry)
        }
    }    
    return list
}
