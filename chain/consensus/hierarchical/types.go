package hierarchical

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
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
	Unknown
)

// MsgType returns the
func GetMsgType(msg *types.Message) MsgType {
	t := Unknown

	// Get raw addresses
	rfrom, err := msg.From.RawAddr()
	if err != nil {
		return t
	}
	rto, err := msg.To.RawAddr()
	if err != nil {
		return t
	}

	// FIXME: Add additional checks using subnet prefix from
	// the address. Cross-msgs should always include a HAAddress.

	// FUND: If same raw address in from and to
	if rfrom == rto {
		return Fund
	}

	// RELEASE: Coming from the burntAddress
	if rfrom == builtin.BurntFundsActorAddr {
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

// Implement keyer interface so it can be used as a
// key for maps
type SubnetKey address.SubnetID

var _ abi.Keyer = SubnetKey("")

func (id SubnetKey) Key() string {
	return string(id)
}
