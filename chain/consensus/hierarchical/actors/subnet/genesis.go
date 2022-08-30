package subnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipfs/go-merkledag"
	"github.com/ipld/go-car"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/account"
	"github.com/filecoin-project/lotus/chain/actors/builtin/cron"
	init_ "github.com/filecoin-project/lotus/chain/actors/builtin/init"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/multisig"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	param "github.com/filecoin-project/lotus/chain/consensus/common/params"
	consensus "github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/node/bundle"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
)

const (
	networkVersion = network.Version16
)

func makeGenesisBlock(
	ctx context.Context,
	bs bstore.Blockstore,
	cns consensus.ConsensusType,
	chp abi.ChainEpoch,
	t genesis.Template,
) (*genesis2.GenesisBootstrap, error) {
	var err error
	st, _, err := MakeInitialStateTree(ctx, bs, cns, chp, t)
	if err != nil {
		return nil, xerrors.Errorf("make initial state tree failed: %w", err)
	}

	stateRoot, err := st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("flush state tree failed: %w", err)
	}

	store := adt.WrapStore(ctx, cbor.NewCborStore(bs))
	emptyRoot, err := adt0.MakeEmptyArray(store).Root()
	if err != nil {
		return nil, xerrors.Errorf("amt build failed: %w", err)
	}

	mm := &types.MsgMeta{
		BlsMessages:   emptyRoot,
		SecpkMessages: emptyRoot,
	}
	mmb, err := mm.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing msgmeta failed: %w", err)
	}
	if err := bs.Put(ctx, mmb); err != nil {
		return nil, xerrors.Errorf("putting msgmeta block to blockstore: %w", err)
	}

	log.Infof("Empty Genesis root: %s", emptyRoot)

	var proof []byte
	switch cns {
	case consensus.PoW:
		proof, err = param.GenesisWorkTarget.Bytes()
		if err != nil {
			return nil, err
		}
	case consensus.Delegated, consensus.Dummy, consensus.Mir:
		// TODO: We can't use randomness in genesis block
		// if want to make it deterministic. Consider using
		// a seed to for the ticket generation?
		// _, _ = rand.Read(tickBuf)
		proof = make([]byte, 32)
	}

	genesisTicket := &types.Ticket{
		VRFProof: proof,
	}

	b := &types.BlockHeader{
		Miner:                 system.Address,
		Ticket:                genesisTicket,
		Parents:               []cid.Cid{},
		Height:                0,
		ParentWeight:          types.NewInt(0),
		ParentStateRoot:       stateRoot,
		Messages:              mmb.Cid(),
		ParentMessageReceipts: emptyRoot,
		BLSAggregate:          nil,
		BlockSig:              nil,
		Timestamp:             t.Timestamp,
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

	if err := bs.Put(ctx, sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &genesis2.GenesisBootstrap{
		Genesis: b,
	}, nil
}

func makeTemplate(
	subnetID string,
	vreg, rem address.Address,
	seq uint64,
	b types.BigInt,
	a *genesis.Actor,
) (*genesis.Template, error) {
	accounts := make([]genesis.Actor, 0)
	if a != nil {
		accounts = append(accounts, *a)
	}
	return &genesis.Template{
		NetworkVersion: networkVersion,
		Accounts:       accounts,
		Miners:         nil,
		NetworkName:    subnetID,
		Timestamp:      seq,

		VerifregRootKey: genesis.Actor{
			Type:    genesis.TAccount,
			Balance: b,
			Meta:    json.RawMessage(`{"Owner":"` + vreg.String() + `"}`),
		},
		RemainderAccount: genesis.Actor{
			Type: genesis.TAccount,
			Meta: json.RawMessage(`{"Owner":"` + rem.String() + `"}`),
		},
	}, nil
}

func CreateGenesisFile(
	ctx context.Context,
	fileName string,
	alg consensus.ConsensusType,
	addr address.Address,
) error {
	memks := wallet.NewMemKeyStore()
	w, err := wallet.NewWallet(memks)
	if err != nil {
		return err
	}

	vreg, err := w.WalletNew(ctx, types.KTBLS)
	if err != nil {
		return err
	}
	rem, err := w.WalletNew(ctx, types.KTBLS)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	t := uint64(time.Now().Unix())

	chp := consensus.MinCheckpointPeriod(alg)

	if err := WriteGenesis(address.RootSubnet, alg, addr, vreg, rem, chp, t, f); err != nil {
		return xerrors.Errorf("write genesis car: %w", err)
	}

	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

func WriteGenesis(
	netName address.SubnetID,
	alg consensus.ConsensusType,
	miner, vreg, rem address.Address,
	chp abi.ChainEpoch,
	seq uint64, w io.Writer,
) error {
	bs := bstore.WrapIDStore(bstore.NewMemorySync())

	var b *genesis2.GenesisBootstrap
	ctx := context.TODO()
	switch alg {
	case consensus.Delegated:
		if miner == address.Undef {
			return xerrors.Errorf("no miner specified for delegated consensus")
		}
		a := genesis.Actor{
			Type:    genesis.TAccount,
			Balance: types.FromFil(0),
			Meta:    json.RawMessage(`{"Owner":"` + miner.String() + `"}`),
		}
		t, err := makeTemplate(netName.String(), vreg, rem, seq, types.FromFil(2), &a)
		if err != nil {
			return err
		}
		b, err = makeGenesisBlock(ctx, bs, alg, chp, *t)
		if err != nil {
			return xerrors.Errorf("failed make genesis block for %s: %w", consensus.ConsensusName(alg), err)
		}
	case consensus.PoW, consensus.Mir, consensus.Dummy:
		t, err := makeTemplate(netName.String(), vreg, rem, seq, types.FromFil(2), nil)
		if err != nil {
			return err
		}
		b, err = makeGenesisBlock(ctx, bs, alg, chp, *t)
		if err != nil {
			return xerrors.Errorf("failed make genesis block for %s: %w", consensus.ConsensusName(alg), err)
		}
	default:
		return xerrors.Errorf("consensus not supported")

	}
	e := offline.Exchange(bs)
	ds := merkledag.NewDAGService(blockservice.New(bs, e))

	if err := car.WriteCarWithWalker(ctx, ds, []cid.Cid{b.Genesis.Cid()}, w, gen.CarWalkFunc); err != nil {
		return xerrors.Errorf("write genesis car: %w", err)
	}
	return nil
}

/* FIXME: We can remove this after transitioning to FVM actors.
func MakeInitialStateTreeLegacy(
	ctx context.Context,
	bs bstore.Blockstore,
	cns consensus.ConsensusType,
	chp abi.ChainEpoch,
	template genesis.Template,
) (*state.StateTree, map[address.Address]address.Address, error) {
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

	stateTree, err := state.NewStateTree(cst, sv)
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
	if err := stateTree.SetActor(system.Address, sysact); err != nil {
		return nil, nil, xerrors.Errorf("set system actor: %w", err)
	}

	// Create empty power actor
	spact, err := SetupStoragePowerActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage power actor: %w", err)
	}
	if err := stateTree.SetActor(power.Address, spact); err != nil {
		return nil, nil, xerrors.Errorf("set storage power actor: %w", err)
	}

	// Create init actor

	idStart, initact, keyIDs, err := genesis2.SetupInitActor(ctx,
		bs,
		template.NetworkName,
		template.Accounts,
		template.VerifregRootKey,
		template.RemainderAccount,
		av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}
	if err := stateTree.SetActor(init_.Address, initact); err != nil {
		return nil, nil, xerrors.Errorf("set init actor: %w", err)
	}

	// Setup sca actor
	params := &sca.ConstructorParams{
		NetworkName:      template.NetworkName,
		Consensus:        cns,
		CheckpointPeriod: uint64(chp),
	}
	scaAct, err := SetupSCAActor(ctx, bs, params)
	if err != nil {
		return nil, nil, err
	}
	err = stateTree.SetActor(consensus.SubnetCoordActorAddr, scaAct)
	if err != nil {
		return nil, nil, xerrors.Errorf("set SCA actor: %w", err)
	}

	// Create empty market actor
	marketAct, err := SetupStorageMarketActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage market actor: %w", err)
	}
	if err := stateTree.SetActor(market.Address, marketAct); err != nil {
		return nil, nil, xerrors.Errorf("set storage market actor: %w", err)
	}
	// Setup reward actor
	// This is a modified reward actor to support the needs of hierarchical consensus
	// protocol.
	rewAct, err := SetupRewardActor(ctx, bs, big.Zero(), av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup reward actor: %w", err)
	}

	err = stateTree.SetActor(reward.Address, rewAct)
	if err != nil {
		return nil, nil, xerrors.Errorf("set reward actor: %w", err)
	}

	bAct, err := genesis2.MakeAccountActor(ctx, cst, av, builtin.BurntFundsActorAddr, big.Zero())
	if err != nil {
		return nil, nil, xerrors.Errorf("setup burnt funds actor state: %w", err)
	}
	if err := stateTree.SetActor(builtin.BurntFundsActorAddr, bAct); err != nil {
		return nil, nil, xerrors.Errorf("set burnt funds actor: %w", err)
	}

	// Create accounts
	for _, info := range template.Accounts {

		switch info.Type {
		case genesis.TAccount:
			if err := genesis2.CreateAccountActor(ctx, cst, stateTree, info, keyIDs, av); err != nil {
				return nil, nil, xerrors.Errorf("failed to create account actor: %w", err)
			}

		case genesis.TMultisig:

			ida, err := address.NewIDAddress(uint64(idStart))
			if err != nil {
				return nil, nil, err
			}
			idStart++

			if err := genesis2.CreateMultisigAccount(ctx, cst, stateTree, ida, info, keyIDs, av); err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, xerrors.New("unsupported account type")
		}

	}

	totalFilAllocated := big.Zero()

	err = stateTree.ForEach(func(addr address.Address, act *types.Actor) error {
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
		err = genesis2.CreateAccountActor(ctx, cst, stateTree, template.RemainderAccount, keyIDs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating remainder acct: %w", err)
		}

	case genesis.TMultisig:
		if err = genesis2.CreateMultisigAccount(ctx, cst, stateTree, builtin.ReserveAddress, template.RemainderAccount, keyIDs, av); err != nil {
			return nil, nil, xerrors.Errorf("failed to set up remainder: %w", err)
		}
	default:
		return nil, nil, xerrors.Errorf("unknown account type for remainder: %w", err)
	}

	return stateTree, keyIDs, nil
}
*/

func MakeInitialStateTree(ctx context.Context,
	bs bstore.Blockstore,
	cns consensus.ConsensusType,
	chp abi.ChainEpoch,
	template genesis.Template) (*state.StateTree, map[address.Address]address.Address, error) {
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

	if err := bundle.LoadBundles(ctx, bs, av); err != nil {
		return nil, nil, xerrors.Errorf("loading actors for genesis block: %w", err)
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

	// Setup reward
	// FIXME: Reward actor should be overwritten to be used with IPC.
	rewact, err := genesis2.SetupRewardActor(ctx, bs, big.Zero(), av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup reward actor: %w", err)
	}

	err = state.SetActor(reward.Address, rewact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set reward actor: %w", err)
	}

	// Setup cron
	cronact, err := genesis2.SetupCronActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup cron actor: %w", err)
	}
	if err := state.SetActor(cron.Address, cronact); err != nil {
		return nil, nil, xerrors.Errorf("set cron actor: %w", err)
	}

	// Setup sca actor
	params := &sca.ConstructorParams{
		NetworkName:      template.NetworkName,
		Consensus:        cns,
		CheckpointPeriod: uint64(chp),
	}
	scaAct, err := SetupSCAActor(ctx, bs, params)
	if err != nil {
		return nil, nil, err
	}

	err = state.SetActor(consensus.SubnetCoordActorAddr, scaAct)
	if err != nil {
		return nil, nil, xerrors.Errorf("set SCA actor: %w", err)
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

	bact, err := MakeAccountActor(ctx, cst, av, builtin.BurntFundsActorAddr, big.Zero())
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
			if err := CreateAccountActor(ctx, cst, state, info, keyIDs, av); err != nil {
				return nil, nil, xerrors.Errorf("failed to create account actor: %w", err)
			}

		case genesis.TMultisig:

			ida, err := address.NewIDAddress(uint64(idStart))
			if err != nil {
				return nil, nil, err
			}
			idStart++

			if err := CreateMultisigAccount(ctx, cst, state, ida, info, keyIDs, av); err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, xerrors.New("unsupported account type")
		}

	}

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

		vact, err := MakeAccountActor(ctx, cst, av, ainfo.Owner, template.VerifregRootKey.Balance)
		if err != nil {
			return nil, nil, xerrors.Errorf("setup verifreg rootkey account state: %w", err)
		}
		if err = state.SetActor(builtin.RootVerifierAddress, vact); err != nil {
			return nil, nil, xerrors.Errorf("set verifreg rootkey account actor: %w", err)
		}
	case genesis.TMultisig:
		if err = CreateMultisigAccount(ctx, cst, state, builtin.RootVerifierAddress, template.VerifregRootKey, keyIDs, av); err != nil {
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

	verifierAct, err := MakeAccountActor(ctx, cst, av, verifierAd, big.Zero())
	if err != nil {
		return nil, nil, xerrors.Errorf("setup first verifier state: %w", err)
	}

	if err = state.SetActor(verifierId, verifierAct); err != nil {
		return nil, nil, xerrors.Errorf("set first verifier actor: %w", err)
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
		err = CreateAccountActor(ctx, cst, state, template.RemainderAccount, keyIDs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating remainder acct: %w", err)
		}

	case genesis.TMultisig:
		if err = CreateMultisigAccount(ctx, cst, state, builtin.ReserveAddress, template.RemainderAccount, keyIDs, av); err != nil {
			return nil, nil, xerrors.Errorf("failed to set up remainder: %w", err)
		}
	default:
		return nil, nil, xerrors.Errorf("unknown account type for remainder: %w", err)
	}

	return state, keyIDs, nil
}

func MakeAccountActor(ctx context.Context, cst cbor.IpldStore, av actors.Version, addr address.Address, bal types.BigInt) (*types.Actor, error) {
	ast, err := account.MakeState(adt.WrapStore(ctx, cst), av, addr)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, ast.GetState())
	if err != nil {
		return nil, err
	}

	actcid, ok := actors.GetActorCodeID(av, actors.AccountKey)
	if !ok {
		return nil, xerrors.Errorf("failed to get account actor code ID for actors version %d", av)
	}

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: bal,
	}

	return act, nil
}

