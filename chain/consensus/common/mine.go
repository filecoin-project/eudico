package common

import (
	"context"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
)

func PrepareBlockForSignature(ctx context.Context, sm *stmgr.StateManager, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	pts, err := sm.ChainStore().LoadTipSet(ctx, bt.Parents)
	if err != nil {
		return nil, xerrors.Errorf("failed to load parent tipset: %w", err)
	}

	st, recpts, err := sm.TipSetState(ctx, pts)
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

	var blsMsgCids, secpkMsgCids, crossMsgCids []cid.Cid
	var blsSigs []crypto.Signature
	for _, msg := range bt.Messages {
		if msg.Signature.Type == crypto.SigTypeBLS {
			blsSigs = append(blsSigs, msg.Signature)
			blsMessages = append(blsMessages, &msg.Message)

			c, err := sm.ChainStore().PutMessage(ctx, &msg.Message)
			if err != nil {
				return nil, err
			}

			blsMsgCids = append(blsMsgCids, c)
		} else {
			c, err := sm.ChainStore().PutMessage(ctx, msg)
			if err != nil {
				return nil, err
			}

			secpkMsgCids = append(secpkMsgCids, c)
			secpkMessages = append(secpkMessages, msg)
		}
	}

	for _, msg := range bt.CrossMessages {
		c, err := sm.ChainStore().PutMessage(ctx, msg)
		if err != nil {
			return nil, err
		}

		crossMsgCids = append(crossMsgCids, c)
	}

	store := sm.ChainStore().ActorStore(ctx)
	blsmsgroot, err := consensus.ToMessagesArray(store, blsMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building bls amt: %w", err)
	}
	secpkmsgroot, err := consensus.ToMessagesArray(store, secpkMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building secpk amt: %w", err)
	}
	crossmsgroot, err := consensus.ToMessagesArray(store, crossMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building cross amt: %w", err)
	}

	mmcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   blsmsgroot,
		SecpkMessages: secpkmsgroot,
		CrossMessages: crossmsgroot,
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
	pweight, err := sm.ChainStore().Weight(ctx, pts)
	if err != nil {
		return nil, err
	}
	next.ParentWeight = pweight

	baseFee, err := sm.ChainStore().ComputeBaseFee(ctx, pts)
	if err != nil {
		return nil, xerrors.Errorf("computing base fee: %w", err)
	}
	next.ParentBaseFee = baseFee
	return &types.FullBlock{
		Header:        next,
		BlsMessages:   blsMessages,
		SecpkMessages: secpkMessages,
		CrossMessages: bt.CrossMessages,
	}, nil

}

func SignBlock(ctx context.Context, w lapi.Wallet, b *types.FullBlock) error {
	next := b.Header
	nosigbytes, err := next.SigningBytes()
	if err != nil {
		return xerrors.Errorf("failed to get signing bytes for block: %w", err)
	}

	sig, err := w.WalletSign(ctx, next.Miner, nosigbytes, lapi.MsgMeta{
		Type: lapi.MTBlock,
	})
	if err != nil {
		return xerrors.Errorf("failed to sign new block %x: %w", next.Miner, err)
	}

	next.BlockSig = sig
	return nil
}
