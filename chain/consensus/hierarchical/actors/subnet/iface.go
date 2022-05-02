package subnet

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
)

// SubnetIface defines the minimum interface that needs to be implemented by subnet actors.
// TODO: @alfonso do we need this?
type SubnetIface interface {
	Join(rt runtime.Runtime, params *Validator) *abi.EmptyValue
	Leave(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue
	SubmitCheckpoint(rt runtime.Runtime, params *sca.CheckpointParams) *abi.EmptyValue
	Kill(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue
}
