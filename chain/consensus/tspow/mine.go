package tspow

import (
	"context"
	"math/rand"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/crypto"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/node/impl"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus"
	param "github.com/filecoin-project/lotus/chain/consensus/params"
	"github.com/filecoin-project/lotus/chain/types"
)

// NOTE: This is super ugly, but I'll use it
// as a workaround to keep the mining implementation for each consensus
// for the different types of API interfaces in the same place.
// This can't stay like this for long (for everyone's sake), but
// will defer it to the future when I have time to give it a bit
// more of thought and we find a more elegant way of tackling this.
func Mine(ctx context.Context, miner address.Address, api *impl.FullNodeAPI, v0api v0api.FullNode) error {
	if v0api != nil {
		api := v0api
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

			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          types.NewTipSetKey(BestWorkBlock(base).Cid()),
				BeaconValues:     nil,
				Ticket:           &types.Ticket{VRFProof: diffb},
				Messages:         msgs,
				Epoch:            base.Height() + 1,
				Timestamp:        uint64(time.Now().Unix()),
				WinningPoStProof: nil,
			})
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}
			if bh == nil {
				continue
			}

			log.Info("try mining at @", base.Height(), base.String())

			err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("submitting block failed", "error", err)
			}

			log.Info("PoW mined a block! ", bh.Cid())
		}
	} else {
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

			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          types.NewTipSetKey(BestWorkBlock(base).Cid()),
				BeaconValues:     nil,
				Ticket:           &types.Ticket{VRFProof: diffb},
				Messages:         msgs,
				Epoch:            base.Height() + 1,
				Timestamp:        uint64(time.Now().Unix()),
				WinningPoStProof: nil,
			})
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}
			if bh == nil {
				continue
			}

			log.Info("try PpW mining at @", base.Height(), base.String())

			err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("submitting block failed", "error", err)
			}

			log.Info("PoW mined a block! ", bh.Cid())
		}
	}
}

func (tsp *TSPoW) CreateBlock(ctx context.Context, w api.Wallet, bt *api.BlockTemplate) (*types.FullBlock, error) {
	pts, err := tsp.sm.ChainStore().LoadTipSet(bt.Parents)
	if err != nil {
		return nil, xerrors.Errorf("failed to load parent tipset: %w", err)
	}

	st, recpts, err := tsp.sm.TipSetState(ctx, pts)
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

			c, err := tsp.sm.ChainStore().PutMessage(&msg.Message)
			if err != nil {
				return nil, err
			}

			blsMsgCids = append(blsMsgCids, c)
		} else {
			c, err := tsp.sm.ChainStore().PutMessage(msg)
			if err != nil {
				return nil, err
			}

			secpkMsgCids = append(secpkMsgCids, c)
			secpkMessages = append(secpkMessages, msg)
		}
	}

	store := tsp.sm.ChainStore().ActorStore(ctx)
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
	pweight, err := tsp.sm.ChainStore().Weight(ctx, pts)
	if err != nil {
		return nil, err
	}
	next.ParentWeight = pweight

	baseFee, err := tsp.sm.ChainStore().ComputeBaseFee(ctx, pts)
	if err != nil {
		return nil, xerrors.Errorf("computing base fee: %w", err)
	}
	next.ParentBaseFee = baseFee

	tgt := big.Zero()
	tgt.SetBytes(next.Ticket.VRFProof)

	bestH := *next
	for i := 0; i < 10000; i++ {
		next.ElectionProof = &types.ElectionProof{
			VRFProof: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		}
		rand.Read(next.ElectionProof.VRFProof)
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
