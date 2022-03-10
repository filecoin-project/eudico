package exec_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	xerrors "golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	actbuiltin "github.com/filecoin-project/lotus/chain/actors/builtin"
	init_ "github.com/filecoin-project/lotus/chain/actors/builtin/init"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	"github.com/filecoin-project/lotus/chain/beacon"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	atom "github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic/exec"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	_ "github.com/filecoin-project/lotus/lib/sigs/bls"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
	proof5 "github.com/filecoin-project/specs-actors/v5/actors/runtime/proof"
	proof7 "github.com/filecoin-project/specs-actors/v7/actors/runtime/proof"
	cbor "github.com/ipfs/go-ipld-cbor"
)

var cidUndef, _ = abi.CidBuilder.Sum([]byte("test"))

var ReplaceActorAddr = func() address.Address {
	a, err := address.NewIDAddress(999)
	if err != nil {
		panic(err)
	}
	return a
}()

func TestComputeState(t *testing.T) {
	ctx := context.TODO()

	// cg, err := gen.NewGenerator()
	cg, err := NewGenerator(t)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Execute messages atomically from cg.Banker()")
	target, err := address.NewIDAddress(101)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []types.Message{}
	enc, err := actors.SerializeParams(&replace.OwnParams{Seed: "testSeed"})
	if err != nil {
		t.Fatal(err)
	}
	msgs = append(msgs, types.Message{
		From:   cg.Banker(),
		To:     ReplaceActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: replace.MethodOwn,
		Params: enc,
	})
	enc, err = actors.SerializeParams(&replace.ReplaceParams{Addr: target})
	if err != nil {
		t.Fatal(err)
	}
	msgs = append(msgs, types.Message{
		From:   cg.Banker(),
		To:     ReplaceActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: replace.MethodReplace,
		Params: enc,
	})
	own1 := &replace.Owners{M: map[string]cid.Cid{target.String(): cidUndef}}
	var st replace.ReplaceState
	ts := cg.StateManager().ChainStore().GetHeaviestTipSet()
	err = exec.ComputeAtomicOutput(ctx, cg.StateManager(), ts, msgs[0].To, &st, []atom.LockableState{own1}, msgs)
	require.NoError(t, err)
	owners, err := st.UnwrapOwners()
	require.NoError(t, err)

	// predicting the address here... may break if other assumptions change
	// FIXME: we could resolve the address to have the right ID always
	// (but feeling lazy now, want to test fast)
	taddr, err := address.NewIDAddress(100)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the atomic replace happened.
	c, ok := owners.M[taddr.String()]
	require.True(t, ok)
	require.Equal(t, c, cidUndef)
	c, ok = owners.M[target.String()]
	require.True(t, ok)
	exp, _ := abi.CidBuilder.Sum([]byte("testSeed"))
	require.Equal(t, c, exp)

	t.Log("Execute messages atomically from target's view")
	// Compute the opposite and compare output CID
	msgs = []types.Message{}
	enc, err = actors.SerializeParams(&replace.OwnParams{Seed: "test"})
	if err != nil {
		t.Fatal(err)
	}
	msgs = append(msgs, types.Message{
		From:   target,
		To:     ReplaceActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: replace.MethodOwn,
		Params: enc,
	})
	enc, err = actors.SerializeParams(&replace.ReplaceParams{Addr: taddr})
	if err != nil {
		t.Fatal(err)
	}
	msgs = append(msgs, types.Message{
		From:   target,
		To:     ReplaceActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: replace.MethodReplace,
		Params: enc,
	})
	own1 = &replace.Owners{M: map[string]cid.Cid{taddr.String(): exp}}
	var st2 replace.ReplaceState
	ts = cg.StateManager().ChainStore().GetHeaviestTipSet()
	err = exec.ComputeAtomicOutput(ctx, cg.StateManager(), ts, msgs[0].To, &st2, []atom.LockableState{own1}, msgs)
	require.NoError(t, err)

	// Check that the atomic replace happened.
	owners, err = st.UnwrapOwners()
	require.NoError(t, err)
	c, ok = owners.M[taddr.String()]
	require.True(t, ok)
	require.Equal(t, c, cidUndef)
	c, ok = owners.M[target.String()]
	require.True(t, ok)
	exp, _ = abi.CidBuilder.Sum([]byte("testSeed"))
	require.Equal(t, c, exp)

	t.Log("Comparing outputs of independent off-chain execution through CID")
	// Compare output cids.
	oc1, err := st.Owners.Cid()
	require.NoError(t, err)
	oc2, err := st2.Owners.Cid()
	require.NoError(t, err)
	require.Equal(t, oc1, oc2)

}

