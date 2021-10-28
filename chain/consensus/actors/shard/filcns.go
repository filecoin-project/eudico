package shard

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"path/filepath"

	"github.com/docker/go-units"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	miner_builtin "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/cmd/lotus-seed/seed"
	"github.com/filecoin-project/lotus/cmd/lotus-sim/simulation/mock"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/mitchellh/go-homedir"
	xerrors "golang.org/x/xerrors"
)

var (
	defaultPreSealSectorSize = "2KiB"
)

func makeFilCnsGenesisBlock(ctx context.Context, bs bstore.Blockstore, template genesis.Template, miner address.Address, repoPath string) (*genesis2.GenesisBootstrap, error) {
	j := journal.NilJournal()
	sys := vm.Syscalls(mock.Verifier)

	// Add some preSealed data for the genesis miner.
	// NOTE: We may not need this and be able to go without it. Revisit
	// when we add FilCns support in shards.
	minerInfo, err := preSealedData(miner, filepath.Join(repoPath, template.NetworkName), defaultPreSealSectorSize, 2, 0)
	if err != nil {
		return nil, err
	}
	// Add genesis miner and pre-sealed data to the genesis block.
	err = addMiner(minerInfo, &template)
	if err != nil {
		return nil, err
	}

	return mkFilCnsGenesis(ctx, j, bs, sys, template)
}

func mkFilCnsGenesis(ctx context.Context, j journal.Journal, bs bstore.Blockstore, sys vm.SyscallBuilder, template genesis.Template) (*genesis2.GenesisBootstrap, error) {
	st, keyIDs, err := MakeInitialStateTree(ctx, FilCns, bs, template)
	if err != nil {
		return nil, xerrors.Errorf("make initial state tree failed: %w", err)
	}

	stateroot, err := st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("flush state tree failed: %w", err)
	}

	// temp chainstore
	cs := store.NewChainStore(bs, bs, datastore.NewMapDatastore(), nil, j)

	// Verify PreSealed Data
	stateroot, err = genesis2.VerifyPreSealedData(ctx, cs, sys, stateroot, template, keyIDs, template.NetworkVersion)
	if err != nil {
		return nil, xerrors.Errorf("failed to verify presealed data: %w", err)
	}

	stateroot, err = genesis2.SetupStorageMiners(ctx, cs, sys, stateroot, template.Miners, template.NetworkVersion)
	if err != nil {
		return nil, xerrors.Errorf("setup miners failed: %w", err)
	}

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

	log.Infof("Empty Genesis root: %s", emptyroot)

	tickBuf := make([]byte, 32)
	// NOTE: We can't use randomness in genesis block
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

	if err := bs.Put(sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &genesis2.GenesisBootstrap{
		Genesis: b,
	}, nil
}

func preSealedData(tAddr address.Address, dir string, sectorSz string, numSectors int, sectorOffset int) (string, error) {
	sbroot, err := homedir.Expand(dir)
	if err != nil {
		return "", err
	}
	// No Key info
	var k *types.KeyInfo
	/*
		if c.String("key") != "" {
			k = new(types.KeyInfo)
			kh, err := ioutil.ReadFile(c.String("key"))
			if err != nil {
				return err
			}
			kb, err := hex.DecodeString(string(kh))
			if err != nil {
				return err
			}
			if err := json.Unmarshal(kb, k); err != nil {
				return err
			}
		}
	*/
	sectorSizeInt, err := units.RAMInBytes(sectorSz)
	if err != nil {
		return "", err
	}
	sectorSize := abi.SectorSize(sectorSizeInt)

	spt, err := miner_builtin.SealProofTypeFromSectorSize(sectorSize, networkVersion)
	if err != nil {
		return "", err
	}

	fakeSectors := false
	gm, key, err := seed.PreSeal(tAddr, spt, abi.SectorNumber(sectorOffset), numSectors, sbroot, []byte("assume this is random"), k, fakeSectors)
	if err != nil {
		return "", err
	}

	return writeGenesisMiner(tAddr, sbroot, gm, key)
}

func writeGenesisMiner(maddr address.Address, sbroot string, gm *genesis.Miner, key *types.KeyInfo) (string, error) {
	prefPath := filepath.Join(sbroot, "pre-seal-"+maddr.String())
	output := map[string]genesis.Miner{
		maddr.String(): *gm,
	}

	out, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", err
	}

	log.Infof("Writing preseal manifest to %s", filepath.Join(sbroot, "pre-seal-"+maddr.String()+".json"))

	if err := ioutil.WriteFile(prefPath+".json", out, 0664); err != nil {
		return "", err
	}

	if key != nil {
		b, err := json.Marshal(key)
		if err != nil {
			return "", err
		}

		if err := ioutil.WriteFile(prefPath+".key", []byte(hex.EncodeToString(b)), 0664); err != nil {
			return "", err
		}
	}

	return prefPath + ".json", nil
}

func addMiner(preSealInfo string, template *genesis.Template) error {
	miners := map[string]genesis.Miner{}
	minb, err := ioutil.ReadFile(preSealInfo)
	if err != nil {
		return xerrors.Errorf("read preseal file: %w", err)
	}
	if err := json.Unmarshal(minb, &miners); err != nil {
		return xerrors.Errorf("unmarshal miner info: %w", err)
	}

	for mn, miner := range miners {
		log.Infof("Adding miner %s to genesis template", mn)
		{
			id := uint64(genesis2.MinerStart) + uint64(len(template.Miners))
			maddr, err := address.NewFromString(mn)
			if err != nil {
				return xerrors.Errorf("parsing miner address: %w", err)
			}
			mid, err := address.IDFromAddress(maddr)
			if err != nil {
				return xerrors.Errorf("getting miner id from address: %w", err)
			}
			if mid != id {
				return xerrors.Errorf("tried to set miner t0%d as t0%d", mid, id)
			}
		}

		template.Miners = append(template.Miners, miner)
		log.Infof("Giving %s some initial balance", miner.Owner)
		template.Accounts = append(template.Accounts, genesis.Actor{
			Type:    genesis.TAccount,
			Balance: big.Mul(big.NewInt(2), big.NewInt(int64(build.FilecoinPrecision))),
			Meta:    (&genesis.AccountMeta{Owner: miner.Owner}).ActorMeta(),
		})
	}

	return nil
}

func filCnsGenTemplate(shardID string, miner, vreg, rem address.Address, seq uint64) (*genesis.Template, error) {
	return &genesis.Template{
		NetworkVersion: networkVersion,
		Accounts:       []genesis.Actor{},
		/*
			Accounts: []genesis.Actor{{
				Type:    genesis.TAccount,
				Balance: types.FromFil(2),
				Meta:    json.RawMessage(`{"Owner":"` + miner.String() + `"}`),
			}},
		*/
		Miners:      nil,
		NetworkName: shardID,
		// NOTE: We can't use a Timestamp for this
		// because then the genesis generation in the shard
		// is non-deterministic. We use a swquence number for now.
		// Timestamp:   uint64(time.Now().Unix()),
		Timestamp: seq,

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
