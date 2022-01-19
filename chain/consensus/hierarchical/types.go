package hierarchical

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
)

// ConsensusType for subnet
type ConsensusType uint64

// List of supported/implemented consensus for subnets.
const (
	Delegated ConsensusType = iota
	PoW
	Tendermint
)

// MsgType of cross message
type MsgType uint64

// List of cross messages supported
const (
	Fund MsgType = iota
	Release
	Cross
)

// MsgType returns the
func GetMsgType(msg *types.Message) MsgType {
	if msg.From == msg.To {
		return Fund
	}

	if msg.From == builtin.BurntFundsActorAddr {
		return Release
	}
	return Cross
}

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
