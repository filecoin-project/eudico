package mirbft

import (
	"context"
	"time"

	mirbft "github.com/hyperledger-labs/mirbft/pkg/types"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("Mir miner started")
	defer log.Info("Mir miner stopped")

	mirAgent, err := NewMirAgent(uint64(0))
	if err != nil {
		return err
	}
	mirErrors := mirAgent.Start(ctx)

	netName, nerr := api.StateNetworkName(ctx)
	if nerr != nil {
		return nerr
	}
	subnetID := address.SubnetID(netName)

	log.Infof("Mir miner params: network name - %s, subnet ID - %s", netName, subnetID)

	mining := time.NewTicker(600 * time.Millisecond)
	defer mining.Stop()

	for {
		select {
		case merr := <-mirErrors:
			return xerrors.Errorf("Mir node error:", merr)
		case <-ctx.Done():
			log.Debug("Mir miner %s: context closed")
			return nil
		case <-mining.C:
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

			// TODO: Here several cross-messages may have the same nonce (as we discussed for Tendermint).
			// This may be an issue for Mir (from our previous discussions). Just to have it in mind.
			crossMsgs, err := api.GetUnverifiedCrossMsgsPool(ctx, subnetID, base.Height()+1)
			if err != nil {
				log.Errorw("unable to select cross-messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d crossmsgs", subnetID, base.Height()+1, len(crossMsgs))

			for _, msg := range msgs {
				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error("unable to serialize message:", err)
					continue
				}

				reqNo := mirbft.ReqNo(msg.Message.Nonce)
				tx := common.NewSignedMessageBytes(msgBytes, nil)

				// TODO: define what client ID is in Eudico case.
				// Probably, client ID is a peer ID. In this case Mir's client ID interface and type should be changed.
				//
				// nodeID, err := api.ID(ctx)
				//	if err != nil {
				//		log.Fatalf("unable to get a node ID: %s", err)
				//	}
				//	nodeIDBytes := blake3.Sum256([]byte(nodeID.String()))
				err = mirAgent.Node.SubmitRequest(ctx, mirbft.ClientID(0), reqNo, tx, nil)
				if err != nil {
					log.Error("unable to submit a message to Mir:", err)
					continue
				}
				log.Debug("successfully sent a message to Mir")

			}

			for _, msg := range crossMsgs {
				msgBytes, err := msg.Msg.Serialize()
				if err != nil {
					log.Error("unable to serialize cross-message:", err)
					continue
				}

				tx := common.NewCrossMessageBytes(msgBytes, nil)
				reqNo := mirbft.ReqNo(msg.Msg.Nonce)

				err = mirAgent.Node.SubmitRequest(ctx, mirbft.ClientID(0), reqNo, tx, nil)
				if err != nil {
					log.Error("unable to submit a message to Mir:", err)
					continue
				}
				log.Debug("successfully sent a message to Mir")

			}

			// TODO: this approach to get messages works only for one node.
			// For general case we should have a mechanism that allows to all nodes get the same messages from the application.
			block := mirAgent.App.Block()

			var newMsgs []*types.SignedMessage
			var newCrossMsgs []*types.Message

			for _, tx := range block {
				msg, err := parseTx(tx)
				if err != nil {
					log.Error("unable to parse a message:", err)
					log.Info(msg)
					continue
				}
				switch m := msg.(type) {
				case *types.Message:
					newCrossMsgs = append(newCrossMsgs, m)
				case *types.SignedMessage:
					newMsgs = append(newMsgs, m)
				default:
					log.Error("received an unknown message")
				}
			}

			log.Infof("[subnet: %s, epoch: %d] try to create a block", subnetID, base.Height()+1)
			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          base.Key(),
				BeaconValues:     nil,
				Ticket:           nil,
				Epoch:            base.Height() + 1,
				Timestamp:        uint64(time.Now().Unix()),
				WinningPoStProof: nil,
				Messages:         newMsgs,
				CrossMessages:    newCrossMsgs,
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

func (bft *Mir) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
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
