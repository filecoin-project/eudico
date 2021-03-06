package subnet

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
)

// SubnetIface defines the minimum interface that needs to be implemented by subnet actors.
type SubnetIface interface {
	Join(rt runtime.Runtime, v *hierarchical.Validator) *abi.EmptyValue
	Leave(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue
	SubmitCheckpoint(rt runtime.Runtime, params *sca.CheckpointParams) *abi.EmptyValue
	Kill(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue
}
