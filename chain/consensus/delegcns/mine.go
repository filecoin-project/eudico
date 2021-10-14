package delegcns

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/lotus/api"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
)

// NOTE: This is super ugly, but I'll use it
// as a workaround to keep the mining implementation for each consensus
// for the different types of API interfaces in the same place.
// This can't stay like this for long (for everyone's sake), but
// will defer it to the future when I have time to give it a bit
// more of thought and we find a more elegant way of tackling this.
func Mine(ctx context.Context, api *impl.FullNodeAPI, v0api v0api.FullNode) error {
	if v0api != nil {
		api := v0api
		head, err := api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting head: %w", err)
		}

		minerid, err := address.NewFromString("t0100")
		if err != nil {
			return err
		}
		miner, err := api.StateAccountKey(ctx, minerid, types.EmptyTSK)
		if err != nil {
			return err
		}

		log.Info("starting delegated mining on @", head.Height())

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
				})
				if err != nil {
					log.Errorw("creating block failed", "error", err)
					continue
				}

				err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
					Header:        bh.Header,
					BlsMessages:   bh.BlsMessages,
					SecpkMessages: bh.SecpkMessages,
				})
				if err != nil {
					log.Errorw("submitting block failed", "error", err)
				}

				log.Info("delegated mined a block! ", bh.Cid(), " msgs ", len(msgs))
			case <-ctx.Done():
				return nil
			}
		}
	} else {
		head, err := api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting head: %w", err)
		}

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
				})
				if err != nil {
					log.Errorw("creating block failed", "error", err)
					continue
				}

				err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
					Header:        bh.Header,
					BlsMessages:   bh.BlsMessages,
					SecpkMessages: bh.SecpkMessages,
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
}

func (deleg *Delegated) CreateBlock(ctx context.Context, w api.Wallet, bt *api.BlockTemplate) (*types.FullBlock, error) {
	pts, err := deleg.sm.ChainStore().LoadTipSet(bt.Parents)
	if err != nil {
		return nil, xerrors.Errorf("failed to load parent tipset: %w", err)
	}

	st, recpts, err := deleg.sm.TipSetState(ctx, pts)
	if err != nil {
		return nil, xerrors.Errorf("failed to load tipset state: %w", err)
	}

	next := &types.BlockHeader{
		Miner:         bt.Miner,
		Parents:       bt.Parents.Cids(),
		Ticket:        bt.Ticket,
		ElectionProof: bt.Eproof,

		BeaconEntries:         bt.BeaconValues,
		Height:                bt.Epoch,
		Timestamp:             bt.Timestamp,
		WinPoStProof:          bt.WinningPoStProof,
		ParentStateRoot:       st,
		ParentMessageReceipts: recpts,
	}

	var blsMessages []*types.Message
	var secpkMessages []*types.SignedMessage

	var blsMsgCids, secpkMsgCids []cid.Cid
	var blsSigs []crypto.Signature
	for _, msg := range bt.Messages {
		if msg.Signature.Type == crypto.SigTypeBLS {
			blsSigs = append(blsSigs, msg.Signature)
			blsMessages = append(blsMessages, &msg.Message)

			c, err := deleg.sm.ChainStore().PutMessage(&msg.Message)
			if err != nil {
				return nil, err
			}

			blsMsgCids = append(blsMsgCids, c)
		} else {
			c, err := deleg.sm.ChainStore().PutMessage(msg)
			if err != nil {
				return nil, err
			}

			secpkMsgCids = append(secpkMsgCids, c)
			secpkMessages = append(secpkMessages, msg)

		}
	}

	store := deleg.sm.ChainStore().ActorStore(ctx)
	blsmsgroot, err := consensus.ToMessagesArray(store, blsMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building bls amt: %w", err)
	}
	secpkmsgroot, err := consensus.ToMessagesArray(store, secpkMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building secpk amt: %w", err)
	}

	mmcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   blsmsgroot,
		SecpkMessages: secpkmsgroot,
	})
	if err != nil {
		return nil, err
	}
	next.Messages = mmcid

	aggSig, err := consensus.AggregateSignatures(blsSigs)
	if err != nil {
		return nil, err
	}

	next.BLSAggregate = aggSig
	pweight, err := deleg.sm.ChainStore().Weight(ctx, pts)
	if err != nil {
		return nil, err
	}
	next.ParentWeight = pweight

	baseFee, err := deleg.sm.ChainStore().ComputeBaseFee(ctx, pts)
	if err != nil {
		return nil, xerrors.Errorf("computing base fee: %w", err)
	}
	next.ParentBaseFee = baseFee

	nosigbytes, err := next.SigningBytes()
	if err != nil {
		return nil, xerrors.Errorf("failed to get signing bytes for block: %w", err)
	}

	sig, err := w.WalletSign(ctx, bt.Miner, nosigbytes, api.MsgMeta{
		Type: api.MTBlock,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to sign new block: %w", err)
	}

	next.BlockSig = sig

	fullBlock := &types.FullBlock{
		Header:        next,
		BlsMessages:   blsMessages,
		SecpkMessages: secpkMessages,
	}

	return fullBlock, nil
}
