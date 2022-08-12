package mir

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"go.uber.org/zap/buffer"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/platform/logging"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
)

// TODO: Update the description below.

// Mine implements "block mining" using Mir framework.
//
// Mine implements the following algorithm:
// 1. Retrieve messages and cross-messages from the mempool.
//    Note, that messages can be added into mempool via the libp2p mechanism and the CLI.
// 2. Send messages and cross messages to the Mir node through the request pool implementing FIFO.
// 3. Receive ordered messages from the Mir node and parse them.
// 4. Create the next Filecoin block. Note, only a leader Eudico node, chosen by round-robin election, creates a block.
// 5. Sync this block without sending it over the libp2p network.
//
// There are two ways how mining with Mir can be started:
// 1) Environment variables: validators ID and network address are passed via EUDICO_MIR_VALIDATORS variable.
//    This approach can be used to run Mir in the root network and for simple demos.
// 2) Hierarchical consensus framework: validators ID and network address
//    are received via state, after each validator joins the subnet.
//    This is used to run Mir in a subnet.
func Mine(ctx context.Context, addr address.Address, api v1api.FullNode) error {
	log.With("addr", addr).Infof("Mir miner started")
	defer log.With("addr", addr).Infof("Mir miner completed")

	m, err := NewManager(ctx, addr, api)
	if err != nil {
		return fmt.Errorf("unable to create a manager: %w", err)
	}
	log := logging.FromContext(ctx, log).With("miner", m.ID())

	log.Infof("Miner info:\n\twallet - %s\n\tnetwork - %s\n\tsubnet - %s\n\tMir ID - %s\n\tvalidators - %v",
		m.Addr, m.NetName, m.SubnetID, m.MirID, m.LastValidatorSet.GetValidators())

	mirErrors := m.Start(ctx)
	mirHead := m.App.ChainNotify

	// TODO: remove or use the original variant of the for-loop.
	submit := time.NewTicker(SubmitInterval)
	defer submit.Stop()

	reconfigure := time.NewTicker(ReconfigurationInterval)
	defer reconfigure.Stop()

	// TODO: This timer is needed for debugging. Remove it when drafting is completed.
	updateEnv := time.NewTimer(time.Second * 6)
	defer updateEnv.Stop()

	lastValidatorSetHash, err := m.LastValidatorSet.Hash()
	if err != nil {
		return err
	}

	for {
		// Here we use ctx.Err() in the beginning of for loop instead of using it in the select statement
		// because if ctx has been closed then `api.ChainHead(ctx)` returns an error,
		// and we will be in the infinite loop dut to continue.
		if ctx.Err() != nil {
			log.Debug("Mir miner: context closed")
			return nil
		}
		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("failed to get chain head", "error", err)
			continue
		}

		// Miner (leader) for an epoch is assigned deterministically using round-robin.
		// All other validators use the same Miner in the block.
		epochMiner := m.GetBlockMiner(base.Height())
		nextEpoch := base.Height() + 1

		select {
		case err := <-mirErrors:
			// TODO: This is a temporal solution while we are discussing that issue
			// https://filecoinproject.slack.com/archives/C03C77HN3AS/p1660330971306019
			panic(fmt.Errorf("miner consensus error: %w", err))

			return fmt.Errorf("miner consensus error: %w", err)

		case <-reconfigure.C:
			//
			// Send a reconfiguration transaction if the validator set in the actor has been changed.
			//

			// TODO: this works only for demo purposes.
			// In reality, SCA must call us to signal that something has been changed.
			// For example, two changes may occur between reads and if validators read the state at different times
			// they could get different val sets.

			// TODO: decide what should we do with environment variable for root net and subnet actor for subnet.
			// if the variable is empty then we will get warn messages. And it must be empty because otherwise
			// it will be prioritized on subnet actor config for a subnet.
			// But to run Mir in rot and in a subnet and we must to use both.
			newValidatorSet, err := getSubnetValidators(ctx, m.SubnetID, api)
			if err != nil {
				log.With("epoch", nextEpoch).
					Warnf("failed to get subnet validators: %v", err)
				continue
			}

			newValidatorSetHash, err := newValidatorSet.Hash()
			if err != nil {
				log.With("epoch", nextEpoch).
					Warnf("failed to get validator set hash: %v", err)
				continue
			}

			if bytes.Equal(newValidatorSetHash, lastValidatorSetHash) {
				continue
			}

			log.With("epoch", nextEpoch).
				Infof("found new validator set hash: %v", newValidatorSetHash)
			lastValidatorSetHash = newValidatorSetHash

			var payload buffer.Buffer
			err = newValidatorSet.MarshalCBOR(&payload)
			if err != nil {
				log.With("epoch", nextEpoch).
					Warnf("failed to marshal validators: %v", err)
				continue
			}
			m.SubmitRequests(ctx, []*mirproto.Request{
				m.newReconfigurationRequest(payload.Bytes())},
			)

		case batch := <-mirHead:
			msgs, crossMsgs := m.GetMessages(batch)
			log.With("epoch", nextEpoch).
				Infof("try to create a block: msgs - %d, crossMsgs - %d", len(msgs), len(crossMsgs))

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
				log.With("epoch", nextEpoch).
					Errorw("creating a block failed", "error", err)
				continue
			}
			if bh == nil {
				log.With("epoch", nextEpoch).
					Debug("created a nil block")
				continue
			}

			err = api.SyncBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.With("epoch", nextEpoch).
					Errorw("unable to sync a block", "error", err)
				continue
			}

			log.With("epoch", nextEpoch).
				Infof("%s mined a block %v", epochMiner, bh.Cid())
		default:
			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.With("epoch", nextEpoch).
					Errorw("unable to select messages from mempool", "error", err)
			}

			crossMsgs, err := api.GetUnverifiedCrossMsgsPool(ctx, m.SubnetID, base.Height()+1)
			if err != nil {
				log.With("epoch", nextEpoch).
					Errorw("unable to select cross-messages from mempool", "error", err)
			}

			m.SubmitRequests(ctx, m.GetTransportRequests(msgs, crossMsgs))
		}
	}
}
