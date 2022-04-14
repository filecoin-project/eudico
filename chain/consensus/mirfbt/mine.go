package mirbft

import (
	"context"
	"time"

	mirbft "github.com/hyperledger-labs/mirbft/pkg/types"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	addr := miner.String()
	log.Infof("MirBFT miner %s: started", addr)
	defer log.Info("MirBFT miner: stopped")

	netName, nerr := api.StateNetworkName(ctx)
	if nerr != nil {
		return nerr
	}
	subnetID := address.SubnetID(netName)

	log.Infof("MirBFT miner params: network name - %s, subnet ID - %s", netName, subnetID)

	for {
		select {
		case <-ctx.Done():
			log.Debugf("MirBFT miner %s: context closed", addr)
			return nil
		default:
			base, err := api.ChainHead(ctx)
			if err != nil {
				log.Errorw("failed to get the head of chain", "error", err)
				continue
			}

			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.Errorw("unable to select messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d msgs", subnetID, base.Height()+1, len(msgs))

			crossMsgs, err := api.GetCrossMsgsPool(ctx, subnetID, base.Height()+1)
			if err != nil {
				log.Errorw("unable to select cross-messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d crossmsgs", subnetID, base.Height()+1, len(crossMsgs))

			log.Infof("[subnet: %s, epoch: %d] try to create a block", subnetID, base.Height()+1)
			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          base.Key(),
				BeaconValues:     nil,
				Ticket:           nil,
				Epoch:            base.Height() + 1,
				Timestamp:        uint64(time.Now().Unix()),
				WinningPoStProof: nil,
				Messages:         msgs,
				CrossMessages:    crossMsgs,
			})
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}
			if bh == nil {
				log.Debug("created a nil block")
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
				log.Errorw("unable to sync a block", "error", err)
				continue
			}

			log.Infof("[subnet: %s, epoch: %d] mined a block %v", subnetID, bh.Header.Height, bh.Cid())
		}
	}
}

func (bft *MirBFT) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("create block for epoch %d", bt.Epoch)

	for _, msg := range bt.Messages {
		msgBytes, err := msg.Serialize()
		if err != nil {
			log.Error("unable to serialize message:", err)
			continue
		}

		reqNo := mirbft.ReqNo(msg.Message.Nonce)
		tx := common.NewSignedMessageBytes(msgBytes, nil)

		err = bft.mir.Node.SubmitRequest(ctx, mirbft.ClientID(0), reqNo, tx, nil)
		if err != nil {
			log.Error("unable to submit a message to MirBFT:", err)
			continue
		}
		log.Debug("successfully sent a message to MirBFT")

	}

	for _, msg := range bt.CrossMessages {
		msgBytes, err := msg.Serialize()
		if err != nil {
			log.Error("unable to serialize cross-message:", err)
			continue
		}

		tx := common.NewCrossMessageBytes(msgBytes, nil)
		reqNo := mirbft.ReqNo(msg.Nonce)

		err = bft.mir.Node.SubmitRequest(ctx, mirbft.ClientID(0), reqNo, tx, nil)
		if err != nil {
			log.Error("unable to submit a message to MirBFT:", err)
			continue
		}
		log.Debug("successfully sent a message to MirBFT")

	}

	// TODO: temporal delay to be able run tests.
	// Throughput in the basic setting is so high that MirBFT and all other slow consensus are not in sync within subnet.
	time.Sleep(500 * time.Millisecond)
	block := bft.mir.App.Block()

	var msgs []*types.SignedMessage
	var crossMsgs []*types.Message

	for _, tx := range block {
		msg, err := parseTx(tx)
		if err != nil {
			log.Error("unable to parse a message:", err)
			log.Info(msg)
			continue
		}
		switch m := msg.(type) {
		case *types.Message:
			crossMsgs = append(crossMsgs, m)
		case *types.SignedMessage:
			msgs = append(msgs, m)
		default:
			log.Error("received an unknown message")
		}
	}

	bt.Messages = msgs
	bt.CrossMessages = crossMsgs

	b, err := common.PrepareBlockForSignature(ctx, bft.sm, bt)
	if err != nil {
		return nil, err
	}

	err = common.SignBlock(ctx, w, b)
	if err != nil {
		return nil, err
	}

	return b, nil
}
