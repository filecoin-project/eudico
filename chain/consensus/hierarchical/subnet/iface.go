package subnet

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
)

// SubnetMgr is a convenient interface to get SubnetMgr API
// without dependency cycles.
type SubnetMgr interface {
	GetSubnetAPI(id address.SubnetID) (v1api.FullNode, error)
	GetSCAState(ctx context.Context, id address.SubnetID) (*sca.SCAState, blockadt.Store, error)
}
