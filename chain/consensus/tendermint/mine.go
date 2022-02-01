package tendermint

import (
	"context"
	"crypto/sha256"
	"time"

	httptendermintrpcclient "github.com/tendermint/tendermint/rpc/client/http"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

var pool = newMessagePool()

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	tendermintClient, err := httptendermintrpcclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s", err)
	}

	nn, err := api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	log.Info("Network name:", nn)

	subnetID := address.SubnetID(nn)
	log.Info("Subnet ID name:", subnetID)

	tag := sha256.Sum256([]byte(subnetID))
	log.Info("tag:", tag[:4])

	head, err := api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("getting head: %w", err)
	}

	log.Infof("%s starting tendermint mining on @%d",subnetID, head.Height())
	defer log.Info("%s stopping tendermint mining on @%d", subnetID, head.Height())

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("creating block failed", "error", err)
			continue
		}

		log.Infof("%s try tendermint mining at @%d", subnetID, base.Height())

		msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
		if err != nil {
			log.Errorw("unable to select messages from mempool", "error", err)
		}

		log.Debugf("Msgs being proposed in block @%s: %d", base.Height()+1, len(msgs))

		// Get cross-message pool from subnet.
		nn, err := api.StateNetworkName(ctx)
		if err != nil {
			return err
		}
		crossMsgs, err := api.GetCrossMsgsPool(ctx, address.SubnetID(nn), base.Height()+1)
		if err != nil {
			log.Errorw("selecting cross-messages failed", "error", err)
		}
		log.Infof("CrossMsgs being proposed in block @%s: %d", base.Height()+1, len(crossMsgs))

		for _, msg := range msgs {
			tx, err := msg.Serialize()
			if err != nil {
				log.Error(err)
				continue
			}
			log.Debug("next msg:", tx)

			// LRU cache is used to store the messages that have already been sent.
			// It is also a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
			id := sha256.Sum256(tx)
			log.Debug("message hash:", id)

			if pool.shouldSubmitMessage(tx, base.Height()) {
				payload := append(tx, tag[:4]...)
				payload = append(payload, SignedMessageType)
				res, err := tendermintClient.BroadcastTxSync(ctx, payload)
				if err != nil {
					log.Error("unable to send message to Tendermint error:", err)
					continue
				} else {
					pool.addMessage(tx, base.Height())
					log.Info(res)
					log.Info("successfully sent msg to Tendermint:", id)
				}
			}
		}

		for _, msg := range crossMsgs {
			tx, err := msg.Serialize()
			if err != nil {
				log.Error(err)
				continue
			}
			log.Info("!!! next cross msg:", tx)

			// LRU cache is used to store the messages that have already been sent.
			// It is also a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
			id := sha256.Sum256(tx)
			log.Info("!!!!! cross message hash:", id)

			if pool.shouldSubmitMessage(tx, base.Height()) {
				payload := append(tx, tag[:4]...)
				payload = append(payload, CrossMessageType)
				res, err := tendermintClient.BroadcastTxSync(ctx, payload)
				if err != nil {
					log.Error("unable to send cross message to Tendermint error:", err)
					continue
				} else {
					pool.addMessage(tx, base.Height())
					log.Info(res)
					log.Info("successfully sent cross msg to Tendermint:", id)
				}
			}
		}

		bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
			Miner:            miner,
			Parents:          base.Key(),
			BeaconValues:     nil,
			Ticket:           nil,
			Messages:         nil,
			Epoch:            base.Height() + 1,
			Timestamp:        0,
			WinningPoStProof: nil,
			CrossMessages:    nil,
		})
		if err != nil {
			log.Errorw("creating block failed", "error", err)
			continue
		}
		if bh == nil {
			continue
		}

		log.Infof("%s try syncing Tendermint block at @%d", subnetID, base.Height())

		err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
			Header:        bh.Header,
			BlsMessages:   bh.BlsMessages,
			SecpkMessages: bh.SecpkMessages,
			CrossMessages: bh.CrossMessages,
		})
		if err != nil {
			log.Errorw("submitting block failed", "error", err)
		}

		log.Infof("Tendermint mined a block %v in %s:", bh.Cid(), subnetID)
	}
}

func (tendermint *Tendermint) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block for epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block for epoch %d", bt.Epoch)

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	tendermintBlockInfoChan := make(chan *tendermintBlockInfo)
	height := int64(bt.Epoch) + tendermint.offset

	go func() {
		for {
			select {
			case <-ctx.Done():
				// fixme: what should me send here?
				tendermintBlockInfoChan <- nil
				return
			case <-ticker.C:
				resp, err := tendermint.client.Block(ctx, &height)
				if err != nil {
					log.Infof("unable to get the last Tendermint block @%d: %s", height, err)
					continue
				} else {
					log.Infof("Got block %d from Tendermint", resp.Block.Height)
					info := tendermintBlockInfo{
						timestamp: uint64(resp.Block.Time.Unix()),
						minerAddr: resp.Block.ProposerAddress.Bytes(),
						hash:      resp.Block.Hash().Bytes(),
					}
					parseTendermintBlock(resp.Block, &info, tendermint.tag)

					tendermintBlockInfoChan <- &info
				}
			}
		}
	}()
	tb := <-tendermintBlockInfoChan

	addr, err := address.NewSecp256k1Address(tb.minerAddr)
	if err != nil {
		log.Info("unable to decode miner addr:", err)
	}
	log.Info(addr)

	bt.Messages = tb.messages
	bt.CrossMessages = tb.crossMsgs
	bt.Timestamp = tb.timestamp
	//TODO: what is the miner addr?
	//bt.Miner = addr

	b, err := sanitizeMessagesAndPrepareBlockForSignature(ctx, tendermint.sm, bt)
	if err != nil {
		log.Info(err)
		return nil, err
	}

	h := b.Header
	baseTs, err := tendermint.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
	if err != nil {
		return nil, xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	validMsgs, err := common.FilterBlockMessages(ctx, tendermint.store, tendermint.sm, tendermint.subMgr, tendermint.r, tendermint.netName, b, baseTs)
	if validMsgs.BLSMessages != nil {
		b.BlsMessages = validMsgs.BLSMessages
	}
	if validMsgs.SecpkMessages != nil {
		b.SecpkMessages = validMsgs.SecpkMessages
	}
	if validMsgs.CrossMsgs != nil {
		b.CrossMessages = validMsgs.CrossMsgs
	}

	b.Header.Ticket = &types.Ticket{VRFProof: tb.hash}

	/*
		err = tendermint.validateBlock(ctx, b)
		if err != nil {
			log.Info(err)
			return nil, err
		}

	*/

	return b, nil
}