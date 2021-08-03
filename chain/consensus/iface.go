package consensus

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

type Consensus interface {
	ValidateBlock(ctx context.Context, b *types.FullBlock) (err error)
	IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool

	CreateBlock(ctx context.Context, w api.Wallet, bt *api.BlockTemplate) (*types.FullBlock, error)
}
