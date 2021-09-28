package sharding

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"golang.org/x/xerrors"
)

func (sh *Shard) MinerCreateBlock(ctx context.Context, bt *api.BlockTemplate) (*types.BlockMsg, error) {
	fblk, err := sh.cons.CreateBlock(ctx, sh.stateAPI.Wallet, bt)
	if err != nil {
		return nil, err
	}
	if fblk == nil {
		return nil, nil
	}

	var out types.BlockMsg
	out.Header = fblk.Header
	for _, msg := range fblk.BlsMessages {
		out.BlsMessages = append(out.BlsMessages, msg.Cid())
	}
	for _, msg := range fblk.SecpkMessages {
		out.SecpkMessages = append(out.SecpkMessages, msg.Cid())
	}

	return &out, nil
}

func (sh *Shard) StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error) {
	ts, err := sh.ch.GetTipSetFromKey(tsk)
	if err != nil {
		return address.Undef, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}

	return sh.sm.ResolveToKeyAddress(ctx, addr, ts)
}

func (sh *Shard) MpoolSelect(ctx context.Context, tsk types.TipSetKey, ticketQuality float64) ([]*types.SignedMessage, error) {
	ts, err := sh.ch.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}

	return sh.mpool.SelectMessages(ctx, ts, ticketQuality)
}

// TODO: Once we populate APIs this may not be needed anymore.
// This is a reimplementation. Wait for next iteration.
func (sh *Shard) SyncSubmitBlock(ctx context.Context, blk *types.BlockMsg) error {
	/* TODO: Removing the use of slashFilter for now.
	parent, err := sh.syncer.ChainStore().GetBlock(blk.Header.Parents[0])
	if err != nil {
		return xerrors.Errorf("loading parent block: %w", err)
	}

	if a.SlashFilter != nil {
		if err := a.SlashFilter.MinedBlock(blk.Header, parent.Height); err != nil {
			log.Errorf("<!!> SLASH FILTER ERROR: %s", err)
			return xerrors.Errorf("<!!> SLASH FILTER ERROR: %w", err)
		}
	}
	*/

	// TODO: should we have some sort of fast path to adding a local block?
	bmsgs, err := sh.syncer.ChainStore().LoadMessagesFromCids(blk.BlsMessages)
	if err != nil {
		log.Errorw("Error loading messages", "err", err)
		return xerrors.Errorf("failed to load bls messages: %w", err)
	}

	smsgs, err := sh.syncer.ChainStore().LoadSignedMessagesFromCids(blk.SecpkMessages)
	if err != nil {
		log.Errorw("Error loading signed messages", "err", err)
		return xerrors.Errorf("failed to load secpk message: %w", err)
	}

	fb := &types.FullBlock{
		Header:        blk.Header,
		BlsMessages:   bmsgs,
		SecpkMessages: smsgs,
	}

	if err := sh.syncer.ValidateMsgMeta(fb); err != nil {
		log.Errorw("Error validating message meta", "err", err)
		return xerrors.Errorf("provided messages did not match block: %w", err)
	}

	ts, err := types.NewTipSet([]*types.BlockHeader{blk.Header})
	if err != nil {
		return xerrors.Errorf("somehow failed to make a tipset out of a single block: %w", err)
	}
	if err := sh.syncer.Sync(ctx, ts); err != nil {
		return xerrors.Errorf("sync to submitted block failed: %w", err)
	}

	b, err := blk.Serialize()
	if err != nil {
		return xerrors.Errorf("serializing block for pubsub publishing failed: %w", err)
	}

	return sh.pubsub.Publish(build.BlocksTopic(dtypes.NetworkName(sh.ID)), b) //nolint:staticcheck
}

func (sh *Shard) mineDelegated(ctx context.Context, api api.FullNode) error {
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

	miner, err := sh.StateAccountKey(ctx, minerid, types.EmptyTSK)
	if err != nil {
		log.Errorw("Error with StateAccountKey", "err", err)
		return err
	}

	log.Infow("starting mining on @", "shardID", sh.ID, "height", head.Height())

	timer := time.NewTicker(time.Duration(build.BlockDelaySecs) * time.Second)
	for {
		select {
		case <-timer.C:
			base := sh.ch.GetHeaviestTipSet()
			if head == nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}

			log.Infow("try mining at @", "shardID", sh.ID, "height", head.Height())

			msgs, err := sh.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.Errorw("selecting messages failed", "error", err)
			}
			bh, err := sh.MinerCreateBlock(context.TODO(), &lapi.BlockTemplate{
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

			err = sh.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("submitting block failed", "error", err)
			}

			log.Info("mined a block! ", bh.Cid(), " msgs ", len(msgs), "shardID", sh.ID)
		case <-ctx.Done():
			return nil
		}
	}
}
