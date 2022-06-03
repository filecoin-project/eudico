package hierarchical

import (
	"github.com/filecoin-project/go-state-types/abi"
)

type ConstructParams struct {
	Parent            SubnetID
	Name              string
	Consensus         ConsensusType
	MinValidatorStake abi.TokenAmount
	CheckPeriod       abi.ChainEpoch
	Genesis           []byte
}
