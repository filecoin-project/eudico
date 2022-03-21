package tendermint

import (
	"context"
	"time"

	tmclient "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/rpc/coretypes"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	tendermintConsensusBlockDelay = 1200
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("starting miner: ", miner.String())
	defer log.Info("shutdown miner")

	var cache = newMessageCache()

	tendermintClient, err := tmclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s", err)
	}

	nn, err := api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	subnetID := address.SubnetID(nn)

	log.Infof("miner params: network:%s, subnet ID: %s", nn, subnetID)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("failed to get the head of chain", "error", err)
			continue
		}

		msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
		if err != nil {
			log.Errorw("unable to select messages from mempool", "error", err)
		}

		crossMsgs, err := api.GetCrossMsgsPool(ctx, subnetID, base.Height()+1)
		if err != nil {
			log.Errorw("unable to get cross-messages from mempool", "error", err)
		}
		log.Infof("[subnet: %s, epoch: %d] retrieved %d - msgs, %d - crossmsgs", subnetID, base.Height()+1, len(msgs), len(crossMsgs))

		for _, msg := range msgs {
			id := msg.Cid().String()

			if cache.shouldSendMessage(id) {
				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error(err)
					continue
				}
				tx := NewSignedMessageBytes(msgBytes)
				_, err = tendermintClient.BroadcastTxSync(ctx, tx)
				if err != nil {
					log.Error("unable to send a message to Tendermint:", err)
					continue
				} else {
					cache.addSentMessage(id, base.Height())
					log.Infof("successfully sent a message %s to Tendermint", id)
				}
			}
		}

		for _, msg := range crossMsgs {
			id := msg.Cid().String()

			if cache.shouldSendMessage(id) {
				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error(err)
					continue
				}
				tx := NewCrossMessageBytes(msgBytes)
				_, err = tendermintClient.BroadcastTxSync(ctx, tx)
				if err != nil {
					log.Error("unable to send a cross message to Tendermint:", err)
					continue
				} else {
					cache.addSentMessage(id, base.Height())
					log.Infof("successfully sent a message %s to Tendermint", id)
				}
			}
		}

		log.Infof("[subnet: %s, epoch: %d] try to create a block", subnetID, base.Height()+1)

		bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
			Miner:            address.Undef,
			Parents:          base.Key(),
			BeaconValues:     nil,
			Ticket:           nil,
			Epoch:            base.Height() + 1,
			Timestamp:        0,
			WinningPoStProof: nil,
			Messages:         nil,
			CrossMessages:    nil,
		})
		if err != nil {
			log.Errorw("creating block failed", "error", err)
			continue
		}
		if bh == nil {
			continue
		}

		log.Infof("[subnet: %s, epoch: %d] try to sync a block", subnetID, base.Height()+1)

		err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
			Header:        bh.Header,
			BlsMessages:   bh.BlsMessages,
			SecpkMessages: bh.SecpkMessages,
			CrossMessages: bh.CrossMessages,
		})
		if err != nil {
			log.Errorw("unable to sync the block", "error", err)
		}

		cache.clearSentMessages(base.Height())

		log.Infof("[subnet: %s, epoch: %d] mined a block %v", subnetID, bh.Header.Height, bh.Cid())
	}
}

func (tm *Tendermint) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block for epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block for epoch %d", bt.Epoch)

	ticker := time.NewTicker(time.Duration(tendermintConsensusBlockDelay) * time.Millisecond)
	defer ticker.Stop()

	// Calculate actual target height of the Tendermint blockchain.
	height := int64(bt.Epoch) + tm.offset

	try := true
	var next *coretypes.ResultBlock
	var err error
	for try {
		select {
		case <-ctx.Done():
			log.Info("create block function was canceled")
			return nil, nil
		case <-ticker.C:
			log.Info("Received an new block from Tendermint over RPC")
			next, err = tm.client.Block(ctx, &height)
			if err != nil {
				log.Warnf("unable to get a Tendermint block via RPC @%d: %s", height, err)
				continue
			}
			try = false
		}
	}

	msgs, crossMsgs := getEudicoMessagesFromTendermintBlock(next.Block)
	bt.Messages = msgs
	bt.CrossMessages = crossMsgs
	bt.Timestamp = uint64(next.Block.Time.Unix())
	bt.Ticket = &types.Ticket{VRFProof: next.Block.Hash().Bytes()}

	proposerAddress := next.Block.ProposerAddress
	proposerAddrStr := proposerAddress.String()
	if proposerAddrStr != tm.tendermintValidatorAddress {
		// if another Tendermint node proposed the block.
		eudicoAddress, ok := tm.tendermintEudicoAddresses[proposerAddrStr]
		// We have already known the eudico address of the proposer
		if ok {
			bt.Miner = eudicoAddress
			// unknown address
		} else {
			resp, err := tm.client.Validators(ctx, &height, nil, nil)
			if err != nil {
				log.Infof("unable to get Tendermint validators for %d height: %s", height, err)
				return nil, err
			}

			proposerPubKey := findValidatorPubKeyByAddress(resp.Validators, proposerAddress)
			if proposerPubKey == nil {
				return nil, xerrors.New("unable to find the proposer's public key")
			}

			newEudicoAddress, err := getFilecoinAddrFromTendermintPubKey(proposerPubKey)
			if err != nil {
				return nil, err
			}
			tm.tendermintEudicoAddresses[proposerAddrStr] = newEudicoAddress
			bt.Miner = newEudicoAddress
		}
	} else {
		// Our Tendermint node proposed the block
		bt.Miner = tm.eudicoClientAddress
	}

	b, err := sanitizeMessagesAndPrepareBlockForSignature(ctx, tm.sm, bt)
	if err != nil {
		log.Info(err)
		return nil, err
	}

	h := b.Header
	baseTs, err := tm.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
	if err != nil {
		return nil, xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	validMsgs, err := common.FilterBlockMessages(ctx, tm.store, tm.sm, tm.subMgr, tm.r, tm.netName, b, baseTs)
	if err != nil {
		return nil, xerrors.Errorf("failed filtering block messages: %w", err)
	}
	if validMsgs.BLSMessages != nil {
		b.BlsMessages = validMsgs.BLSMessages
	}
	if validMsgs.SecpkMessages != nil {
		b.SecpkMessages = validMsgs.SecpkMessages
	}
	if validMsgs.CrossMsgs != nil {
		b.CrossMessages = validMsgs.CrossMsgs
	}

	log.Infof("%s mined a block", b.Header.Miner)

	return b, nil
}