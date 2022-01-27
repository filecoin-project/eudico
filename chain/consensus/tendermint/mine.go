package tendermint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	httptendermintrpcclient "github.com/tendermint/tendermint/rpc/client/http"
	tenderminttypes "github.com/tendermint/tendermint/types"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
)

// finalityWait is the number of epochs that we will wait
// before being able to re-propose a msg.
const finalityWait = 100

func newMessagePool() *msgPool {
	return &msgPool{pool: make(map[[32]byte]abi.ChainEpoch)}
}

//TODO: messages should be removed from the pool after some time
type msgPool struct {
	lk   sync.RWMutex
	pool map[[32]byte]abi.ChainEpoch
}

func (p *msgPool) addMessage(tx []byte, epoch abi.ChainEpoch) {
	p.lk.Lock()
	defer p.lk.Unlock()

	id := sha256.Sum256(tx)
	p.pool[id] = epoch
}

func (p *msgPool) shouldSubmitMessage(tx []byte, currentEpoch abi.ChainEpoch) bool {
	p.lk.RLock()
	defer p.lk.RUnlock()

	id := sha256.Sum256(tx)
	proposedAt, proposed := p.pool[id]

	return !proposed || proposedAt + finalityWait < currentEpoch
}

var pool = newMessagePool()

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
	tendermintClient, err := httptendermintrpcclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s", err)
	}

	head, err := api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("getting head: %w", err)
	}

	log.Info("starting tendermint mining on @", head.Height())
	defer log.Info("stopping tendermint mining on @", head.Height())

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

		log.Info("try tendermint mining at @", base.Height())

		msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
		if err != nil {
			log.Errorw("unable to select messages from mempool", "error", err)
		}

		log.Infof("selected %d messages from mempool", len(msgs))
		for _, msg := range msgs {
			tx, err := msg.Serialize()
			if err != nil {
				log.Error(err)
				continue
			}
			log.Info(tx)

			// LRU cache is used to store the messages that have already been sent.
			// It is also a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
			id := sha256.Sum256(tx)
			log.Info("message hash:", id)

			if pool.shouldSubmitMessage(tx, base.Height()) {
				res, err := tendermintClient.BroadcastTxSync(ctx, tenderminttypes.Tx(tx))
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

		// Get cross-message pool from subnet.
		nn, err := api.StateNetworkName(ctx)
		if err != nil {
			return err
		}
		crossmsgs, err := api.GetCrossMsgsPool(ctx, hierarchical.SubnetID(nn), base.Height()+1)
		if err != nil {
			log.Errorw("selecting cross-messages failed", "error", err)
		}
		log.Debugf("CrossMsgs being proposed in block @%s: %d", base.Height()+1, len(crossmsgs))

		bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
			Parents:          base.Key(),
			BeaconValues:     nil,
			Ticket:           nil,
			Messages:         nil,
			Epoch:            base.Height() + 1,
			Timestamp:        0,
			WinningPoStProof: nil,
			CrossMessages:    crossmsgs,
		})
		if err != nil {
			log.Errorw("creating block failed", "error", err)
			continue
		}
		if bh == nil {
			continue
		}

		log.Info("try syncing Tendermint block at @", base.Height(), base.String())

		err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
			Header:        bh.Header,
			BlsMessages:   bh.BlsMessages,
			SecpkMessages: bh.SecpkMessages,
			CrossMessages: bh.CrossMessages,
		})
		if err != nil {
			log.Errorw("submitting block failed", "error", err)
		}

		log.Info("Tendermint mined a block!!! ", bh.Cid())
	}
}

func parseTendermintBlock(b *tenderminttypes.Block) []*types.SignedMessage {
	var msgs []*types.SignedMessage
	for _, tx := range b.Txs {
		stx := tx.String()
		txo := stx[3 : len(stx)-1]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			log.Error("unable to decode string while parsing Tx:", err)
			continue
		}
		msg, err := types.DecodeSignedMessage(txoData)
		if err != nil {
			log.Error("unable to decode SignedMessage while parsing Tx:", err)
			continue
		}
		log.Info("Received Tx:", msg)
		msgs = append(msgs, msg)
	}
	return msgs
}

type tendermintBlockInfo struct {
	timestamp uint64
	messages []*types.SignedMessage
	crossMsgs []*types.Message
	minerAddr []byte
	hash []byte
}

func (tendermint *Tendermint) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block for epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block for epoch %d", bt.Epoch)

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	tendermintBlockInfoChan := make(chan *tendermintBlockInfo)
	height := int64(bt.Epoch) + 1

	go func() {
		for {
			select {
			case <-ctx.Done():
				// fixme: what should me send here?
				tendermintBlockInfoChan <- nil
				return
			case <-ticker.C:
				res, err := tendermint.client.Block(ctx, &height)
				log.Infof("Got block %d from Tendermint", res.Block.Height)
				if err != nil {
					log.Info("unable to get the last Tendermint block @%d: %s", height, err)
					continue
				} else {
					info := tendermintBlockInfo{
						messages: parseTendermintBlock(res.Block),
						timestamp: uint64(res.Block.Time.Unix()),
						minerAddr: res.Block.ProposerAddress.Bytes(),
						hash: res.Block.Hash().Bytes(),
					}
					tendermintBlockInfoChan <- &info
				}
			}
		}
	}()
	tb := <-tendermintBlockInfoChan

	log.Info("received msgs from channel:", len(tb.messages))
	addr, err :=  address.NewSecp256k1Address(tb.minerAddr)

	if err != nil {
		log.Info("unable to decode miner addr:", err)
	}
	bt.Messages = tb.messages
	bt.Timestamp = tb.timestamp
	bt.Miner = addr

	b, err := common.SanitizeMessagesAndPrepareBlockForSignature(ctx, tendermint.sm, bt)
	if err != nil {
		log.Info(err)
		return nil, err
	}

	h := b.Header
	baseTs, err := tendermint.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
	if err != nil {
		return nil, xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	validMsgs, err := common.FilterBlockMessages(ctx, tendermint.store, tendermint.sm, tendermint.subMgr, tendermint.netName, b, baseTs)
	if validMsgs.BLSMessages != nil {
		b.BlsMessages = validMsgs.BLSMessages
	}
	if validMsgs.SecpkMessages != nil {
		b.SecpkMessages = validMsgs.SecpkMessages
	}
	if validMsgs.CrossMsgs != nil {
		b.CrossMessages = validMsgs.CrossMsgs
	}

	err = signBlock(b, tb.hash)
	if err != nil {
		return nil, err
	}

	err = tendermint.validateBlock(ctx, b)
	if err != nil {
		log.Info(err)
		return nil, err
	}

	return b, nil
}

func signBlock(b *types.FullBlock, h []byte) error {
	b.Header.BlockSig = &crypto.Signature{
		//TODO: use this incorrect type to not modify "crypto/signature" upstream
		Type: crypto.SigTypeSecp256k1,
		Data: h,
	}
	return nil
}


