package mpower

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	//"github.com/Zondax/multi-party-sig/pkg/taproot"
)

// Mpower actor is only used to determine if a new miner joined or not when running the checkpointing module
// in delegated mode (easier for development)
type State struct {
	MinerCount int64
	Miners     []address.Address
	//PublicKey  []byte //taproot address
}

// func ConstructState(store adt.Store) (*State, error) {
// 	return &State{
// 		MinerCount: 0,
// 		// should have participants with pre generated key
// 		Miners: make([]string, 0),
// 		PublicKey: make([]byte, 0),
// 	}, nil
// }
func ConstructState(store adt.Store) (*State, error) {
	return &State{
		MinerCount: 0,
		// should have participants with pre generated key
		Miners: make([]address.Address, 0),
		//PublicKey: make([]byte, 0),
	}, nil
}
