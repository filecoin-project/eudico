package subnet

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
)

type Manager interface {
	GetSubnetAPI(id address.SubnetID) (v1api.FullNode, error)
	GetSCAState(ctx context.Context, id address.SubnetID) (*sca.SCAState, blockadt.Store, error)
}