func CreateAccountActor(ctx context.Context, cst cbor.IpldStore, state *state.StateTree, info genesis.Actor, keyIDs map[address.Address]address.Address, av actors.Version) error {
	var ainfo genesis.AccountMeta
	if err := json.Unmarshal(info.Meta, &ainfo); err != nil {
		return xerrors.Errorf("unmarshaling account meta: %w", err)
	}

	aa, err := MakeAccountActor(ctx, cst, av, ainfo.Owner, info.Balance)
	if err != nil {
		return err
	}

	ida, ok := keyIDs[ainfo.Owner]
	if !ok {
		return fmt.Errorf("no registered ID for account actor: %s", ainfo.Owner)
	}

	err = state.SetActor(ida, aa)
	if err != nil {
		return xerrors.Errorf("setting account from actmap: %w", err)
	}
	return nil
}

func CreateMultisigAccount(ctx context.Context, cst cbor.IpldStore, state *state.StateTree, ida address.Address, info genesis.Actor, keyIDs map[address.Address]address.Address, av actors.Version) error {
	if info.Type != genesis.TMultisig {
		return fmt.Errorf("can only call CreateMultisigAccount with multisig Actor info")
	}
	var ainfo genesis.MultisigMeta
	if err := json.Unmarshal(info.Meta, &ainfo); err != nil {
		return xerrors.Errorf("unmarshaling account meta: %w", err)
	}

	var signers []address.Address

	for _, e := range ainfo.Signers {
		idAddress, ok := keyIDs[e]
		if !ok {
			return fmt.Errorf("no registered key ID for signer: %s", e)
		}

		// Check if actor already exists
		_, err := state.GetActor(e)
		if err == nil {
			signers = append(signers, idAddress)
			continue
		}

		aa, err := MakeAccountActor(ctx, cst, av, e, big.Zero())
		if err != nil {
			return err
		}

		if err = state.SetActor(idAddress, aa); err != nil {
			return xerrors.Errorf("setting account from actmap: %w", err)
		}
		signers = append(signers, idAddress)
	}

	mst, err := multisig.MakeState(adt.WrapStore(ctx, cst), av, signers, uint64(ainfo.Threshold), abi.ChainEpoch(ainfo.VestingStart), abi.ChainEpoch(ainfo.VestingDuration), info.Balance)
	if err != nil {
		return err
	}

	statecid, err := cst.Put(ctx, mst.GetState())
	if err != nil {
		return err
	}

	actcid, ok := actors.GetActorCodeID(av, actors.MultisigKey)
	if !ok {
		return xerrors.Errorf("failed to get multisig code ID for actors version %d", av)
	}

	err = state.SetActor(ida, &types.Actor{
		Code:    actcid,
		Balance: info.Balance,
		Head:    statecid,
	})
	if err != nil {
		return xerrors.Errorf("setting account from actmap: %w", err)
	}

	return nil
}

