package sharding

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
)

func (sh *Shard) mineDelegated(ctx context.Context) error {
	head := sh.ch.GetHeaviestTipSet()
	if head == nil {
		log.Errorw("Error getting heaviest tipet. Tipset nil")
		return xerrors.Errorf("couldn't get heaviest tipet")
	}

	minerid, err := address.NewFromString("t0100")
	if err != nil {
		log.Errorw("Error getting miner address", "err", err)
		return err
	}

	miner, err := sh.api.StateAccountKey(ctx, minerid, types.EmptyTSK)
	if err != nil {
		log.Errorw("Error with StateAccountKey", "err", err)
		return err
	}

	log.Infow("starting mining on @", "shardID", sh.ID, "height", head.Height())

	timer := time.NewTicker(time.Duration(build.BlockDelaySecs) * time.Second)
	for {
		select {
		case <-timer.C:
			base, err := sh.api.ChainHead(ctx)
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}

			log.Infow("try mining at @", "shardID", sh.ID, "height", base.Height())

			msgs, err := sh.api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.Errorw("selecting messages failed", "error", err)
			}
			bh, err := sh.api.MinerCreateBlock(context.TODO(), &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          base.Key(),
				Ticket:           nil,
				Eproof:           nil,
				BeaconValues:     nil,
				Messages:         msgs,
				Epoch:            base.Height() + 1,
				Timestamp:        base.MinTimestamp() + build.BlockDelaySecs,
				WinningPoStProof: nil,
			})
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}

			err = sh.api.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("submitting block failed", "error", err)
			}

			log.Infow("mined a block! ", "cid", bh.Cid(), " msgs ", len(msgs), "shardID", sh.ID)
		case <-ctx.Done():
			return nil
		}
	}
}
