package tendermint

import (
	"fmt"
	"os"
	"time"

	logger "github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	abci "github.com/tendermint/tendermint/abci/types"
)

const (
	codeBadRequest   = 513 // arbitrary non-zero
	codeStateFailure = 514
)

var (
	version                  = "0.0.2"
	_       abci.Application = (*Application)(nil)
)

type Application struct {
	mempool   *State
	consensus *State
	logger    logger.Logger
}

func NewApplication() (*Application, error) {
	return &Application{
		mempool:   NewState(),
		consensus: NewState(),
		logger:    logger.NewLogfmtLogger(logger.NewSyncWriter(os.Stdout)),
	}, nil
}

func (a *Application) Info(abci.RequestInfo) (resp abci.ResponseInfo) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Info",
			"data", resp.Data,
			"version", resp.Version,
			"last_block_height", resp.LastBlockHeight,
			"last_block_app_hash", fmt.Sprintf("%X", resp.LastBlockAppHash),
		)
	}()

	return abci.ResponseInfo{
		Data:             time.Now().Format("2006.01.02 15:04:05"),
		Version:          version,
		LastBlockHeight:  a.consensus.Commits(),
		LastBlockAppHash: []byte("123"),
	}
}

func (a *Application) InitChain(req abci.RequestInitChain) (resp abci.ResponseInitChain) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "InitChain",
			"time", req.Time.String(),
			"chain_id", req.ChainId,
			"validators", len(req.Validators),
		)
	}()

	return abci.ResponseInitChain{}
}

func (a *Application) Query(req abci.RequestQuery) (resp abci.ResponseQuery) {
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

	return abci.ResponseQuery{
		Code: abci.CodeTypeOK,
	}
}

func (a *Application) BeginBlock(req abci.RequestBeginBlock) (resp abci.ResponseBeginBlock) {
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

	return abci.ResponseBeginBlock{}
}

func (a *Application) CheckTx(req abci.RequestCheckTx) (resp abci.ResponseCheckTx) {
	tx := req.GetTx()
	a.logger.Log(tx)

	_, code, err := parseTx(tx)
	if err != nil {
		return abci.ResponseCheckTx{
			Code: code,
			Log:  err.Error(),
		}
	}

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

	return abci.ResponseCheckTx{
		Code: abci.CodeTypeOK,
	}
}

func (a *Application) DeliverTx(req abci.RequestDeliverTx) (resp abci.ResponseDeliverTx) {
	tx := req.GetTx()
	a.logger.Log(tx)

	msg, code, err := parseTx(tx)
	if err != nil {
		return abci.ResponseDeliverTx{
			Code: code,
			Log:  err.Error(),
		}
	}

	switch subnet := msg.(type) {
	case *RegistrationMessageRequest:
		height := a.consensus.GetSubnetOffset(subnet.Name)
		log.Info("Height:", height)
		regResp := RegistrationMessageResponse{
			Name: subnet.Name,
			Tag: subnet.Tag,
			Offset: height,
		}

		data, err := regResp.Serialize()
		if err != nil {
			return abci.ResponseDeliverTx{
				Code: codeBadRequest,
				Log:  err.Error(),
			}
		}

		return abci.ResponseDeliverTx{
			Code: abci.CodeTypeOK,
			Data: data,
		}
	default:
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

	return abci.ResponseDeliverTx{
		Code: abci.CodeTypeOK,
	}
}

func (a *Application) EndBlock(req abci.RequestEndBlock) (resp abci.ResponseEndBlock) {
	a.consensus.height = req.Height

	defer func() {
		level.Debug(a.logger).Log(
			"abci", "EndBlock",
			"height", req.Height,
			"consensus_state_commits", a.consensus.Commits(),
		)
	}()

	return abci.ResponseEndBlock{}
}

func (a *Application) Commit() (resp abci.ResponseCommit) {
	_ = a.consensus.Commit()
	return abci.ResponseCommit{}
}

func (a *Application) ListSnapshots(abci.RequestListSnapshots) abci.ResponseListSnapshots {
	return abci.ResponseListSnapshots{}
}

func (a *Application) OfferSnapshot(abci.RequestOfferSnapshot) abci.ResponseOfferSnapshot {
	return abci.ResponseOfferSnapshot{}
}

func (a *Application) LoadSnapshotChunk(abci.RequestLoadSnapshotChunk) abci.ResponseLoadSnapshotChunk {
	return abci.ResponseLoadSnapshotChunk{}
}

func (a *Application) ApplySnapshotChunk(abci.RequestApplySnapshotChunk) abci.ResponseApplySnapshotChunk {
	return abci.ResponseApplySnapshotChunk{}
}