var rootkeyMultisig = genesis.MultisigMeta{
	Signers:         []address.Address{remAccTestKey},
	Threshold:       1,
	VestingDuration: 0,
	VestingStart:    0,
}

var DefaultVerifregRootkeyActor = genesis.Actor{
	Type:    genesis.TMultisig,
	Balance: big.NewInt(0),
	Meta:    rootkeyMultisig.ActorMeta(),
}

var remAccTestKey, _ = address.NewFromString("t1ceb34gnsc6qk5dt6n7xg6ycwzasjhbxm3iylkiy")
var remAccMeta = genesis.MultisigMeta{
	Signers:   []address.Address{remAccTestKey},
	Threshold: 1,
}

var DefaultRemainderAccountActor = genesis.Actor{
	Type:    genesis.TMultisig,
	Balance: big.NewInt(0),
	Meta:    remAccMeta.ActorMeta(),
}

type ChainGen struct {
	msgsPerBlock int

	bs blockstore.Blockstore

	cs *store.ChainStore

	beacon beacon.Schedule

	sm *stmgr.StateManager

	genesis   *types.BlockHeader
	CurTipset *store.FullTipSet

	w *wallet.LocalWallet

	Miners    []address.Address
	receivers []address.Address
	// a SecP address
	banker address.Address

	r  repo.Repo
	lr repo.LockedRepo
}

const msgsPerBlock = 20

func NewGenerator(t *testing.T) (*ChainGen, error) {
	j := journal.NilJournal()

	mr := repo.NewMemory(nil)
	lr, err := mr.Lock(repo.StorageMiner)
	if err != nil {
		return nil, xerrors.Errorf("taking mem-repo lock failed: %w", err)
	}

	ds, err := lr.Datastore(context.TODO(), "/metadata")
	if err != nil {
		return nil, xerrors.Errorf("failed to get metadata datastore: %w", err)
	}

	bs, err := lr.Blockstore(context.TODO(), repo.UniversalBlockstore)
	if err != nil {
		return nil, err
	}

	defer func() {
		if c, ok := bs.(io.Closer); ok {
			if err := c.Close(); err != nil {
				fmt.Printf("WARN: failed to close blockstore: %s\n", err)
			}
		}
	}()

	ks, err := lr.KeyStore()
	if err != nil {
		return nil, xerrors.Errorf("getting repo keystore failed: %w", err)
	}

	w, err := wallet.NewWallet(ks)
	if err != nil {
		return nil, xerrors.Errorf("creating memrepo wallet failed: %w", err)
	}

	banker, err := w.WalletNew(context.Background(), types.KTSecp256k1)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate banker key: %w", err)
	}

	receievers := make([]address.Address, msgsPerBlock)
	for r := range receievers {
		receievers[r], err = w.WalletNew(context.Background(), types.KTBLS)
		if err != nil {
			return nil, xerrors.Errorf("failed to generate receiver key: %w", err)
		}
	}

	template := genesis.Template{
		NetworkVersion: network.Version15,
		Accounts: []genesis.Actor{
			{
				Type:    genesis.TAccount,
				Balance: types.FromFil(50000),
				Meta:    (&genesis.AccountMeta{Owner: banker}).ActorMeta(),
			},
		},
		VerifregRootKey:  DefaultVerifregRootkeyActor,
		RemainderAccount: DefaultRemainderAccountActor,
		NetworkName:      uuid.New().String(),
		Timestamp:        uint64(build.Clock.Now().Add(-500 * time.Duration(build.BlockDelaySecs) * time.Second).Unix()),
	}

	genb, err := makeDelegatedGenesisBlock(context.TODO(), bs, template, abi.ChainEpoch(100))
	require.NoError(t, err)
	weight := func(ctx context.Context, stateBs blockstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
		if ts == nil {
			return types.NewInt(0), nil
		}

		return big.NewInt(int64(ts.Height() + 1)), nil
	}
	cs := store.NewChainStore(bs, bs, ds, weight, j)

	genfb := &types.FullBlock{Header: genb.Genesis}
	gents := store.NewFullTipSet([]*types.FullBlock{genfb})

	if err := cs.SetGenesis(context.TODO(), genb.Genesis); err != nil {
		return nil, xerrors.Errorf("set genesis failed: %w", err)
	}

	miners := []address.Address{}

	beac := beacon.Schedule{{Start: 0, Beacon: beacon.NewMockBeacon(time.Second)}}

	sys := vm.Syscalls(&genFakeVerifier{})
	sm, err := stmgr.NewStateManager(cs, common.RootTipSetExecutor(), nil, sys, common.DefaultUpgradeSchedule(), beac)
	if err != nil {
		return nil, xerrors.Errorf("initing stmgr: %w", err)
	}

	gen := &ChainGen{
		bs:           bs,
		cs:           cs,
		sm:           sm,
		msgsPerBlock: msgsPerBlock,
		genesis:      genb.Genesis,
		beacon:       beac,
		w:            w,

		Miners:    miners,
		banker:    banker,
		receivers: receievers,

		CurTipset: gents,

		r:  mr,
		lr: lr,
	}

	return gen, nil
}

