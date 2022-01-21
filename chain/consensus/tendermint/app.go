package tendermint

import (
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	logger "github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	tendermintabci "github.com/tendermint/tendermint/abci/types"

	"github.com/filecoin-project/lotus/chain/types"
)

var (
	version = "0.0.1"
	_ tendermintabci.Application = (*Application)(nil)
)

type Application struct {
	mempool *State
	consensus *State
	logger    logger.Logger
}

func NewApplication() (*Application, error) {
	return &Application{
		consensus: NewState(),
		mempool: NewState(),
		logger: logger.NewLogfmtLogger(logger.NewSyncWriter(os.Stdout)),
	}, nil
}

func (a *Application) Info(tendermintabci.RequestInfo) (resp tendermintabci.ResponseInfo) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Info",
			"data", resp.Data,
			"version", resp.Version,
			"last_block_height", resp.LastBlockHeight,
			"last_block_app_hash", fmt.Sprintf("%X", resp.LastBlockAppHash),
		)
	}()

	return tendermintabci.ResponseInfo{
		Data:             time.Now().Format("2006.01.02 15:04:05"),
		Version:          version,
		LastBlockHeight:  a.consensus.Commits(),
		LastBlockAppHash: a.consensus.GetLastBlockHash(),
	}
}

func (a *Application) InitChain(req tendermintabci.RequestInitChain) (resp tendermintabci.ResponseInitChain) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "InitChain",
			"time", req.Time.String(),
			"chain_id", req.ChainId,
			"app_state_bytes", len(req.AppStateBytes),
		)
	}()

	return tendermintabci.ResponseInitChain{}
}

func (a *Application) Query(req tendermintabci.RequestQuery) (resp tendermintabci.ResponseQuery) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Query",
			"height", resp.GetHeight(),
			"data", string(req.Data),
			"ok", resp.IsOK(),
			"code", resp.Code,
			"key", resp.Key,
			"value", resp.Value,
			"log", resp.Log,
			"info", resp.Info,
		)
	}()

	id := string(req.Data)

	value, found := a.consensus.GetBlock(id)
	if !found {
		return tendermintabci.ResponseQuery{
			Code: codeBadRequest,
			Key:  req.Data,
			Log:  fmt.Sprintf("block with height %s not found", id),
		}
	}

	return tendermintabci.ResponseQuery{
		Code:  tendermintabci.CodeTypeOK,
		Key:   req.Data,
		Value: value,
	}
}

func (a *Application) BeginBlock(req tendermintabci.RequestBeginBlock) (resp tendermintabci.ResponseBeginBlock) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "BeginBlock",
			"hash", fmt.Sprintf("%x", req.Hash),
			"header.chain_id", req.Header.ChainID,
			"header.height", req.Header.Height,
			"header.time", req.Header.Time,
			"last_commit_info.round", req.LastCommitInfo.Round,
			"byzantine_validators", len(req.ByzantineValidators),
		)
	}()

	return tendermintabci.ResponseBeginBlock{}
}

func (a *Application) CheckTx(req tendermintabci.RequestCheckTx) (resp tendermintabci.ResponseCheckTx) {
	a.logger.Log(req.GetTx())
	_, err := types.DecodeSignedMessage(req.GetTx())
	if err != nil {
		a.logger.Log("CheckTx_decoding_Tx_error:", err)
		return tendermintabci.ResponseCheckTx{
			Code: codeBadRequest,
			Log:  fmt.Sprintf("unable to decode a Filecoin message: %s", err.Error()),
		}
	}

	id :=sha256.Sum256(req.Tx)
	ok := a.mempool.ExistTx(id)
	if ok {
		return tendermintabci.ResponseCheckTx{
			Code: codeBadRequest,
			Log: fmt.Sprintf("tx already added: %s", id),
		}
	}
	a.mempool.AddTx(id)

	level.Debug(a.logger).Log(
		"abci", "CheckTx",
		"tx len", len(req.Tx),
		"ok", resp.IsOK(),
		"code", resp.Code,
		"gas_used", resp.GasUsed,
		"gas_wanted", resp.GasWanted,
		"log", resp.Log,
		"info", resp.Info,
	)

	return tendermintabci.ResponseCheckTx{
		Code: tendermintabci.CodeTypeOK,
	}
}

func (a *Application) DeliverTx(req tendermintabci.RequestDeliverTx) (resp tendermintabci.ResponseDeliverTx) {
	_, err := types.DecodeSignedMessage(req.Tx)
	if err != nil {
		return tendermintabci.ResponseDeliverTx{
			Code: codeBadRequest,
			Log:  err.Error(),
		}
	}

	defer func() {
		level.Debug(a.logger).Log(
			"abci", "DeliverTx",
			"ok", resp.IsOK(),
			"code", resp.Code,
			"log", resp.Log,
			"info", resp.Info,
		)
	}()

	return tendermintabci.ResponseDeliverTx{
		Code: tendermintabci.CodeTypeOK,
	}
}

func (a *Application) EndBlock(req tendermintabci.RequestEndBlock) (resp tendermintabci.ResponseEndBlock) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "EndBlock",
			"height", req.Height,
			"consensus_state_commits", a.consensus.Commits(),
		)
	}()

	return tendermintabci.ResponseEndBlock{}
}

func (a *Application) Commit() (resp tendermintabci.ResponseCommit) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Commit",
			"data", fmt.Sprintf("%x", resp.Data),
		)
	}()

	err := a.consensus.Commit()
	if err != nil {
		panic(fmt.Sprintf("error: Commit failed: %v", err))
	}

	copyState(a.mempool, a.consensus)

	return tendermintabci.ResponseCommit{
		Data: a.consensus.GetLastBlockHash(),
	}
}

func (a *Application) ListSnapshots(tendermintabci.RequestListSnapshots) tendermintabci.ResponseListSnapshots {
	return tendermintabci.ResponseListSnapshots{}
}

func (a *Application) OfferSnapshot(tendermintabci.RequestOfferSnapshot) tendermintabci.ResponseOfferSnapshot {
	return tendermintabci.ResponseOfferSnapshot{}
}

func (a *Application) LoadSnapshotChunk(tendermintabci.RequestLoadSnapshotChunk) tendermintabci.ResponseLoadSnapshotChunk {
	return tendermintabci.ResponseLoadSnapshotChunk{}
}

func (a *Application) ApplySnapshotChunk(tendermintabci.RequestApplySnapshotChunk) tendermintabci.ResponseApplySnapshotChunk {
	return tendermintabci.ResponseApplySnapshotChunk{}
}

const (
	codeBadRequest = 513 // arbitrary non-zero
	codeStateFailure = 514
)


