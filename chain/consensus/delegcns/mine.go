package delegcns

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	"golang.org/x/xerrors"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, addr address.Address, api v1api.FullNode) error {
	head, err := api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("getting head: %w", err)
	}

	// FIXME: Miner in delegated consensus is always the one with
	// ID=t0100, if we want this to be configurable it may require
	// some tweaking in delegated genesis and mining.
	// Leaving it like this for now.
	minerid, err := address.NewFromString("t0100")
	if err != nil {
		return err
	}
	miner, err := api.StateAccountKey(ctx, minerid, types.EmptyTSK)
	if err != nil {
		return err
	}

	log.Info("starting mining on @", head.Height())

	timer := time.NewTicker(time.Duration(build.BlockDelaySecs) * time.Second)
	for {
		select {
		case <-timer.C:
			base, err := api.ChainHead(ctx)
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}

			log.Info("try delegated mining at @", base.Height())

			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.Errorw("selecting messages failed", "error", err)
			}

			// Get cross-message pool from subnet.
			nn, err := api.StateNetworkName(ctx)
			if err != nil {
				return err
			}
			wrappedCrossMsgs, err := api.GetCrossMsgsPool(ctx, address.SubnetID(nn), base.Height()+1)
			if err != nil {
				log.Errorw("selecting cross-messages failed", "error", err)
			}
			log.Debugf("CrossMsgs being proposed in block @%s: %d", base.Height()+1, len(wrappedCrossMsgs))

			var crossMsgs []*types.Message
			for _, m := range wrappedCrossMsgs {
				crossMsgs = append(crossMsgs, m.Msg)
			}

			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          base.Key(),
				Ticket:           nil,
				Eproof:           nil,
				BeaconValues:     nil,
				Messages:         msgs,
				Epoch:            base.Height() + 1,
				Timestamp:        base.MinTimestamp() + build.BlockDelaySecs,
				WinningPoStProof: nil,
				CrossMessages:    crossMsgs,
			})
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}

			err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
				CrossMessages: bh.CrossMessages,
			})
			if err != nil {
				log.Errorw("submitting block failed", "error", err)
			}

			log.Info("delegated mined a block! ", bh.Cid(), " msgs ", len(msgs))
		case <-ctx.Done():
			return nil
		}
	}
}

func (deleg *Delegated) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	b, err := common.PrepareBlockForSignature(ctx, deleg.sm, bt)
	if err != nil {
		return nil, err
	}

	err = common.SignBlock(ctx, w, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
