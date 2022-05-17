package tspow

import (
	"context"
	"crypto/rand"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/consensus/platform/logging"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	param "github.com/filecoin-project/lotus/chain/consensus/common/params"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log := logging.FromContext(ctx, log)

	head, err := api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("getting head: %w", err)
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
				return xerrors.Errorf("failed to get lookback tipset+1: %w", err)
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

		crossmsgs, err := api.GetCrossMsgsPool(ctx, address.SubnetID(nn), base.Height()+1)
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
			CrossMessages: bh.CrossMessages,
		})
		if err != nil {
			log.Errorw("submitting block failed", "error", err)
			continue
		}

		log.Info("PoW mined a block! ", bh.Cid())
	}
}

func (tsp *TSPoW) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	b, err := common.PrepareBlockForSignature(ctx, tsp.sm, bt)
	if err != nil {
		return nil, err
	}
	next := b.Header

	tgt := big.Zero()
	tgt.SetBytes(next.Ticket.VRFProof)

	bestH := *next
	for i := 0; i < 10000; i++ {
		next.ElectionProof = &types.ElectionProof{
			VRFProof: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		}
		rand.Read(next.ElectionProof.VRFProof) //nolint:errcheck
		if work(&bestH).LessThan(work(next)) {
			bestH = *next
			if work(next).GreaterThanEqual(tgt) {
				break
			}
		}
	}
	next = &bestH

	if work(next).LessThan(tgt) {
		return nil, nil
	}

	err = common.SignBlock(ctx, w, b)
	if err != nil {
		return nil, err
	}

	return b, nil
}
