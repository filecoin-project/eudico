package dummy

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
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

	validatorsEnv := os.Getenv(ValidatorsEnv)
	var validators []address.Address
	validators, err = validatorsFromString(validatorsEnv)
	if err != nil {
		return fmt.Errorf("failed to get validators addresses: %w", err)
	}
	if len(validators) == 0 {
		validators = append(validators, miner)
	}

	submit := time.NewTicker(400 * time.Millisecond)
	defer submit.Stop()

	leader := validators[0]

	for {
		select {
		case <-ctx.Done():
			log.Debug("Dummy miner: context closed")
			return nil

		case <-submit.C:
			base, err := api.ChainHead(ctx)
			if err != nil {
				log.Errorw("failed to get the head of chain", "error", err)
				continue
			}

			if miner != leader {
				continue
			}

			epochMiner := validators[int(base.Height())%len(validators)]

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

			err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("unable to sync a block", "error", err)
				continue
			}

			log.Infof("[subnet: %s, epoch: %d] mined a block %v", subnetID, bh.Header.Height, bh.Cid())
		}
	}
}

func (bft *Dummy) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	b, err := common.PrepareBlockForSignature(ctx, bft.sm, bt)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// validatorsFromString parses comma-separated validator addresses string.
//
// Examples of the validator string: "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy,t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy"
func validatorsFromString(input string) ([]address.Address, error) {
	var addrs []address.Address
	for _, id := range hierarchical.SplitAndTrimEmpty(input, ",", " ") {
		a, err := address.NewFromString(id)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %v: %w", id, err)
		}
		addrs = append(addrs, a)
	}
	return addrs, nil
}
