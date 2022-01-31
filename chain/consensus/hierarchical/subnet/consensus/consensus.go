package consensus

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("subnet-cns")

// TODO // FIXME: Make an SubnetConsensus interface from this functions
// to avoid having to use so many switch/cases. Deferring to the next
// refactor.
func Weight(consensus hierarchical.ConsensusType) (store.WeightFunc, error) {
	switch consensus {
	case hierarchical.Delegated:
		return delegcns.Weight, nil
	case hierarchical.PoW:
		return tspow.Weight, nil
	case hierarchical.Tendermint:
		return tendermint.Weight, nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func New(consensus hierarchical.ConsensusType,
	sm *stmgr.StateManager, snMgr subnet.SubnetMgr,
	beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier,
	genesis chain.Genesis, netName dtypes.NetworkName) (consensus.Consensus, error) {

	switch consensus {
	case hierarchical.Delegated:
		return delegcns.NewDelegatedConsensus(sm, snMgr, beacon, r, verifier, genesis, netName), nil
	case hierarchical.PoW:
		return tspow.NewTSPoWConsensus(sm, snMgr, beacon, r, verifier, genesis, netName), nil
	case hierarchical.Tendermint:
		return tendermint.NewConsensus(sm, snMgr, beacon, r, verifier, genesis, netName), nil
	default:
		return nil, xerrors.New("consensus type not suported")
	}
}

func Mine(ctx context.Context, api v1api.FullNode, cnsType hierarchical.ConsensusType) error {
	// TODO: We should check if these processes throw an error
	switch cnsType {
	case hierarchical.Delegated:
		go delegcns.Mine(ctx, api)
	case hierarchical.PoW:
		miner, err := GetWallet(ctx, api)
		if err != nil {
			log.Errorw("no valid identity found for PoW mining", "err", err)
			return err
		}
		go tspow.Mine(ctx, miner, api)
	case hierarchical.Tendermint:
		miner, err := GetWallet(ctx, api)
		if err != nil {
			log.Errorw("no valid identity found for Tendermint mining", "err", err)
			return err
		}
		go tendermint.Mine(ctx, miner, api)
	default:
		return xerrors.New("consensus type not suported")
	}
	return nil
}

// Get an identity from the peer's wallet.
// First check if a default identity has been set and
// if not take the first from the list.
// NOTE: We should probably make this configurable.
func GetWallet(ctx context.Context, api v1api.FullNode) (address.Address, error) {
	addr, err := api.WalletDefaultAddress(ctx)
	// If no defualt wallet set
	if err != nil || addr == address.Undef {
		addrs, err := api.WalletList(ctx)
		if err != nil {
			return address.Undef, err
		}
		if len(addrs) == 0 {
			return address.Undef, xerrors.Errorf("no valid wallet found in peer")
		}
		addr = addrs[0]
	}
	return addr, nil
}
