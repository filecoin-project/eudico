package tendermint

import (
	"context"
	"time"

	"github.com/minio/blake2b-simd"
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
	tendermintConsensusBlockDelay = 1000
)

var pool = newMessagePool()

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("starting miner: ", miner.String())
	defer log.Info("shutdown miner")

	tendermintClient, err := tmclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s", err)
	}

	nn, err := api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	subnetID := address.SubnetID(nn)
	tag := blake2b.Sum256([]byte(subnetID))
	log.Infof("miner params: network:%s, subnet ID: %s, subnet tag: %x", nn, subnetID, tag[:tagLength])

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("getting block failed", "error", err)
			continue
		}

		log.Infof("[%s] epoch is %d", subnetID, base.Height())

		msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
		if err != nil {
			log.Errorw("unable to select messages from mempool", "error", err)
		}

		crossMsgs, err := api.GetCrossMsgsPool(ctx, subnetID, base.Height()+1)
		if err != nil {
			log.Errorw("selecting cross-messages failed", "error", err)
		}
		log.Infof("[%s] retrieved %d - msgs, %d - crossmsgs from the pool for @%s", subnetID, len(msgs), len(crossMsgs), base.Height()+1)

		for _, msg := range msgs {
			msgBytes, err := msg.Serialize()
			if err != nil {
				log.Error(err)
				continue
			}

			// Message cache is used to store the messages that have already been sent to Tendermint.
			// It is also a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
			id := blake2b.Sum256(msgBytes)

			if pool.shouldSubmitMessage(msgBytes, base.Height()) {
				tx := NewSignedMessageBytes(msgBytes, tag[:])
				_, err := tendermintClient.BroadcastTxSync(ctx, tx)
				if err != nil {
					log.Error("unable to send msg to Tendermint:", err)
					continue
				} else {
					pool.addMessage(msgBytes, base.Height())
					log.Info("successfully sent msg to Tendermint:", id)
				}
			}
		}

		for _, msg := range crossMsgs {
			msgBytes, err := msg.Serialize()
			if err != nil {
				log.Error(err)
				continue
			}

			// Message cache is used to store the messages that have already been sent.
			// It is also a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
			id := blake2b.Sum256(msgBytes)

			if pool.shouldSubmitMessage(msgBytes, base.Height()) {
				tx := NewCrossMessageBytes(msgBytes, tag[:])
				_, err := tendermintClient.BroadcastTxSync(ctx, tx)
				if err != nil {
					log.Error("unable to send cross msg to Tendermint:", err)
					continue
				} else {
					pool.addMessage(msgBytes, base.Height())
					log.Info("successfully sent cross msg to Tendermint:", id)
				}
			}
		}

		log.Infof("[%s] try creating Tendermint block for epoch %d", subnetID, base.Height()+1)

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

		log.Infof("[%s] try syncing Tendermint block for epoch %d", subnetID, base.Height()+1)

		err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
			Header:        bh.Header,
			BlsMessages:   bh.BlsMessages,
			SecpkMessages: bh.SecpkMessages,
			CrossMessages: bh.CrossMessages,
		})
		if err != nil {
			log.Errorw("submitting block failed", "error", err)
		}

		log.Infof("[%s] Tendermint mined a block %v for epoch %d", subnetID, bh.Cid(), bh.Header.Height)
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
			log.Info("create block was stopped")
			return nil, nil
		case <-ticker.C:
			next, err = tm.client.Block(ctx, &height)
			if err != nil {
				log.Infof("unable to get the Tendermint block @%d: %s", height, err)
				continue
			}
			try = false
		}
	}

	msgs, crossMsgs := getEudicoMessagesFromTendermintBlock(next.Block, tm.tag)
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

	log.Infof("!!! %s mined a block", b.Header.Miner)

	return b, nil
}
