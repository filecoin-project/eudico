package shard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/cron"
	init_ "github.com/filecoin-project/lotus/chain/actors/builtin/init"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/ipfs/go-blockservice"
	cid "github.com/ipfs/go-cid"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipfs/go-merkledag"
	"github.com/ipld/go-car"
	xerrors "golang.org/x/xerrors"
)

const (
	networkVersion = network.Version14
)

func WriteGenesis(netName string, consensus ConsensusType, repoPath string, miner, vreg, rem address.Address, seq uint64, w io.Writer) error {
	bs := bstore.WrapIDStore(bstore.NewMemorySync())

	var b *genesis2.GenesisBootstrap
	switch consensus {
	case Delegated:
		if miner == address.Undef {
			return xerrors.Errorf("no miner specified for delegated consensus")
		}
		template, err := delegatedGenTemplate(netName, miner, vreg, rem, seq)
		if err != nil {
			return err
		}
		b, err = makeDelegatedGenesisBlock(context.TODO(), bs, *template)
		if err != nil {
			return xerrors.Errorf("error making genesis delegated block: %w", err)
		}
	case PoW:
		template, err := powGenTemplate(netName, vreg, rem, seq)
		if err != nil {
			return err
		}
		b, err = makePoWGenesisBlock(context.TODO(), bs, *template)
		if err != nil {
			return xerrors.Errorf("error making genesis filcns block: %w", err)
		}
	case FilCns:
		if miner == address.Undef {
			return xerrors.Errorf("no miner specified for filecoin consensus")
		}
		template, err := filCnsGenTemplate(netName, miner, vreg, rem, seq)
		if err != nil {
			return err
		}
		b, err = makeFilCnsGenesisBlock(context.TODO(), bs, *template, miner, repoPath)
		if err != nil {
			return xerrors.Errorf("error making genesis filcns block: %w", err)
		}
	}

	offl := offline.Exchange(bs)
	blkserv := blockservice.New(bs, offl)
	dserv := merkledag.NewDAGService(blkserv)

	if err := car.WriteCarWithWalker(context.TODO(), dserv, []cid.Cid{b.Genesis.Cid()}, w, gen.CarWalkFunc); err != nil {
		return xerrors.Errorf("write genesis car: %w", err)
	}
	return nil
}

