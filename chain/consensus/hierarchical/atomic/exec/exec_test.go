package exec_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
	proof5 "github.com/filecoin-project/specs-actors/v5/actors/runtime/proof"
	proof7 "github.com/filecoin-project/specs-actors/v7/actors/runtime/proof"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/require"
	xerrors "golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/blockstore"
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
	"github.com/filecoin-project/lotus/chain/beacon"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	consensus "github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic/exec"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/lib/sigs"
	_ "github.com/filecoin-project/lotus/lib/sigs/bls"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
	"github.com/filecoin-project/lotus/node/bundle"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
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
	ts := cg.StateManager().ChainStore().GetHeaviestTipSet()
	output, err := exec.ComputeAtomicOutput(ctx, cg.StateManager(), ts, msgs[0].To, []atomic.LockableState{own1}, msgs)
	require.NoError(t, err)
	var owners replace.Owners
	err = atomic.UnwrapLockableState(output, &owners)
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
	ts = cg.StateManager().ChainStore().GetHeaviestTipSet()
	output2, err := exec.ComputeAtomicOutput(ctx, cg.StateManager(), ts, msgs[0].To, []atomic.LockableState{own1}, msgs)
	require.NoError(t, err)
	var owners2 replace.Owners
	err = atomic.UnwrapLockableState(output2, &owners2)
	require.NoError(t, err)

	// Check that the atomic replace happened.
	c, ok = owners2.M[taddr.String()]
	require.True(t, ok)
	require.Equal(t, c, cidUndef)
	c, ok = owners2.M[target.String()]
	require.True(t, ok)
	exp, _ = abi.CidBuilder.Sum([]byte("testSeed"))
	require.Equal(t, c, exp)

	t.Log("Comparing outputs of independent off-chain execution through CID")
	// Compare output cids.
	oc1, err := output.Cid()
	require.NoError(t, err)
	oc2, err := output2.Cid()
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
		NetworkName:      address.RootStr,
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

func MakeInitialStateTree(ctx context.Context,
	bs blockstore.Blockstore,
	template genesis.Template,
	chp abi.ChainEpoch) (*state.StateTree, map[address.Address]address.Address, error) {
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

type genFakeVerifier struct{}

var _ storiface.Verifier = (*genFakeVerifier)(nil)

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
