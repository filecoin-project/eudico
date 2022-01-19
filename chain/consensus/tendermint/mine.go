package tendermint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/ipfs/go-cid"
	tenderminttypes "github.com/tendermint/tendermint/types"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/crypto"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/types"
)

func Mine(ctx context.Context, miner address.Address, api v1api.FullNode) error {
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
			log.Errorw("selecting messages failed", "error", err)
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
			Miner:            miner, //TODO: use real Tendermint ID, check that that is correct.
			Parents:          base.Key(),
			Ticket:           nil, //TODO: define the value if needed
			Eproof:           nil,
			BeaconValues:     nil,
			Messages:         msgs,
			Epoch:            base.Height() + 1,
			Timestamp:        base.MinTimestamp() + build.BlockDelaySecs,
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

		log.Info("try Tendermint mining at @", base.Height(), base.String())

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

func (tendermint *Tendermint) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	log.Infof("starting creating block in epoch %d", bt.Epoch)
	defer log.Infof("stopping creating block in epoch %d", bt.Epoch)

	pts, err := tendermint.sm.ChainStore().LoadTipSet(bt.Parents)
	if err != nil {
		return nil, xerrors.Errorf("failed to load parent tipset: %w", err)
	}

	st, recpts, err := tendermint.sm.TipSetState(ctx, pts)
	if err != nil {
		return nil, xerrors.Errorf("failed to load tipset state: %w", err)
	}

	next := &types.BlockHeader{
		Miner:         bt.Miner, //TODO: define miner value
		Parents:       bt.Parents.Cids(),
		Ticket:        bt.Ticket,
		ElectionProof: bt.Eproof,

		BeaconEntries:         bt.BeaconValues,
		Height:                bt.Epoch,
		Timestamp:             bt.Timestamp,
		WinPoStProof:          bt.WinningPoStProof,
		ParentStateRoot:       st,
		ParentMessageReceipts: recpts,
	}

	log.Infof("Next block is %+v", next)

	var blsMessages []*types.Message
	var secpkMessages []*types.SignedMessage

	var blsMsgCids, secpkMsgCids, crossMsgCids []cid.Cid
	var blsSigs []crypto.Signature
	for _, msg := range bt.Messages {
		if msg.Signature.Type == crypto.SigTypeBLS {
			blsSigs = append(blsSigs, msg.Signature)
			blsMessages = append(blsMessages, &msg.Message)

			c, err := tendermint.sm.ChainStore().PutMessage(&msg.Message)
			if err != nil {
				return nil, err
			}

			blsMsgCids = append(blsMsgCids, c)
		} else {
			c, err := tendermint.sm.ChainStore().PutMessage(msg)
			if err != nil {
				return nil, err
			}

			secpkMsgCids = append(secpkMsgCids, c)
			secpkMessages = append(secpkMessages, msg)
		}
	}

	for _, msg := range bt.CrossMessages {
		c, err := tendermint.sm.ChainStore().PutMessage(msg)
		if err != nil {
			return nil, err
		}

		crossMsgCids = append(crossMsgCids, c)
	}

	store := tendermint.sm.ChainStore().ActorStore(ctx)
	blsmsgroot, err := consensus.ToMessagesArray(store, blsMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building bls amt: %w", err)
	}
	secpkmsgroot, err := consensus.ToMessagesArray(store, secpkMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building secpk amt: %w", err)
	}
	crossmsgroot, err := consensus.ToMessagesArray(store, crossMsgCids)
	if err != nil {
		return nil, xerrors.Errorf("building cross amt: %w", err)
	}

	mmcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   blsmsgroot,
		SecpkMessages: secpkmsgroot,
		CrossMessages: crossmsgroot,
	})
	if err != nil {
		return nil, err
	}
	next.Messages = mmcid

	aggSig, err := consensus.AggregateSignatures(blsSigs)
	if err != nil {
		return nil, err
	}

	next.BLSAggregate = aggSig
	pweight, err := tendermint.sm.ChainStore().Weight(ctx, pts)
	if err != nil {
		return nil, err
	}
	next.ParentWeight = pweight

	baseFee, err := tendermint.sm.ChainStore().ComputeBaseFee(ctx, pts)
	if err != nil {
		return nil, xerrors.Errorf("computing base fee: %w", err)
	}
	next.ParentBaseFee = baseFee

	nosigbytes, err := next.SigningBytes()
	if err != nil {
		return nil, xerrors.Errorf("failed to get signing bytes for block: %w", err)
	}

	sig, err := w.WalletSign(ctx, bt.Miner, nosigbytes, lapi.MsgMeta{
		Type: lapi.MTBlock,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to sign new block: %w", err)
	}

	next.BlockSig = sig

	fullBlock := &types.FullBlock{
		Header:        next,
		BlsMessages:   blsMessages,
		SecpkMessages: secpkMessages,
		CrossMessages: bt.CrossMessages,
	}

	fullBlockHeaderBytes, err := fullBlock.Header.Serialize()
	if err != nil {
		return nil, xerrors.Errorf("unable to serialize a block: %w", err)
	}
	fullBlockHeaderHash := sha256.Sum256(fullBlockHeaderBytes)

	errChan := make(chan error)
	result := make(chan *types.FullBlock)
	quitChan := make(chan struct{})
	createQuitChan := make(chan struct{})

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	timer := time.After(5 * time.Second)

	go func() {
		log.Infof("starting Tendermint receiving goroutine in epoch %d", bt.Epoch)
		defer log.Infof("stopping Tendermint receiving goroutine in epoch %d", bt.Epoch)
		for {
			select {
			case <-quitChan:
				return
			case <-ctx.Done():
				log.Info("No block was mined in Tendermint due to closing context")
				return
			//TODO: fix this. it stops after 10 sec
			case <-timer:
				log.Info("No block was mined in Tendermint due to reaching timeout")
				log.Info("Checking the last block in Tendermint ...")
				tendermintLastBlock, err := tendermint.client.Block(ctx, nil)
				if err != nil {
					log.Info("unable to get the last Tendermint block: %s", err)
				}
				log.Infof("Tendermint block height: %d", tendermintLastBlock.Block.Height)
				log.Infof("Tendermint block last commit: %d", tendermintLastBlock.Block.LastCommit.Height)
				errChan <- xerrors.New("unable to create a block due to reaching timeout")
				return
			case <-ticker.C:
				log.Info("Ticker delivered")
				height := int64(bt.Epoch)+1
				tendermintTargetBlock, err := tendermint.client.Block(ctx, nil)
				if err != nil {
					log.Info("unable to get the target Tendermint block %d: %s", height, err)
					return
				}
				if tendermintTargetBlock.Block.Height < height {
					close(createQuitChan)
					return
				}
				tx := tendermintTargetBlock.Block.Txs[0].String()
				txo := tx[3 : len(tx)-1]
				txoData, err := hex.DecodeString(txo)
				if err != nil {
					errChan <- err
					return
				}
				receivedTxHash := sha256.Sum256(txoData)
				log.Infof("tendermint event commit block height: %d, mined block height: %d\n",
					tendermintTargetBlock.Block.Height, bt.Epoch)
				log.Infof("Tendermint receivedTxHash: %x, fullBlockHeaderHash: %x",
					receivedTxHash, fullBlockHeaderHash)
				if tendermintTargetBlock.Block.Height == int64(bt.Epoch)+1 &&
					bytes.Equal(receivedTxHash[:], fullBlockHeaderHash[:]) {
					result <- fullBlock
					return
				} else {
					close(createQuitChan)
					return
				}

			case e := <-tendermint.events:
				continue
				log.Info("Received an event from Tendermint")
				block, ok := e.Data.(tenderminttypes.EventDataNewBlock)
				if !ok {
					continue
				}
				log.Infof("Received an event block %d", block.Block.Height)
				if len(block.Block.Txs) != 1 || len(block.Block.Txs[0]) < len("Tx{}") {
					errChan <- xerrors.New("received an incorrect event from Tendermint")
					return
				}

				tx := block.Block.Txs[0].String()
				txo := tx[3 : len(tx)-1]
				log.Infof("received Tendermint event with height %d", block.Block.Height)
				txoData, err := hex.DecodeString(txo)
				if err != nil {
					errChan <- err
					return
				}
				receivedTxHash := sha256.Sum256(txoData)
				log.Infof("tendermint event commit block height: %d, mined block height: %d\n",
					block.Block.LastCommit.Height, bt.Epoch)
				log.Infof("receivedTxHash: %x, fullBlockHeaderHash: %x",
					receivedTxHash, fullBlockHeaderHash)
				if block.Block.Height == int64(bt.Epoch)+1 &&
					bytes.Equal(receivedTxHash[:], fullBlockHeaderHash[:]) {
					result <- fullBlock
					return
				} else  {
					log.Info("received a block from tendermint that we didn't send")
					close(createQuitChan)
					return
				}
			}
		}
	}()

	log.Info("Broadcast TX to Tendermint")
	_, err = tendermint.client.BroadcastTxSync(ctx, tenderminttypes.Tx(fullBlockHeaderBytes))
	if err != nil {
		log.Error("broadcast error:", err)
		close(quitChan)
		return nil, xerrors.Errorf("broadcasting a block as a TX to Tendermint: %w", err)
	}

	for {
		select {
		case <- createQuitChan:
			return nil, xerrors.New("Another block was mined")
		case err := <-errChan:
			return nil, err
		case block := <-result:
			return block, nil
		}
	}
}
