package shard

import (
	"context"
	"encoding/json"

	address "github.com/filecoin-project/go-address"
	bstore "github.com/filecoin-project/lotus/blockstore"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/cmd/lotus-sim/simulation/mock"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
)

func makeFilCnsGenesisBlock(ctx context.Context, bs bstore.Blockstore, template genesis.Template) (*genesis2.GenesisBootstrap, error) {
	j := journal.NilJournal()
	syscalls := vm.Syscalls(mock.Verifier)
	// TODO: Reusing standard filecoin consensus generation. We need to setup shard actor in this genesis
	// so we most probably need to take it out as we did with delegaed consensus.
	return genesis2.MakeGenesisBlock(context.TODO(), j, bs, syscalls, template)

}

func filCnsGenTemplate(shardID string, miner, vreg, rem address.Address, seq uint64) (*genesis.Template, error) {
	return &genesis.Template{
		NetworkVersion: networkVersion,
		Accounts: []genesis.Actor{{
			Type:    genesis.TAccount,
			Balance: types.FromFil(2),
			Meta:    json.RawMessage(`{"Owner":"` + miner.String() + `"}`),
		}},
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
