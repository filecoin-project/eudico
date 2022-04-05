package mpower

import (

	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"

)

// Mpower actor is only used to determine if a new miner joined or not when running the checkpointing module
// in delegated mode (easier for development)
type State struct {
	MinerCount int64
	Miners     []string
	PublicKey  []byte //taproot address
}

func ConstructState(store adt.Store) (*State, error) {
	return &State{
		MinerCount: 0,
		// should have participants with pre generated key
		Miners:    make([]string, 0),
		PublicKey: make([]byte, 0),
	}, nil
}
