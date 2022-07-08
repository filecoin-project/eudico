package hierarchical

import (
	"fmt"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
)

// ConsensusType for subnet.
type ConsensusType uint64

// List of supported/implemented consensus algorithms for subnets.
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

// Consensus returns the consensus algorithm.
func Consensus(name string) ConsensusType {
	switch {
	case strings.EqualFold(name, "delegated"):
		return Delegated
	case strings.EqualFold(name, "pow"):
		return PoW
	case strings.EqualFold(name, "tendermint"):
		return Tendermint
	case strings.EqualFold(name, "filecoinec"):
		return FilecoinEC
	case strings.EqualFold(name, "mir"):
		return Mir
	case strings.EqualFold(name, "dummy"):
		return Dummy
	default:
		panic(fmt.Sprintf("unknown or unspecified consensus algorithm %s", name))
	}
}

// MinCheckpointPeriod returns a minimal allowed checkpoint period for the consensus in a subnet.
//
// Finality determines the number of epochs to wait before considering a change "final".
func MinCheckpointPeriod(alg ConsensusType) abi.ChainEpoch {
	switch alg {
	case Delegated:
		return build.DelegatedPoWCheckpointPeriod
	case PoW:
		return build.PoWCheckpointPeriod
	case Tendermint:
		return build.TendermintCheckpointPeriod
	case FilecoinEC:
		return build.FilecoinCheckpointPeriod
	case Mir:
		return build.MirCheckpointPeriod
	case Dummy:
		return build.DummyCheckpointPeriod
	default:
		panic(fmt.Sprintf("unknown consensus algorithm %v", alg))
	}
}

// MinFinality returns a minimal allowed finality threshold for the consensus in a subnet.
func MinFinality(alg ConsensusType) abi.ChainEpoch {
	switch alg {
	case Delegated:
		return build.DelegatedPoWFinality
	case PoW:
		return build.PoWFinality
	case Tendermint:
		return build.TendermintFinality
	case FilecoinEC:
		return build.FilecoinFinality
	case Mir:
		return build.MirFinality
	case Dummy:
		return build.DummyFinality
	default:
		panic(fmt.Sprintf("unknown consensus algorithm %v", alg))
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

func IsCrossMsg(msg *types.Message) bool {
	_, errf := msg.From.Subnet()
	_, errt := msg.To.Subnet()
	return errf == nil && errt == nil
}

func UnbundleCrossAndSecpMsgs(msgs []types.ChainMsg) (secp []types.ChainMsg, cross []types.ChainMsg) {
	for _, cm := range msgs {
		if IsCrossMsg(cm.VMMessage()) {
			cross = append(cross, cm)
		} else {
			secp = append(secp, cm)
		}
	}
	return
}

// SubnetCoordActorAddr is the address of the SCA actor in a subnet. It is initialized in genesis with the address t064.
var SubnetCoordActorAddr = func() address.Address {
	a, err := address.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()

type SubnetParams struct {
	Name              string           // Subnet name.
	Addr              address.Address  // Subnet address.
	Parent            address.SubnetID // Parent subnet ID.
	Stake             abi.TokenAmount  // Initial stake.
	CheckpointPeriod  abi.ChainEpoch   // Checkpointing period for a subnet.
	FinalityThreshold abi.ChainEpoch   // Finality threshold for a subnet.
	Consensus         ConsensusParams  // Consensus params.
}

type ConsensusParams struct {
	Alg           ConsensusType   // Consensus algorithm.
	DelegMiner    address.Address // Miner in delegated consensus.
	MinValidators uint64          // Min number of validators required to start a network.
}

type MiningParams struct {
	LogLevel    string
	LogFileName string
}

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
