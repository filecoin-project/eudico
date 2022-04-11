package mirbft

import (
	"context"
	"crypto"
	"fmt"

	mirCrypto "github.com/hyperledger-labs/mirbft/pkg/crypto"
	"github.com/hyperledger-labs/mirbft/pkg/dummyclient"
	mirLogging "github.com/hyperledger-labs/mirbft/pkg/logging"
	t "github.com/hyperledger-labs/mirbft/pkg/types"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("starting MirBFT miner: ", miner.String())
	defer log.Info("shutdown MirBFT miner: ", miner.String())

	ownID := t.NodeID(0)
	nodeIds := []t.NodeID{ownID}
	reqReceiverAddrs := make(map[t.NodeID]string)
	for _, i := range nodeIds {
		reqReceiverAddrs[i] = fmt.Sprintf("127.0.0.1:%d", reqReceiverBasePort+i)
	}

	mirBFTClient := dummyclient.NewDummyClient(
		t.ClientID(0),
		crypto.SHA256,
		&mirCrypto.DummyCrypto{DummySig: []byte{0}},
		mirLogging.ConsoleDebugLogger,
	)

	mirBFTClient.Connect(context.Background(), reqReceiverAddrs)

	nn, err := api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	subnetID := address.SubnetID(nn)

	log.Infof("MirBFT miner params: network name - %s, subnet ID - %s", nn, subnetID)

	for {
		select {
		case <-ctx.Done():
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

			for _, msg := range msgs {
				id := msg.Cid().String()

				log.Debugf("[subnet: %s, epoch: %d] >>>>> msg to send: %s", subnetID, base.Height()+1, id)

				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error("unable to serialize message:", err)
					continue
				}

				err = mirBFTClient.SubmitRequest(msgBytes)
				if err != nil {
					log.Error("unable to send a message to Tendermint:", err)
					continue
				}
				log.Debugf("successfully sent a message %s to Tendermint", id)

			}

			//TODO: we send only messages, have to add cross-messages.

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

			log.Infof("[subnet: %s, epoch: %d] mined a block %v", subnetID, bh.Header.Height, bh.Cid())
		}
	}

	return nil
}

func (m *MirBFT) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block for epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block for epoch %d", bt.Epoch)

	return nil, nil
}
