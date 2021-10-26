package shard

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/docker/go-units"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	miner_builtin "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/cmd/lotus-seed/seed"
	"github.com/filecoin-project/lotus/cmd/lotus-sim/simulation/mock"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/mitchellh/go-homedir"
	xerrors "golang.org/x/xerrors"
)

func makeFilCnsGenesisBlock(ctx context.Context, bs bstore.Blockstore, template genesis.Template, miner address.Address) (*genesis2.GenesisBootstrap, error) {
	j := journal.NilJournal()
	syscalls := vm.Syscalls(mock.Verifier)
	// TODO: Add pre-seal data like is done in lotus-seed.
	minerInfo, err := preSealedData(miner, "2KiB", 2, 0)
	if err != nil {
		return nil, err
	}
	err = addMiner(minerInfo, &template)
	if err != nil {
		return nil, err
	}
	// TODO: Add miner lotus-seed genesis add-miner devnet.json ~/.genesis-sectors/pre-seal-t01000.json
	// And verify if it is deterministic.
	// TODO: Reusing standard filecoin consensus generation. We need to setup shard actor in this genesis
	// so we most probably need to take it out as we did with delegaed consensus.
	return genesis2.MakeGenesisBlock2(context.TODO(), j, bs, syscalls, template)

}

// See preSelCmd from cmd/lotus-seed/main.go
func preSealedData(miner address.Address, sectorSz string, numSectors int, sectorOffset int) (string, error) {
	// genesis = node.Override(new(modules.Genesis), testing.MakeGenesis(cctx.String(makeGenFlag), cctx.String(preTemplateFlag)))
	// No dir root specified
	sbroot, err := homedir.Expand("/tmp/genesis")
	fmt.Println(">>>>>>>  SBROOT", sbroot)
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
	// TODO: We should add here the right t ID for the miner
	// that is joining the shard and sealing the data.
	// ADD T ADDRESS AS INPUT? DO WE NEED THE SOURCE ADDRESS ALSO?
	miner, err = address.NewFromString("t01000")
	if err != nil {
		panic(err)
	}
	gm, key, err := seed.PreSeal(miner, spt, abi.SectorNumber(sectorOffset), numSectors, sbroot, []byte("this should be random"), k, fakeSectors)
	if err != nil {
		return "", err
	}

	return writeGenesisMiner(miner, sbroot, gm, key)
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

		// TODO: allow providing key
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
			Balance: big.Mul(big.NewInt(50_000_000), big.NewInt(int64(build.FilecoinPrecision))),
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