func MakeInitialStateTree(ctx context.Context, consensus ConsensusType, bs bstore.Blockstore, template genesis.Template) (*state.StateTree, map[address.Address]address.Address, error) {
	// Create empty state tree

	cst := cbor.NewCborStore(bs)
	_, err := cst.Put(context.TODO(), []struct{}{})
	if err != nil {
		return nil, nil, xerrors.Errorf("putting empty object: %w", err)
	}

	sv, err := state.VersionForNetwork(template.NetworkVersion)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting state tree version: %w", err)
	}

	state, err := state.NewStateTree(cst, sv)
	if err != nil {
		return nil, nil, xerrors.Errorf("making new state tree: %w", err)
	}

	av, err := actors.VersionForNetwork(template.NetworkVersion)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting network version: %w", err)
	}

	// Create system actor

	sysact, err := genesis2.SetupSystemActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup system actor: %w", err)
	}
	if err := state.SetActor(system.Address, sysact); err != nil {
		return nil, nil, xerrors.Errorf("set system actor: %w", err)
	}

	// Create init actor

	idStart, initact, keyIDs, err := genesis2.SetupInitActor(ctx, bs, template.NetworkName, template.Accounts, template.VerifregRootKey, template.RemainderAccount, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}
	if err := state.SetActor(init_.Address, initact); err != nil {
		return nil, nil, xerrors.Errorf("set init actor: %w", err)
	}

	// Setup shard actor
	shardact, err := SetupShardActor(ctx, bs, template.NetworkName)
	if err != nil {
		return nil, nil, err
	}
	err = state.SetActor(ShardActorAddr, shardact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set shard actor: %w", err)
	}

	// Setup reward
	// RewardActor's state is overwritten by SetupStorageMiners, but needs to exist for miner creation messages
	rewact, err := genesis2.SetupRewardActor(ctx, bs, big.Zero(), av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup reward actor: %w", err)
	}

	err = state.SetActor(reward.Address, rewact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set reward actor: %w", err)
	}

	// NOTE: Cron, power and verified actors only instantiated for Filecoin consensus (at least for now)
	if consensus == FilCns {
		// Setup cron
		cronact, err := genesis2.SetupCronActor(ctx, bs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("setup cron actor: %w", err)
		}
		if err := state.SetActor(cron.Address, cronact); err != nil {
			return nil, nil, xerrors.Errorf("set cron actor: %w", err)
		}

		// Create empty power actor
		spact, err := genesis2.SetupStoragePowerActor(ctx, bs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("setup storage power actor: %w", err)
		}
		if err := state.SetActor(power.Address, spact); err != nil {
			return nil, nil, xerrors.Errorf("set storage power actor: %w", err)
		}

		// Create empty market actor
		marketact, err := genesis2.SetupStorageMarketActor(ctx, bs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("setup storage market actor: %w", err)
		}
		if err := state.SetActor(market.Address, marketact); err != nil {
			return nil, nil, xerrors.Errorf("set storage market actor: %w", err)
		}

		// Create verified registry
		verifact, err := genesis2.SetupVerifiedRegistryActor(ctx, bs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("setup verified registry market actor: %w", err)
		}

		if err := state.SetActor(verifreg.Address, verifact); err != nil {
			return nil, nil, xerrors.Errorf("set verified registry actor: %w", err)
		}
	}

	bact, err := genesis2.MakeAccountActor(ctx, cst, av, builtin.BurntFundsActorAddr, big.Zero())
	if err != nil {
		return nil, nil, xerrors.Errorf("setup burnt funds actor state: %w", err)
	}
	if err := state.SetActor(builtin.BurntFundsActorAddr, bact); err != nil {
		return nil, nil, xerrors.Errorf("set burnt funds actor: %w", err)
	}

	// Create accounts
	for _, info := range template.Accounts {

		switch info.Type {
		case genesis.TAccount:
			if err := genesis2.CreateAccountActor(ctx, cst, state, info, keyIDs, av); err != nil {
				return nil, nil, xerrors.Errorf("failed to create account actor: %w", err)
			}

		case genesis.TMultisig:

			ida, err := address.NewIDAddress(uint64(idStart))
			if err != nil {
				return nil, nil, err
			}
			idStart++

			if err := genesis2.CreateMultisigAccount(ctx, cst, state, ida, info, keyIDs, av); err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, xerrors.New("unsupported account type")
		}

	}

	// This is only needed if we set up the verified actor.
	if consensus == FilCns {
		switch template.VerifregRootKey.Type {
		case genesis.TAccount:
			var ainfo genesis.AccountMeta
			if err := json.Unmarshal(template.VerifregRootKey.Meta, &ainfo); err != nil {
				return nil, nil, xerrors.Errorf("unmarshaling account meta: %w", err)
			}

			_, ok := keyIDs[ainfo.Owner]
			if ok {
				return nil, nil, fmt.Errorf("rootkey account has already been declared, cannot be assigned 80: %s", ainfo.Owner)
			}

			vact, err := genesis2.MakeAccountActor(ctx, cst, av, ainfo.Owner, template.VerifregRootKey.Balance)
			if err != nil {
				return nil, nil, xerrors.Errorf("setup verifreg rootkey account state: %w", err)
			}
			if err = state.SetActor(builtin.RootVerifierAddress, vact); err != nil {
				return nil, nil, xerrors.Errorf("set verifreg rootkey account actor: %w", err)
			}
		case genesis.TMultisig:
			if err = genesis2.CreateMultisigAccount(ctx, cst, state, builtin.RootVerifierAddress, template.VerifregRootKey, keyIDs, av); err != nil {
				return nil, nil, xerrors.Errorf("failed to set up verified registry signer: %w", err)
			}
		default:
			return nil, nil, xerrors.Errorf("unknown account type for verifreg rootkey: %w", err)
		}

		// Setup the first verifier as ID-address 81
		// TODO: remove this
		skBytes, err := sigs.Generate(crypto.SigTypeBLS)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating random verifier secret key: %w", err)
		}

		verifierPk, err := sigs.ToPublic(crypto.SigTypeBLS, skBytes)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating random verifier public key: %w", err)
		}

		verifierAd, err := address.NewBLSAddress(verifierPk)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating random verifier address: %w", err)
		}

		verifierId, err := address.NewIDAddress(81)
		if err != nil {
			return nil, nil, err
		}

		verifierAct, err := genesis2.MakeAccountActor(ctx, cst, av, verifierAd, big.Zero())
		if err != nil {
			return nil, nil, xerrors.Errorf("setup first verifier state: %w", err)
		}

		if err = state.SetActor(verifierId, verifierAct); err != nil {
			return nil, nil, xerrors.Errorf("set first verifier actor: %w", err)
		}
	}

	totalFilAllocated := big.Zero()

	err = state.ForEach(func(addr address.Address, act *types.Actor) error {
		if act.Balance.Nil() {
			panic(fmt.Sprintf("actor %s (%s) has nil balance", addr, builtin.ActorNameByCode(act.Code)))
		}
		totalFilAllocated = big.Add(totalFilAllocated, act.Balance)
		return nil
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("summing account balances in state tree: %w", err)
	}

	totalFil := big.Mul(big.NewInt(int64(build.FilBase)), big.NewInt(int64(build.FilecoinPrecision)))
	remainingFil := big.Sub(totalFil, totalFilAllocated)
	if remainingFil.Sign() < 0 {
		return nil, nil, xerrors.Errorf("somehow overallocated filecoin (allocated = %s)", types.FIL(totalFilAllocated))
	}

	template.RemainderAccount.Balance = remainingFil

	switch template.RemainderAccount.Type {
	case genesis.TAccount:
		var ainfo genesis.AccountMeta
		if err := json.Unmarshal(template.RemainderAccount.Meta, &ainfo); err != nil {
			return nil, nil, xerrors.Errorf("unmarshaling account meta: %w", err)
		}

		_, ok := keyIDs[ainfo.Owner]
		if ok {
			return nil, nil, fmt.Errorf("remainder account has already been declared, cannot be assigned 90: %s", ainfo.Owner)
		}

		keyIDs[ainfo.Owner] = builtin.ReserveAddress
		err = genesis2.CreateAccountActor(ctx, cst, state, template.RemainderAccount, keyIDs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating remainder acct: %w", err)
		}

	case genesis.TMultisig:
		if err = genesis2.CreateMultisigAccount(ctx, cst, state, builtin.ReserveAddress, template.RemainderAccount, keyIDs, av); err != nil {
			return nil, nil, xerrors.Errorf("failed to set up remainder: %w", err)
		}
	default:
		return nil, nil, xerrors.Errorf("unknown account type for remainder: %w", err)
	}

	return state, keyIDs, nil
}

func SetupShardActor(ctx context.Context, bs bstore.Blockstore, networkName string) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	st, err := ConstructShardState(adt.WrapStore(ctx, cst), networkName)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, st)
	if err != nil {
		return nil, err
	}

	act := &types.Actor{
		Code:    actor.ShardActorCodeID,
		Balance: big.Zero(),
		Head:    statecid,
	}

	return act, nil
}
