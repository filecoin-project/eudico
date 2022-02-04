package tendermint

import (
	"context"
	"crypto/sha256"
	"time"

	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	tmclient "github.com/tendermint/tendermint/rpc/client/http"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

var pool = newMessagePool()

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	log.Info("Miner addr:", miner.String())
	tendermintClient, err := tmclient.New(NodeAddr())
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
			Miner:            address.Undef,
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

func (tm *Tendermint) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block for epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block for epoch %d", bt.Epoch)

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	tendermintBlockInfoChan := make(chan *tendermintBlockInfo)
	height := int64(bt.Epoch) + tm.offset

	go func() {
		for {
			select {
			case <-ctx.Done():
				// fixme: what should me send here?
				tendermintBlockInfoChan <- nil
				return
			case <-ticker.C:
				resp, err := tm.client.Block(ctx, &height)
				if err != nil {
					log.Infof("unable to get the last Tendermint block @%d: %s", height, err)
					continue
				} else {
					log.Infof("Got block %d from Tendermint", resp.Block.Height)
					info := tendermintBlockInfo{
						timestamp: uint64(resp.Block.Time.Unix()),
						hash:      resp.Block.Hash().Bytes(),
						proposerAddress: resp.Block.ProposerAddress.String(),
					}
					parseTendermintBlock(resp.Block, &info, tm.tag)

					tendermintBlockInfoChan <- &info
				}
			}
		}
	}()

	tb := <-tendermintBlockInfoChan

	bt.Miner = tm.clientAddress
	// if another Tendermint node proposed the block.
	if tb.proposerAddress != tm.validatorAddress {
		eudicoAddress, ok := tm.tendermintEudicoAddresses[tb.proposerAddress]
		// known address
		if ok {
			bt.Miner = eudicoAddress
		// unknown address
		} else {
			resp, err := tm.client.Validators(ctx, &height, nil, nil)
			if err != nil {
				log.Infof("unable to get Tendermint validators for %d height: %s", height, err)
				return nil, err
			}

			proposerPubKey := findValidatorPubKeyByAddress(resp.Validators, []byte(tb.proposerAddress))
			if proposerPubKey == nil {
				return nil, xerrors.New("unable to find target public key")
			}

			uncompressedProposerPubKey, err := secp.ParsePubKey(proposerPubKey)

			eudicoAddress, err := address.NewSecp256k1Address(uncompressedProposerPubKey.SerializeUncompressed())
			if err != nil {
				log.Info("unable to create address in Eudico format:", err)
				return nil, err
			}
			tm.tendermintEudicoAddresses[tb.proposerAddress] = eudicoAddress
			bt.Miner = eudicoAddress
		}
	}

	bt.Messages = tb.messages
	bt.CrossMessages = tb.crossMsgs
	bt.Timestamp = tb.timestamp

	b, err := sanitizeMessagesAndPrepareBlockForSignature(ctx, tm.sm, bt)
	if err != nil {
		log.Info(err)
		return nil, err
	}

	h := b.Header
	baseTs, err := tm.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
	if err != nil {
		return nil, xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	validMsgs, err := common.FilterBlockMessages(ctx, tm.store, tm.sm, tm.subMgr, tm.r, tm.netName, b, baseTs)
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
		err = tm.validateBlock(ctx, b)
		if err != nil {
			log.Info(err)
			return nil, err
		}

	*/

	log.Infof("!!!!%s mined a block", b.Header.Miner)

	return b, nil
}