func SetupSCAActor(ctx context.Context, bs bstore.Blockstore, params *sca.ConstructorParams) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	st, err := sca.ConstructSCAState(adt.WrapStore(ctx, cst), params)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, st)
	if err != nil {
		return nil, err
	}

	act := &types.Actor{
		Code:    actor.SubnetCoordActorCodeID,
		Balance: big.Zero(),
		Head:    statecid,
	}

	return act, nil
}

/* FIXME: Previous setup functions used for the legacy VM. We may need to adapt the ones for FVM to
do the same.
func SetupRewardActor(ctx context.Context, bs bstore.Blockstore, qaPower big.Int, av actors.Version) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	rst := reward.ConstructState(qaPower)

	statecid, err := cst.Put(ctx, rst)
	if err != nil {
		return nil, err
	}

	// NOTE: For now, everything in the reward actor is the same except the code,
	// where we included an additional method to fund accounts. This may change
	// in the future when we design specific reward system for subnets.
	act := &types.Actor{
		Code: actor.RewardActorCodeID,
		// NOTE: This sets up the initial balance of the reward actor.
		Balance: types.BigInt{Int: build.InitialRewardBalance},
		Head:    statecid,
	}

	return act, nil
}

func SetupStorageMarketActor(ctx context.Context, bs bstore.Blockstore, av actors.Version) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	mst, err := market.MakeState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), av)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, mst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, ok := actors.GetActorCodeID(av, actors.MarketKey)
	if !ok {
		return nil, xerrors.Errorf("failed to get market actor code ID for actors version %d", av)
	}

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: big.Zero(),
	}

	return act, nil
}

func SetupStoragePowerActor(ctx context.Context, bs bstore.Blockstore, av actors.Version) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	mst, err := market.MakeState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), av)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, mst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, ok := actors.GetActorCodeID(av, actors.MarketKey)
	if !ok {
		return nil, xerrors.Errorf("failed to get market actor code ID for actors version %d", av)
	}

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: big.Zero(),
	}

	return act, nil
}
*/
