package hierarchical

import (
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// ConsensusType for subnet.
type ConsensusType uint64

type ConsensusParams struct {
	DelegMiner    address.Address // Miner in delegated consensus.
	MinValidators uint64          // Min number of validators required to start a network.
}

type MiningParams struct {
	LogLevel    string
	LogFileName string
}

// List of supported/implemented consensus for subnets.
const (
	Delegated ConsensusType = iota
	PoW
	Tendermint
	Mir
	FilecoinEC
	Dummy
)

// ConsensusName returns the consensus algorithm name.
func ConsensusName(alg ConsensusType) string {
	switch alg {
	case Delegated:
		return "Delegated"
	case PoW:
		return "PoW"
	case Tendermint:
		return "Tendermint"
	case FilecoinEC:
		return "FilecoinEC"
	case Mir:
		return "Mir"
	case Dummy:
		return "Dummy"
	default:
		return "unknown"
	}
}

// MsgType of cross message.
type MsgType uint64

// List of cross messages supported.
const (
	Unknown MsgType = iota
	BottomUp
	TopDown
)

// GetMsgType returns the MsgType of the message.
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

// SubnetCoordActorAddr is the address of the SCA actor in a subnet. It is initialized in genesis with the address t064.
var SubnetCoordActorAddr = func() address.Address {
	a, err := address.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()

// SubnetKey implements Keyer interface, so it can be used as a key for maps.
type SubnetKey address.SubnetID

var _ abi.Keyer = SubnetKey("")

func (id SubnetKey) Key() string {
	return string(id)
}

func IsBottomUp(from, to address.SubnetID) bool {
	_, l := from.CommonParent(to)
	sfrom := strings.Split(from.String(), "/")
	return len(sfrom)-1 > l
}

// ApplyAsBottomUp is used to determine if a cross-message in the current subnet needs to be applied as a top-down or
// bottom-up message according to the path its following (i.e. we process a message or a msgMeta).
func ApplyAsBottomUp(curr address.SubnetID, msg *types.Message) (bool, error) {
	sto, err := msg.To.Subnet()
	if err != nil {
		return false, xerrors.Errorf("error getting subnet from hierarchical address in cross-msg")
	}
	sfrom, err := msg.From.Subnet()
	if err != nil {
		return false, xerrors.Errorf("error getting subnet from hierarchical address in cross-msg")
	}

	mt := GetMsgType(msg)
	cpcurr, _ := curr.CommonParent(sto)
	cpfrom, _ := sfrom.CommonParent(sto)
	return mt == BottomUp && cpcurr == cpfrom, nil
}
