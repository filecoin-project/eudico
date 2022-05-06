package mir

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	t "github.com/filecoin-project/mir/pkg/types"
)

// Mine mines Filecoin blocks.
//
// Mine implements the following algorithm:
// 1. Retrieve messages and cross-messages from mempool.
//    Note, that messages can be added into mempool via libp2p mechanisms and CLI.
// 2. Send these messages to the Mir node.
// 3. Receive ordered messages from the Mir node and push them into the next Filecoin block.
// 4. Submit this block over the libp2p network.
//
// There are two ways how mining with Mir consensus can be started:
// 1) Environment variables: validators ID and their network addresses are passed
//    via EUDICO_MIR_CLIENTS and EUDICO_MIR_ID variables.
//    This approach can be used to run Mir in the root network and for simple demos.
// 2) Hierarchical consensus framework: validators IDs and their network addresses
//    are received via state, after each validator joins the subnet.
//    This is used to run Mir in a subnet.
func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	seenMessages := make(map[cid.Cid]bool)
	var cache = newMessageCache()

	log.Info("Mir miner started")
	defer log.Info("Mir miner stopped")

	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	subnetID := address.SubnetID(netName)
	clientID := fmt.Sprintf("%s:%s", subnetID, miner)

	var validators []hierarchical.Validator

	clientsEnv := os.Getenv(MirClientsEnv)
	if clientsEnv != "" {
		validators, err = hierarchical.ValidatorsFromString(clientsEnv)
		if err != nil {
			return xerrors.Errorf("failed to get validators from string: %s", err)
		}
	} else {
		if subnetID == address.RootSubnet {
			return xerrors.New("can't be run in root net without provided validators")
		}
		validators, err = api.SubnetGetValidators(ctx, subnetID)
		if err != nil {
			return xerrors.New("failed to get validators from state")
		}
	}
	if len(validators) == 0 {
		return xerrors.New("empty validator set")
	}

	log.Infof("Mir miner params:\n\tminer - %s\n\tnetwork name - %s\n\tsubnet ID - %s\n\tclientID - %s\n\tvalidators - %v",
		miner, netName, subnetID, clientID, validators)

	// ----

	mirAgent, err := NewMirAgent(ctx, clientID, validators)
	if err != nil {
		return err
	}
	mirErrors := mirAgent.Start(ctx)
	mirHead := mirAgent.App.ChainNotify

	submit := time.NewTicker(SubmitInterval)
	defer submit.Stop()

	for {
		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("failed to get the head of chain", "error", err)
			continue
		}

		epochMiner := validators[int(base.Height())%len(validators)].Addr

		log.Infof("Miner for %d epoch: %v", base.Height(), epochMiner)

		select {
		case <-ctx.Done():
			log.Debug("Mir miner: context closed")
			return nil
		case merr := <-mirErrors:
			return merr
		case mb := <-mirHead:
			log.Debugf(">>>>> received %d messages in Mir block", len(mb))
			msgs, crossMsgs := getMessagesFromMirBlockUnstable(mb, seenMessages)

			log.Infof("[subnet: %s, epoch: %d] try to create a block", subnetID, base.Height()+1)
			bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
				Miner:            epochMiner,
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
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d msgs from mempool", subnetID, base.Height()+1, len(msgs))

			crossMsgs, err := api.GetUnverifiedCrossMsgsPool(ctx, subnetID, base.Height()+1)
			if err != nil {
				log.Errorw("unable to select cross-messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d crossmsgs from mempool", subnetID, base.Height()+1, len(crossMsgs))

			for _, msg := range msgs {
				id := msg.Cid().String()

				if cache.shouldSendMessage(id) {
					msgBytes, err := msg.Serialize()
					if err != nil {
						log.Error("unable to serialize message:", err)
						continue
					}

					reqNo := t.ReqNo(msg.Message.Nonce)
					tx := common.NewSignedMessageBytes(msgBytes, nil)

					err = mirAgent.SubmitRequest(ctx, reqNo, tx, []byte{})
					if err != nil {
						log.Error("unable to submit a message to Mir:", err)
						continue
					} else {
						cache.addSentMessage(id, base.Height())
						log.Debugf("%v successfully sent a request (%v, %d) to Mir", clientID, msg.Message.From, reqNo)
						log.Debugf("successfully sent a message %s to Mir", id)
					}
				}
			}

			for _, msg := range crossMsgs {
				id := msg.Cid().String()

				if cache.shouldSendMessage(id) {
					msgBytes, err := msg.Serialize()
					if err != nil {
						log.Error("unable to serialize cross-message:", err)
						continue
					}

					tx := common.NewCrossMessageBytes(msgBytes, nil)
					reqNo := t.ReqNo(msg.Msg.Nonce)

					err = mirAgent.SubmitRequest(ctx, reqNo, tx, []byte{})
					if err != nil {
						log.Error("unable to submit a message to Mir:", err)
						continue
					} else {
						cache.addSentMessage(id, base.Height())
						log.Debugf("%v successfully sent a cross request (%v, %d) to Mir", clientID, msg.Msg.From, reqNo)
						log.Debugf("successfully sent a cross request %s to Mir", id)
					}
				}

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

	// We don't sign blocks mined by Mir validators
	return b, nil
}
