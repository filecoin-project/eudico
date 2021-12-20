package hierarchical

import (
	address "github.com/filecoin-project/go-address"
)

// ConsensusType for subnet
type ConsensusType uint64

// List of supported/implemented consensus for subnets.
const (
	Delegated ConsensusType = iota
	PoW
)

// MsgType of cross message
type MsgType uint64

// List of cross messages supported
const (
	Fund MsgType = iota
	Release
	Cross
)

// SubnetCoordActorAddr is the address of the SCA actor
// in a subnet.
//
// It is initialized in genesis with the
// address t064
var SubnetCoordActorAddr = func() address.Address {
	a, err := address.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()
