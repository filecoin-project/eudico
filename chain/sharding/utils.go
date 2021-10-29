package sharding

import (
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"golang.org/x/xerrors"
)

func tipSetExecutor(consensus shard.ConsensusType) (stmgr.Executor, error) {
	switch consensus {
	case shard.Delegated:
		return delegcns.TipSetExecutor(), nil
	case shard.PoW:
		return tspow.TipSetExecutor(), nil
	case shard.FilCns:
		return filcns.NewTipSetExecutor(), nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func weight(consensus shard.ConsensusType) (store.WeightFunc, error) {
	switch consensus {
	case shard.Delegated:
		return delegcns.Weight, nil
	case shard.PoW:
		return tspow.Weight, nil
	case shard.FilCns:
		return filcns.Weight, nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func newConsensus(consensus shard.ConsensusType,
	sm *stmgr.StateManager, beacon beacon.Schedule,
	verifier ffiwrapper.Verifier,
	genesis chain.Genesis) (consensus.Consensus, error) {

	switch consensus {
	case shard.Delegated:
		return delegcns.NewDelegatedConsensus(sm, beacon, verifier, genesis), nil
	case shard.PoW:
		return tspow.NewTSPoWConsensus(sm, beacon, verifier, genesis), nil
	case shard.FilCns:
		return filcns.NewFilecoinExpectedConsensus(sm, beacon, verifier, genesis), nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}
