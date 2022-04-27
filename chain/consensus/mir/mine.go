package mir

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	mirTypes "github.com/filecoin-project/mir/pkg/types"
)

// Mine mines Filecoin blocks.

// Mine implements the following algorithm:
// 1. Retrieve messages and cross-messages from mempool.
// 2. Send these messages to the Mir node.
// 3. Receive ordered messages from the Mir node and push them into the next Filecoin block.
// 4. Submit this block.
func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("Mir miner started")
	defer log.Info("Mir miner stopped")

	// TODO: Suppose we want to use Mir in Root and in a subnet at the same time.
	// Do we need two Mir agents for that?
	// nodeID, err := api.ID(ctx)
	// if err != nil {
	//	log.Fatalf("unable to get a node ID: %s", err)
	// }
	nodeID := NodeID()
	nodeNumber := NodeNumber()

	mirAgent, err := NewMirAgent(nodeID, nodeNumber)
	if err != nil {
		return err
	}
	mirErrors := mirAgent.Start(ctx)
	mirHead := mirAgent.App.ChainNotify

	netName, nerr := api.StateNetworkName(ctx)
	if nerr != nil {
		return nerr
	}
	subnetID := address.SubnetID(netName)

	log.Infof("Mir miner params: network name - %s, subnet ID - %s", netName, subnetID)

	submit := time.NewTicker(SubmitInterval)
	defer submit.Stop()

	for {
		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("failed to get the head of chain", "error", err)
			continue
		}

		select {
		case <-ctx.Done():
			log.Debug("Mir miner: context closed")
			return nil
		case merr := <-mirErrors:
			return merr
		case mb := <-mirHead:
			log.Debugf(">>>>> received %d messages in Mir block", len(mb))
			msgs, crossMsgs := getMessagesFromMirBlock(mb)

			log.Infof("[subnet: %s, epoch: %d] try to create a block", subnetID, base.Height()+1)
			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				// TODO: if there are several nodes then the miner is the Mir node proposed the block within Mir.
				Miner:            miner,
				Parents:          base.Key(),
				BeaconValues:     nil,
				Ticket:           &ltypes.Ticket{VRFProof: nil},
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
		case <-submit.C:
			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.Errorw("unable to select messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d msgs", subnetID, base.Height()+1, len(msgs))

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

				reqNo := mirTypes.ReqNo(msg.Message.Nonce)
				tx := common.NewSignedMessageBytes(msgBytes, nil)

				// TODO: client ID should be subnet.String()+"::"+peer.ID.String()
				err = mirAgent.Node.SubmitRequest(ctx, mirTypes.ClientID(nodeID), reqNo, tx, nil)
				if err != nil {
					log.Error("unable to submit a message to Mir:", err)
					continue
				}
				log.Debug("successfully sent a message to Mir")
			}

			for _, msg := range crossMsgs {
				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error("unable to serialize cross-message:", err)
					continue
				}

				tx := common.NewCrossMessageBytes(msgBytes, nil)
				reqNo := mirTypes.ReqNo(msg.Msg.Nonce)

				err = mirAgent.Node.SubmitRequest(ctx, mirTypes.ClientID(0), reqNo, tx, []byte{})
				if err != nil {
					log.Error("unable to submit a message to Mir:", err)
					continue
				}
				log.Debug("successfully sent a message to Mir")
			}
		}
	}
}

// CreateBlock creates a final Filecoin block from the input block template.
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
