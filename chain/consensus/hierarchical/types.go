package hierarchical

import (
	"path"
	"strings"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
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
	Unknown MsgType = iota
	BottomUp
	TopDown
)

// MsgType returns the
func GetMsgType(msg *types.Message) MsgType {
	t := Unknown

	sto, err := msg.To.Subnet()
	if err != nil {
		return t
	}
	sfrom, err := msg.From.Subnet()
	if err != nil {
		return t
	}
	if IsBottomUp(sfrom, sto) {
		return BottomUp
	}
	return TopDown
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

func CommonParent(from, to address.SubnetID) (address.SubnetID, int) {
	s1 := strings.Split(from.String(), "/")
	s2 := strings.Split(to.String(), "/")
	if len(s1) < len(s2) {
		a := s1
		s1 = s2
		s2 = a
	}
	out := "/"
	l := 0
	for i, s := range s2 {
		if s == s1[i] {
			out = path.Join(out, s)
			l = i
		} else {
			return address.SubnetID(out), l
		}
	}
	return address.SubnetID(out), l
}

func isParent(curr, from, to address.SubnetID) bool {
	parent, _ := CommonParent(from, to)
	return parent == curr
}

func IsBottomUp(from, to address.SubnetID) bool {
	_, l := CommonParent(from, to)
	sfrom := strings.Split(from.String(), "/")
	if len(sfrom)-1 <= l {
		return false
	}
	return true
}
