package tendermint

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ipfs/go-cid"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	tenderminttypes "github.com/tendermint/tendermint/types"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/crypto"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
)

// sanitizeMessagesAndPrepareBlockForSignature checks and removes invalid messages from the block fixture
// and return the bock with valid messages.
func sanitizeMessagesAndPrepareBlockForSignature(ctx context.Context, sm *stmgr.StateManager, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
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
	var crossMessages []*types.Message

	var blsMsgCids, secpkMsgCids, crossMsgCids []cid.Cid
	var blsSigs []crypto.Signature
	for _, msg := range bt.Messages {
		err := sigs.Verify(&msg.Signature, msg.Message.From, msg.Message.Cid().Bytes())
		if err != nil {
			log.Warn("invalid signed message was filtered")
			continue
		}

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

		crossMessages = append(crossMessages, msg)
		crossMsgCids = append(crossMsgCids, c)
	}

	store := sm.ChainStore().ActorStore(ctx)
	blsMsgRoot, err := consensus.ToMessagesArray(store, blsMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building bls amt: %w", err)
	}
	secpkMsgRoot, err := consensus.ToMessagesArray(store, secpkMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building secpk amt: %w", err)
	}
	crossMsgRoot, err := consensus.ToMessagesArray(store, crossMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building cross amt: %w", err)
	}

	mmcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   blsMsgRoot,
		SecpkMessages: secpkMsgRoot,
		CrossMessages: crossMsgRoot,
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
		CrossMessages: crossMessages,
	}, nil
}

// isBlockSealed checks that the following conditions hold:
//     - all messages from the Filecoin block are contained in the Tendermint block.
//     - Tendermint block hash is equal to Filecoin ticket field.
func isBlockSealed(fb *types.FullBlock, tb *tenderminttypes.Block) error {
	tendermintMessagesHashes, err := getMessageMapFromTendermintBlock(tb)
	if err != nil {
		return xerrors.Errorf("unable to get Tendermint message map: %w", err)
	}

	for _, msg := range fb.BlsMessages {
		_, found := tendermintMessagesHashes[msg.Cid()]
		if !found {
			return xerrors.New("bls messages are not sealed")
		}
	}

	for _, msg := range fb.SecpkMessages {
		_, found := tendermintMessagesHashes[msg.Cid()]
		if !found {
			return xerrors.New("secpk messages are not sealed")
		}
	}

	for _, msg := range fb.CrossMessages {
		_, found := tendermintMessagesHashes[msg.Cid()]
		if !found {
			return xerrors.New("cross msgs messages are not sealed")
		}
	}

	if !bytes.Equal(fb.Header.Ticket.VRFProof, tb.Hash().Bytes()) {
		log.Infof("block tendermint hash is invalid %x", fb.Header.Ticket.VRFProof)
		return xerrors.New("block ticket and hash are different")
	}
	return nil
}

func validateLocalBlock(ctx context.Context, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	stats.Record(ctx, metrics.BlockPublished.M(1))

	if size := msg.Size(); size > 1<<20-1<<15 {
		log.Errorf("ignoring oversize block (%dB)", size)
		return pubsub.ValidationIgnore, "oversize_block"
	}

	blk, what, err := decodeAndCheckBlock(msg)
	if err != nil {
		log.Errorf("got invalid local block: %s", err)
		return pubsub.ValidationIgnore, what
	}

	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func decodeAndCheckBlock(msg *pubsub.Message) (*types.BlockMsg, string, error) {
	blk, err := types.DecodeBlockMsg(msg.GetData())
	if err != nil {
		return nil, "invalid", xerrors.Errorf("error decoding block: %w", err)
	}

	if count := len(blk.BlsMessages) + len(blk.SecpkMessages); count > build.BlockMessageLimit {
		return nil, "too_many_messages", fmt.Errorf("block contains too many messages (%d)", count)
	}

	// make sure we have a signature
	if blk.Header.BlockSig != nil {
		return nil, "missing_signature", fmt.Errorf("block with a signature")
	}

	return blk, "", nil
}
