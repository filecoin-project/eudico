package subnet

import (
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
)

// SubnetMgr is a convenient interface to get SubnetMgr API
// without dependency cycles.
type SubnetMgr interface {
	GetSubnetAPI(id hierarchical.SubnetID) (v1api.FullNode, error)
}
