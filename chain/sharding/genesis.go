package sharding

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	init_ "github.com/filecoin-project/lotus/chain/actors/builtin/init"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	shardactor "github.com/filecoin-project/lotus/chain/sharding/actors"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

// TODO: Extract all of these function to consensus specific code that can be called
// from here for shards and from the consensus-specific cmds to start mining in root chain.
func MakeDelegatedGenesisBlock(ctx context.Context, j journal.Journal, bs bstore.Blockstore, sys vm.SyscallBuilder, template genesis.Template) (*genesis2.GenesisBootstrap, error) {
	if j == nil {
		j = journal.NilJournal()
	}
	st, _, err := MakeInitialStateTree(ctx, bs, template)
	if err != nil {
		return nil, xerrors.Errorf("make initial state tree failed: %w", err)
	}

	stateroot, err := st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("flush state tree failed: %w", err)
	}

	// temp chainstore
	//cs := store.NewChainStore(bs, bs, datastore.NewMapDatastore(), j)

	/*	// Verify PreSealed Data
		stateroot, err = VerifyPreSealedData(ctx, cs, sys, stateroot, template, keyIDs, template.NetworkVersion)
		if err != nil {
			return nil, xerrors.Errorf("failed to verify presealed data: %w", err)
		}

		stateroot, err = SetupStorageMiners(ctx, cs, sys, stateroot, template.Miners, template.NetworkVersion)
		if err != nil {
			return nil, xerrors.Errorf("setup miners failed: %w", err)
		}*/

	store := adt.WrapStore(ctx, cbor.NewCborStore(bs))
	emptyroot, err := adt0.MakeEmptyArray(store).Root()
	if err != nil {
		return nil, xerrors.Errorf("amt build failed: %w", err)
	}

	mm := &types.MsgMeta{
		BlsMessages:   emptyroot,
		SecpkMessages: emptyroot,
	}
	mmb, err := mm.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing msgmeta failed: %w", err)
	}
	if err := bs.Put(mmb); err != nil {
		return nil, xerrors.Errorf("putting msgmeta block to blockstore: %w", err)
	}

	tickBuf := make([]byte, 32)
	_, _ = rand.Read(tickBuf)
	genesisticket := &types.Ticket{
		VRFProof: tickBuf,
	}

	b := &types.BlockHeader{
		Miner:                 system.Address,
		Ticket:                genesisticket,
		Parents:               []cid.Cid{},
		Height:                0,
		ParentWeight:          types.NewInt(0),
		ParentStateRoot:       stateroot,
		Messages:              mmb.Cid(),
		ParentMessageReceipts: emptyroot,
		BLSAggregate:          nil,
		BlockSig:              nil,
		Timestamp:             template.Timestamp,
		ElectionProof:         new(types.ElectionProof),
		BeaconEntries: []types.BeaconEntry{
			{
				Round: 0,
				Data:  make([]byte, 32),
			},
		},
		ParentBaseFee: abi.NewTokenAmount(build.InitialBaseFee),
	}

	sb, err := b.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing block header failed: %w", err)
	}

	if err := bs.Put(sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &genesis2.GenesisBootstrap{
		Genesis: b,
	}, nil
}

func MakeInitialStateTree(ctx context.Context, bs bstore.Blockstore, template genesis.Template) (*state.StateTree, map[address.Address]address.Address, error) {
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

	err = state.SetActor(shardactor.ShardActorAddr, shardact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set reward actor: %w", err)
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
	st, err := shardactor.ConstructShardState(adt.WrapStore(ctx, cst), networkName)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, st)
	if err != nil {
		return nil, err
	}

	act := &types.Actor{
		Code:    shardactor.ShardActorCodeID,
		Balance: big.Zero(),
		Head:    statecid,
	}

	return act, nil
}

func delegatedGenTemplate(shardID string) (*genesis.Template, error) {
	memks := wallet.NewMemKeyStore()
	w, err := wallet.NewWallet(memks)
	if err != nil {
		return nil, err
	}
	vreg, err := w.WalletNew(context.TODO(), types.KTBLS)
	if err != nil {
		return nil, err
	}
	rem, err := w.WalletNew(context.TODO(), types.KTBLS)
	if err != nil {
		return nil, err
	}

	// TODO: Assign the miner set in the shard for delegated consensus.
	miner := "t1r6o5d5s5zjzqhqs4nh3ac7k5e7dhg5oqa7v64oy"
	return &genesis.Template{
		NetworkVersion: network.Version13,
		// TODO: In delegated consensus we need to assign this account to a
		// valid miner. For testing purposes we'll add no account here (although
		// we may need to add something for mining.
		// Accounts: []genesis.Actor{},

		Accounts: []genesis.Actor{{
			Type:    genesis.TAccount,
			Balance: types.FromFil(2),
			// TODO: Here we are hard-cording an actor for the genesis for testing purposes.
			// This needs to change to gather the info from shard actor call.
			// Meta: json.RawMessage(`{"Owner":"` + miner.String() + `"}`),
			Meta: json.RawMessage(`{"Owner":"` + miner + `"}`),
		}},
		Miners: nil,
		// TODO: This needs to be the ID of the shard with shardID
		NetworkName: shardID,
		Timestamp:   uint64(time.Now().Unix()),

		VerifregRootKey: genesis.Actor{
			Type:    genesis.TAccount,
			Balance: types.FromFil(2),
			Meta:    json.RawMessage(`{"Owner":"` + vreg.String() + `"}`), // correct??
		},
		RemainderAccount: genesis.Actor{
			Type: genesis.TAccount,
			Meta: json.RawMessage(`{"Owner":"` + rem.String() + `"}`), // correct??
		},
	}, nil
}
