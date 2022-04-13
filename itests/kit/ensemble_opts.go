package kit

import (
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/wallet"
)

type EnsembleOpt func(opts *ensembleOpts) error

type genesisAccount struct {
	key            *wallet.Key
	initialBalance abi.TokenAmount
}

type ensembleOpts struct {
	pastOffset   time.Duration
	verifiedRoot genesisAccount
	accounts     []genesisAccount
	mockProofs   bool

	upgradeSchedule stmgr.UpgradeSchedule

	rootConsensus   hierarchical.ConsensusType
	subnetConsensus hierarchical.ConsensusType
}

var DefaultEnsembleOpts = ensembleOpts{
	pastOffset: 10000000 * time.Second, // time sufficiently in the past to trigger catch-up mining.
	upgradeSchedule: stmgr.UpgradeSchedule{{
		Height:  -1,
		Network: build.NewestNetworkVersion,
	}},
}

// RootTSPoW activates PoW consensus protocol for the root subnet in Eudico.
func RootTSPoW() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.rootConsensus = hierarchical.PoW
		return nil
	}
}

// RootDelegated activates Delegated consensus protocol for the root subnet in Eudico.
func RootDelegated() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.rootConsensus = hierarchical.Delegated
		return nil
	}
}

// RootTendermint activates Tendermint consensus protocol for the root subnet in Eudico.
func RootTendermint() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.rootConsensus = hierarchical.Tendermint
		return nil
	}
}

// RootFilcns activates Filcns consensus protocol for the root subnet in Eudico.
func RootFilcns() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.rootConsensus = hierarchical.FilecoinEC
		return nil
	}
}

// RootMirBFT activates MirBFT consensus protocol for the root subnet in Eudico.
func RootMirBFT() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.rootConsensus = hierarchical.FilecoinEC
		return nil
	}
}

// SubnetTSPoW activates PoW consensus protocol for a subnet in Eudico.
func SubnetTSPoW() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.subnetConsensus = hierarchical.PoW
		return nil
	}
}

// SubnetDelegated activates Delegated consensus protocol for a subnet in Eudico.
func SubnetDelegated() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.subnetConsensus = hierarchical.Delegated
		return nil
	}
}

// SubnetTendermint activates Tendermint consensus protocol for a subnet in Eudico.
func SubnetTendermint() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.subnetConsensus = hierarchical.Tendermint
		return nil
	}
}

// SubnetMirbft activates MirBFT consensus protocol for a subnet in Eudico.
func SubnetMirbft() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.subnetConsensus = hierarchical.MirBFT
		return nil
	}
}

// MockProofs activates mock proofs for the entire ensemble.
func MockProofs() EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.mockProofs = true
		// since we're using mock proofs, we don't need to download
		// proof parameters
		build.DisableBuiltinAssets = true
		return nil
	}
}

// RootVerifier specifies the key to be enlisted as the verified registry root,
// as well as the initial balance to be attributed during genesis.
func RootVerifier(key *wallet.Key, balance abi.TokenAmount) EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.verifiedRoot.key = key
		opts.verifiedRoot.initialBalance = balance
		return nil
	}
}

// Account sets up an account at genesis with the specified key and balance.
func Account(key *wallet.Key, balance abi.TokenAmount) EnsembleOpt {
	return func(opts *ensembleOpts) error {
		opts.accounts = append(opts.accounts, genesisAccount{
			key:            key,
			initialBalance: balance,
		})
		return nil
	}
}
