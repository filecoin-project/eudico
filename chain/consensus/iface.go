package consensus

import (
	"context"

	"github.com/filecoin-project/lotus/chain/types"
)

type Consensus interface {
	ValidateBlock(ctx context.Context, b *types.FullBlock) (err error)
}
