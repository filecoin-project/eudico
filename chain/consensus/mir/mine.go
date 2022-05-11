package mir

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/platform/logging"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	t "github.com/filecoin-project/mir/pkg/types"
)

// Mine handles mining using Mir.
//
// Mine implements the following algorithm:
// 1. Retrieve messages and cross-messages from mempool.
//    Note, that messages can be added into mempool via the libp2p mechanism and the CLI.
// 2. Send these messages to the Mir node.
// 3. Receive ordered messages from the Mir node and push them into the next Filecoin block.
// 4. Submit this block over libp2p network.
//
// There are two ways how mining with Mir can be started:
// 1) Environment variables: validators ID and their network addresses are passed
//    via EUDICO_MIR_MINERS and EUDICO_MIR_ID variables.
//    This approach can be used to run Mir in the root network and for simple demos.
// 2) Hierarchical consensus framework: validators IDs and their network addresses
//    are received via state, after each validator joins the subnet.
//    This is used to run Mir in a subnet.
func Mine(ctx context.Context, addr address.Address, api v1api.FullNode) error {
	log = logging.FromContext(ctx, log)

	m, err := newMiner(ctx, addr, api)
	if err != nil {
		return err
	}
	log.Infof("Mir miner %s started", m.mirID())
	defer log.Infof("Mir miner %s completed", m.mirID())

	log.Infof("Miner info:\n\twallet - %s\n\tnetwork - %s\n\tsubnet - %s\n\tMir ID - %s\n\tvalidators - %v",
		m.addr, m.netName, m.subnetID, m.mirID(), m.validators)

	mirAgent, err := NewMirAgent(ctx, m.mirID(), m.validators)
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
			log.Errorw("failed to get chain head", "error", err)
			continue
		}

		// Miner for an epoch is chosen deterministically using round-robin.
		epochMiner := m.validators[int(base.Height())%len(m.validators)].Addr
		log.Debugf("Miner in %d epoch: %v", base.Height(), epochMiner)

		select {
		case <-ctx.Done():
			log.Debug("Mir miner: context closed")
			return nil
		case err := <-mirErrors:
			return err
		case mb := <-mirHead:
			msgs, crossMsgs := getMessagesFromMirBlock(mb)

			log.Infof("[subnet: %s, epoch: %d] try to create a block", m.subnetID, base.Height()+1)
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
				log.Errorw("creating a block failed", "error", err)
				continue
			}
			if bh == nil {
				log.Debug("created a nil block")
				continue
			}

			log.Infof("[subnet: %s, epoch: %d] try to sync a block", m.subnetID, base.Height()+1)
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

			log.Infof("[subnet: %s, epoch: %d] %s mined a block %v",
				m.subnetID, bh.Header.Height, epochMiner, bh.Cid())
		case <-submit.C:
			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.Errorw("unable to select messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d msgs from mempool", m.subnetID, base.Height()+1, len(msgs))

			crossMsgs, err := api.GetUnverifiedCrossMsgsPool(ctx, m.subnetID, base.Height()+1)
			if err != nil {
				log.Errorw("unable to select cross-messages from mempool", "error", err)
			}
			log.Debugf("[subnet: %s, epoch: %d] retrieved %d crossmsgs from mempool", m.subnetID, base.Height()+1, len(crossMsgs))

			for _, msg := range msgs {

				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error("unable to serialize a message:", err)
					continue
				}

				// client ID for this message MUST BE the same on all Eudico nodes.
				clientID := fmt.Sprintf("%s:%s", m.subnetID, msg.Message.From.String())
				reqNo := t.ReqNo(msg.Message.Nonce)
				tx := common.NewSignedMessageBytes(msgBytes, nil)

				err = mirAgent.Node.SubmitRequest(ctx, t.ClientID(clientID), reqNo, tx, []byte{})
				if err != nil {
					log.Error("unable to submit a message to Mir:", err)
					continue
				}
				log.Debugf("successfully sent message %s from %s to Mir", msg.Message.Cid(), clientID)
			}

			for _, msg := range crossMsgs {
				msgBytes, err := msg.Serialize()
				if err != nil {
					log.Error("unable to serialize cross-message:", err)
					continue
				}

				tx := common.NewCrossMessageBytes(msgBytes, nil)
				reqNo := t.ReqNo(msg.Message.Nonce)
				msn, err := msg.Message.From.Subnet()
				if err != nil {
					log.Error("unable to get subnet from message:", err)
					continue
				}
				// client ID for this message MUST BE the same on all Eudico nodes.
				clientID := fmt.Sprintf("%s:%s", msn, msg.Message.From.String())

				err = mirAgent.Node.SubmitRequest(ctx, t.ClientID(clientID), reqNo, tx, []byte{})
				if err != nil {
					log.Error("unable to submit a message to Mir:", err)
					continue
				}
				log.Debugf("successfully sent message %s from %s to Mir", msg.Cid(), clientID)
			}
		}
	}
}

type minerInfo struct {
	netName    dtypes.NetworkName
	subnetID   address.SubnetID
	addr       address.Address
	validators []hierarchical.Validator
}

func newMiner(ctx context.Context, addr address.Address, api v1api.FullNode) (*minerInfo, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}
	subnetID := address.SubnetID(netName)

	var validators []hierarchical.Validator

	minersEnv := os.Getenv(MirMinersEnv)
	if minersEnv != "" {
		validators, err = hierarchical.ValidatorsFromString(minersEnv)
		if err != nil {
			return nil, xerrors.Errorf("failed to get validators from string: %s", err)
		}
	} else {
		if subnetID == address.RootSubnet {
			return nil, xerrors.New("can't be run in the rootnet without validators")
		}
		validators, err = api.SubnetStateGetValidators(ctx, subnetID)
		if err != nil {
			return nil, xerrors.New("failed to get validators from state")
		}
	}
	if len(validators) == 0 {
		return nil, xerrors.New("empty validator set")
	}

	m := minerInfo{
		addr:       addr,
		subnetID:   subnetID,
		netName:    netName,
		validators: validators,
	}

	return &m, nil
}

func (m *minerInfo) mirID() string {
	return fmt.Sprintf("%s:%s", m.subnetID, m.addr)
}