func makeDelegatedGenesisBlock(ctx context.Context, bs blockstore.Blockstore, template genesis.Template, checkPeriod abi.ChainEpoch) (*genesis2.GenesisBootstrap, error) {
	st, _, err := MakeInitialStateTree(ctx, bs, template, checkPeriod)
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
		CrossMessages: emptyroot,
	}
	mmb, err := mm.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing msgmeta failed: %w", err)
	}
	if err := bs.Put(ctx, mmb); err != nil {
		return nil, xerrors.Errorf("putting msgmeta block to blockstore: %w", err)
	}

	tickBuf := make([]byte, 32)
	// TODO: We can't use randomness in genesis block
	// if want to make it deterministic. Consider using
	// a seed to for the ticket generation?
	// _, _ = rand.Read(tickBuf)
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

	if err := bs.Put(ctx, sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &genesis2.GenesisBootstrap{
		Genesis: b,
	}, nil
}

func MakeInitialStateTree(ctx context.Context, bs blockstore.Blockstore, template genesis.Template, checkPeriod abi.ChainEpoch) (*state.StateTree, map[address.Address]address.Address, error) {
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

	// Create empty power actor
	spact, err := SetupStoragePowerActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage power actor: %w", err)
	}
	if err := state.SetActor(power.Address, spact); err != nil {
		return nil, nil, xerrors.Errorf("set storage power actor: %w", err)
	}

	// Create init actor

	idStart, initact, keyIDs, err := genesis2.SetupInitActor(ctx, bs, template.NetworkName, template.Accounts, template.VerifregRootKey, template.RemainderAccount, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}
	if err := state.SetActor(init_.Address, initact); err != nil {
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
	err = state.SetActor(hierarchical.SubnetCoordActorAddr, scaact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set SCA actor: %w", err)
	}

	// NOTE: Setting a replace actor at the beginning so we don't have
	// to initialize it for testing.
	ract, err := SetupReplaceActor(ctx, bs)
	if err != nil {
		return nil, nil, err
	}
	err = state.SetActor(ReplaceActorAddr, ract)
	if err != nil {
		return nil, nil, xerrors.Errorf("set Replace actor: %w", err)
	}

	// Create empty market actor
	marketact, err := SetupStorageMarketActor(ctx, bs, av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage market actor: %w", err)
	}
	if err := state.SetActor(market.Address, marketact); err != nil {
		return nil, nil, xerrors.Errorf("set storage market actor: %w", err)
	}
	// Setup reward actor
	// This is a modified reward actor to support the needs of hierarchical consensus
	// protocol.
	rewact, err := SetupRewardActor(ctx, bs, big.Zero(), av)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup reward actor: %w", err)
	}

	err = state.SetActor(reward.RewardActorAddr, rewact)
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

		keyIDs[ainfo.Owner] = actbuiltin.ReserveAddress
		err = genesis2.CreateAccountActor(ctx, cst, state, template.RemainderAccount, keyIDs, av)
		if err != nil {
			return nil, nil, xerrors.Errorf("creating remainder acct: %w", err)
		}

	case genesis.TMultisig:
		if err = genesis2.CreateMultisigAccount(ctx, cst, state, actbuiltin.ReserveAddress, template.RemainderAccount, keyIDs, av); err != nil {
			return nil, nil, xerrors.Errorf("failed to set up remainder: %w", err)
		}
	default:
		return nil, nil, xerrors.Errorf("unknown account type for remainder: %w", err)
	}

	return state, keyIDs, nil
}

