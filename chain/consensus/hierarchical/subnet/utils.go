package subnet

import (
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"golang.org/x/xerrors"
)

func weight(consensus hierarchical.ConsensusType) (store.WeightFunc, error) {
	switch consensus {
	case hierarchical.Delegated:
		return delegcns.Weight, nil
	case hierarchical.PoW:
		return tspow.Weight, nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func newConsensus(consensus hierarchical.ConsensusType,
	sm *stmgr.StateManager, beacon beacon.Schedule,
	verifier ffiwrapper.Verifier,
	genesis chain.Genesis) (consensus.Consensus, error) {

	switch consensus {
	case hierarchical.Delegated:
		return delegcns.NewDelegatedConsensus(sm, beacon, verifier, genesis), nil
	case hierarchical.PoW:
		return tspow.NewTSPoWConsensus(sm, beacon, verifier, genesis), nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}
