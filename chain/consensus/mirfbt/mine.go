package mirbft

import (
	"context"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("starting MirBFT miner: ", miner.String())
	defer log.Info("shutdown MirBFT miner: ", miner.String())

	return nil
}

func (m *MirBFT) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block for epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block for epoch %d", bt.Epoch)

	return nil, nil
}
