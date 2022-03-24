package tendermint

// This is an adaptation of the reference Tendermint application for Eudico needs.
// The reference implementation can be found here - https://github.com/tendermint/tendermint/tree/master/abci/example.

import (
	"time"

	abci "github.com/tendermint/tendermint/abci/types"
	logger "github.com/tendermint/tendermint/libs/log"
)

const (
	codeBadRequest   = 513
	codeStateFailure = 514
)

var (
	version                  = "0.0.3"
	_       abci.Application = (*Application)(nil)
)

type Application struct {
	consensus *State
	logger    logger.Logger
}

func NewApplication() (*Application, error) {
	return &Application{
		consensus: NewState(),
		logger:    logger.NewNopLogger(),
	}, nil
}

func (a *Application) Info(abci.RequestInfo) (resp abci.ResponseInfo) {
	return abci.ResponseInfo{
		Data:             time.Now().Format("2006.01.02 15:04:05"),
		Version:          version,
		LastBlockHeight:  a.consensus.Height(),
		LastBlockAppHash: a.consensus.Hash(),
	}
}

func (a *Application) InitChain(req abci.RequestInitChain) (resp abci.ResponseInitChain) {
	return abci.ResponseInitChain{}
}

func (a *Application) Query(req abci.RequestQuery) (resp abci.ResponseQuery) {
	switch req.Path {
	case "/reg":
		subnetID := req.Data
		height, ok := a.consensus.GetSubnetOffset(subnetID)
		if !ok {
			return abci.ResponseQuery{
				Code: codeStateFailure,
				Log:  "subnet offset hasn't been set yet",
			}
		}

		regResp := RegistrationMessageResponse{
			Name:   subnetID,
			Offset: height,
		}

		data, err := regResp.Serialize()
		if err != nil {
			return abci.ResponseQuery{
				Code: codeBadRequest,
				Log:  err.Error(),
			}
		}

		resp.Key = req.Data
		resp.Value = data
		return
	default:
		return abci.ResponseQuery{
			Code: abci.CodeTypeOK,
		}
	}
}

func (a *Application) BeginBlock(req abci.RequestBeginBlock) (resp abci.ResponseBeginBlock) {
	return abci.ResponseBeginBlock{}
}

func (a *Application) CheckTx(req abci.RequestCheckTx) (resp abci.ResponseCheckTx) {
	tx := req.GetTx()

	_, code, err := parseTx(tx)
	if err != nil {
		return abci.ResponseCheckTx{
			Code: code,
			Log:  err.Error(),
		}
	}

	return abci.ResponseCheckTx{
		Code: abci.CodeTypeOK,
	}
}

func (a *Application) DeliverTx(req abci.RequestDeliverTx) (resp abci.ResponseDeliverTx) {
	tx := req.GetTx()

	msg, code, err := parseTx(tx)
	if err != nil {
		return abci.ResponseDeliverTx{
			Code: code,
			Log:  err.Error(),
		}
	}

	switch subnet := msg.(type) {
	case *RegistrationMessageRequest:
		// We support only one subnet per Tendermint blockchain.
		_, existedSubnet := a.consensus.GetSubnetOffset(subnet.Name)
		if a.consensus.IsSubnetSet() && !existedSubnet {
			return abci.ResponseDeliverTx{
				Code: codeStateFailure,
				Log:  "only one subnet can be registered",
			}
		}
		height := a.consensus.SetOrGetSubnetOffset(subnet.Name)
		regResp := RegistrationMessageResponse{
			Name:   subnet.Name,
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

	return abci.ResponseDeliverTx{
		Code: abci.CodeTypeOK,
	}
}

func (a *Application) EndBlock(req abci.RequestEndBlock) (resp abci.ResponseEndBlock) {
	a.logger.Info("End block height:", req.Height)
	a.consensus.m.Lock()
	a.consensus.height = req.Height
	a.consensus.m.Unlock()

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
