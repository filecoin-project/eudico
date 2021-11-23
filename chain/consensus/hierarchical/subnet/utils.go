package subnet

import (
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"golang.org/x/xerrors"
)

func tipSetExecutor(consensus subnet.ConsensusType) (stmgr.Executor, error) {
	switch consensus {
	case subnet.Delegated:
		return delegcns.TipSetExecutor(), nil
	case subnet.PoW:
		return tspow.TipSetExecutor(), nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func weight(consensus subnet.ConsensusType) (store.WeightFunc, error) {
	switch consensus {
	case subnet.Delegated:
		return delegcns.Weight, nil
	case subnet.PoW:
		return tspow.Weight, nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func newConsensus(consensus subnet.ConsensusType,
	sm *stmgr.StateManager, beacon beacon.Schedule,
	verifier ffiwrapper.Verifier,
	genesis chain.Genesis) (consensus.Consensus, error) {

	switch consensus {
	case subnet.Delegated:
		return delegcns.NewDelegatedConsensus(sm, beacon, verifier, genesis), nil
	case subnet.PoW:
		return tspow.NewTSPoWConsensus(sm, beacon, verifier, genesis), nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}
