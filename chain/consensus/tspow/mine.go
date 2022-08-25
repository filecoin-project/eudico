package tspow

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	param "github.com/filecoin-project/lotus/chain/consensus/common/params"
	"github.com/filecoin-project/lotus/chain/consensus/platform/logging"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log := logging.FromContext(ctx, log)

	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("getting head: %w", err)
	}

	log.Info("starting PoW mining on @", head.Height())

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("creating block failed", "error", err)
			continue
		}
		base, _ = types.NewTipSet([]*types.BlockHeader{BestWorkBlock(base)})

		expDiff := param.GenesisWorkTarget
		if base.Height()+1 >= MaxDiffLookback {
			lbr := base.Height() + 1 - DiffLookback(base.Height())
			lbts, err := api.ChainGetTipSetByHeight(ctx, lbr, base.Key())
			if err != nil {
				return fmt.Errorf("failed to get lookback tipset+1: %w", err)
			}

			expDiff = Difficulty(base, lbts)
		}

		diffb, err := expDiff.Bytes()
		if err != nil {
			return err
		}

		msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
		if err != nil {
			log.Errorw("selecting messages failed", "error", err)
		}

		// Get cross-message pool from subnet.

		nn, err := api.StateNetworkName(ctx)
		if err != nil {
			return err
		}
		sn, err := address.SubnetIDFromString(string(nn))
		if err != nil {
			return err
		}

		crossmsgs, err := api.GetCrossMsgsPool(ctx, sn, base.Height()+1)
		if err != nil {
			log.Errorw("selecting cross-messages failed", "error", err)
		}

		log.Debugf("CrossMsgs being proposed in block @%s: %d", base.Height()+1, len(crossmsgs))

		bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
			Miner:            miner,
			Parents:          types.NewTipSetKey(BestWorkBlock(base).Cid()),
			BeaconValues:     nil,
			Ticket:           &types.Ticket{VRFProof: diffb},
			Messages:         msgs,
			Epoch:            base.Height() + 1,
			Timestamp:        uint64(time.Now().Unix()),
			WinningPoStProof: nil,
			CrossMessages:    crossmsgs,
		})
		if err != nil {
			log.Errorw("creating block failed", "error", err)
			continue
		}
		if bh == nil {
			continue
		}

		log.Info("try PoW mining at @", base.Height(), base.String())

		err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
			Header:        bh.Header,
			BlsMessages:   bh.BlsMessages,
			SecpkMessages: bh.SecpkMessages,
		})
		if err != nil {
			log.Errorw("submitting block failed", "error", err)
			continue
		}

		log.Info("PoW mined a block! ", bh.Cid())
	}
}
