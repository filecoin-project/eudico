package mir

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/platform/logging"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
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
	log := logging.FromContext(ctx, log)

	m, err := newMiner(ctx, addr, api)
	if err != nil {
		return err
	}
	log.Infof("Mir miner %s started", m.mirID())
	defer log.Infof("Mir miner %s completed", m.mirID())

	log.Infof("Miner info:\n\twallet - %s\n\tnetwork - %s\n\tsubnet - %s\n\tMir ID - %s\n\tvalidators - %v",
		m.addr, m.netName, m.subnetID, m.mirID(), m.validators)
	log.Info("Mir timer:", build.MirTimer)

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
		// Only one miner pulls the ordered messages and proposes a Filecoin block.
		// We are suggesting no faults.
		if epochMiner != addr {
			continue
		}
		log.Debugf("Miner in %d epoch: %v", base.Height(), epochMiner)

		select {
		case <-ctx.Done():
			log.Debug("Mir miner: context closed")
			return nil
		case err := <-mirErrors:
			return err
		case hashes := <-mirHead:
			msgs, crossMsgs := m.getMessagesByHashes(hashes)
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
			var refs []*RequestRef // These are request references.
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

			refs, err = m.addSignedMessages(refs, msgs)
			if err != nil {
				log.Errorw("unable to add messages", "error", err)
			}

			refs, err = m.addCrossMessages(refs, crossMsgs)
			if err != nil {
				log.Errorw("unable to add cross-messages", "error", err)
			}

			mirAgent.SubmitRequests(ctx, refs)
		}
	}
}