func SetupSCAActor(ctx context.Context, bs blockstore.Blockstore, params *sca.ConstructorParams) (*types.Actor, error) {
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

func SetupReplaceActor(ctx context.Context, bs blockstore.Blockstore) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	st, err := replace.ConstructState(adt.WrapStore(ctx, cst))
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, st)
	if err != nil {
		return nil, err
	}

	act := &types.Actor{
		Code:    actor.ReplaceActorCodeID,
		Balance: big.Zero(),
		Head:    statecid,
	}

	return act, nil
}

func SetupRewardActor(ctx context.Context, bs blockstore.Blockstore, qaPower big.Int, av actors.Version) (*types.Actor, error) {
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

func SetupStorageMarketActor(ctx context.Context, bs blockstore.Blockstore, av actors.Version) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	mst, err := market.MakeState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), av)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, mst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, err := market.GetActorCodeID(av)
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

func SetupStoragePowerActor(ctx context.Context, bs blockstore.Blockstore, av actors.Version) (*types.Actor, error) {

	cst := cbor.NewCborStore(bs)
	pst, err := power.MakeState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), av)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, pst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, err := power.GetActorCodeID(av)
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

type genFakeVerifier struct{}

var _ ffiwrapper.Verifier = (*genFakeVerifier)(nil)

func (m genFakeVerifier) VerifySeal(svi proof5.SealVerifyInfo) (bool, error) {
	return true, nil
}

func (m genFakeVerifier) VerifyAggregateSeals(aggregate proof5.AggregateSealVerifyProofAndInfos) (bool, error) {
	panic("not supported")
}

func (m genFakeVerifier) VerifyReplicaUpdate(update proof7.ReplicaUpdateInfo) (bool, error) {
	panic("not supported")
}

func (m genFakeVerifier) VerifyWinningPoSt(ctx context.Context, info proof7.WinningPoStVerifyInfo) (bool, error) {
	panic("not supported")
}

func (m genFakeVerifier) VerifyWindowPoSt(ctx context.Context, info proof7.WindowPoStVerifyInfo) (bool, error) {
	panic("not supported")
}

func (m genFakeVerifier) GenerateWinningPoStSectorChallenge(ctx context.Context, proof abi.RegisteredPoStProof, id abi.ActorID, randomness abi.PoStRandomness, u uint64) ([]uint64, error) {
	panic("not supported")
}
func (cg *ChainGen) Blockstore() blockstore.Blockstore {
	return cg.bs
}

func (cg *ChainGen) StateManager() *stmgr.StateManager {
	return cg.sm
}

func (cg *ChainGen) SetStateManager(sm *stmgr.StateManager) {
	cg.sm = sm
}

func (cg *ChainGen) ChainStore() *store.ChainStore {
	return cg.cs
}

func (cg *ChainGen) BeaconSchedule() beacon.Schedule {
	return cg.beacon
}

func (cg *ChainGen) Genesis() *types.BlockHeader {
	return cg.genesis
}

func (cg *ChainGen) Banker() address.Address {
	return cg.banker
}
