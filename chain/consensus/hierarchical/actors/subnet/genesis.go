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
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	init_ "github.com/filecoin-project/lotus/chain/actors/builtin/init"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	param "github.com/filecoin-project/lotus/chain/consensus/common/params"
	consensus "github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/genesis"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
)

const (
	networkVersion = network.Version15
)

func makeGenesisBlock(
	ctx context.Context,
	cns consensus.ConsensusType,
	bs bstore.Blockstore,
	t genesis.Template,
	e abi.ChainEpoch,
) (*genesis2.GenesisBootstrap, error) {
	var err error
	st, _, err := MakeInitialStateTree(ctx, bs, t, e)
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
		CrossMessages: emptyRoot,
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
	case consensus.Delegated:
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
	cns consensus.ConsensusType,
	addr address.Address,
	checkPeriod abi.ChainEpoch,
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

	if err := WriteGenesis(address.RootSubnet, cns, addr, vreg, rem, checkPeriod, t, f); err != nil {
		return xerrors.Errorf("write genesis car: %w", err)
	}

	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

func WriteGenesis(
	netName address.SubnetID,
	cns consensus.ConsensusType,
	miner, vreg, rem address.Address,
	checkPeriod abi.ChainEpoch,
	seq uint64, w io.Writer,
) error {
	bs := bstore.WrapIDStore(bstore.NewMemorySync())

	var b *genesis2.GenesisBootstrap
	ctx := context.TODO()
	switch cns {
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
		b, err = makeGenesisBlock(ctx, cns, bs, *t, checkPeriod)
		if err != nil {
			return xerrors.Errorf("failed make genesis block for %s: %w", consensus.ConsensusName(cns), err)
		}
	case consensus.PoW, consensus.Tendermint, consensus.Mir, consensus.Dummy:
		t, err := makeTemplate(netName.String(), vreg, rem, seq, types.FromFil(2), nil)
		if err != nil {
			return err
		}
		b, err = makeGenesisBlock(ctx, cns, bs, *t, checkPeriod)
		if err != nil {
			return xerrors.Errorf("failed make genesis block for %s: %w", consensus.ConsensusName(cns), err)
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

func MakeInitialStateTree(
	ctx context.Context,
	bs bstore.Blockstore,
	template genesis.Template,
	checkPeriod abi.ChainEpoch,
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

	idStart, initact, keyIDs, err := genesis2.SetupInitActor(ctx, bs, template.NetworkName, template.Accounts, template.VerifregRootKey, template.RemainderAccount, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}
	if err := stateTree.SetActor(init_.Address, initact); err != nil {
		return nil, nil, xerrors.Errorf("set init actor: %w", err)
	}

	// Setup sca actor
	params := &sca.ConstructorParams{
		NetworkName:      template.NetworkName,
		CheckpointPeriod: uint64(checkPeriod),
	}
	scaact, err := SetupSCAActor(ctx, bs, params)
	if err != nil {
		return nil, nil, err
	}
	err = stateTree.SetActor(consensus.SubnetCoordActorAddr, scaact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set SCA actor: %w", err)
	}

	// Create empty market actor
	marketact, err := SetupStorageMarketActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage market actor: %w", err)
	}
	if err := stateTree.SetActor(market.Address, marketact); err != nil {
		return nil, nil, xerrors.Errorf("set storage market actor: %w", err)
	}
	// Setup reward actor
	// This is a modified reward actor to support the needs of hierarchical consensus
	// protocol.
	rewact, err := SetupRewardActor(ctx, bs, big.Zero(), av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup reward actor: %w", err)
	}

	err = stateTree.SetActor(reward.RewardActorAddr, rewact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set reward actor: %w", err)
	}

	bact, err := genesis2.MakeAccountActor(ctx, cst, av, builtin.BurntFundsActorAddr, big.Zero())
	if err != nil {
		return nil, nil, xerrors.Errorf("setup burnt funds actor state: %w", err)
	}
	if err := stateTree.SetActor(builtin.BurntFundsActorAddr, bact); err != nil {
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

	actcid, err := builtin.GetMarketActorCodeID(av)
	if err != nil {
		return nil, err
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
	pst, err := power.MakeState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), av)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, pst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, err := builtin.GetPowerActorCodeID(av)
	if err != nil {
		return nil, err
	}

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: big.Zero(),
	}

	return act, nil
}
