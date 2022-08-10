package roundrobin

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/platform/logging"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log := logging.FromContext(ctx, log)

	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	subnetID, err := address.SubnetIDFromString(string(netName))
	if err != nil {
		return err
	}

	submitting := time.NewTicker(300 * time.Millisecond)
	defer submitting.Stop()

	validatorsEnv := os.Getenv(ValidatorsEnv)
	validators, err := validatorsFromString(validatorsEnv)
	if err != nil {
		return fmt.Errorf("failed to get validators addresses: %w", err)
	}

	log.Infof("Round-robin info:\n\twallet - %s\n\tnetwork - %s\n\tsubnet - %s\n\tvalidators - %v",
		miner, netName, subnetID, validators)

	for {
		select {
		case <-ctx.Done():
			log.Info("Round-robin miner: context closed")
			return nil
		case <-submitting.C:
			base, err := api.ChainHead(ctx)
			if err != nil {
				log.Errorw("failed to get the head of chain", "error", err)
				continue
			}

			epochMiner := validators[int(base.Height())%len(validators)]
			/*
				if epochMiner != miner {
					time.Sleep(200 * time.Millisecond)
					continue
				}

			*/

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
				Miner:            epochMiner,
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

			err = api.SyncBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("unable to sync a block", "error", err)
				continue
			}

			log.Infof("[subnet: %s, epoch: %d] %v mined a block %v", subnetID, bh.Header.Height, miner, bh.Cid())
		}
	}
}

func (bft *RoundRobin) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	b, err := common.PrepareBlockForSignature(ctx, bft.sm, bt)
	if err != nil {
		return nil, err
	}

	return b, nil
}
