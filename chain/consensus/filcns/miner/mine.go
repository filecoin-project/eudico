package miner

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-statestore"
	"github.com/filecoin-project/lotus/api/v1api"
	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/lotus/journal"
	lotusminer "github.com/filecoin-project/lotus/miner"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage"
	"github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"
)

var scfg = sectorstorage.SealerConfig{
	ParallelFetchLimit: 1,
	AllowAddPiece:      true,
	AllowPreCommit1:    true,
	AllowPreCommit2:    true,
	AllowCommit:        true,
	AllowUnseal:        true,
}

// NOTE: This is super ugly, but I'll use it
// as a workaround to keep the mining implementation for each consensus
// for the different types of API interfaces in the same place.
// This can't stay like this for long (for everyone's sake), but
// will defer it to the future when I have time to give it a bit
// more of thought and we find a more elegant way of tackling this.
func Mine(ctx context.Context, ds datastore.Datastore, miner address.Address, v1api v1api.FullNode) error {
	// TODO: Also fix this.
	mid, err := modules.MinerID(dtypes.MinerAddress(miner))
	if err != nil {
		return err
	}
	// TODO: Make repo filesystem in next iteration
	// repo.NewFS
	mr := repo.NewMemory(nil)
	lr, err := mr.Lock(repo.StorageMiner)
	if err != nil {
		return err
	}
	ind := stores.NewIndex()
	// TODO: Assign right netName
	nds := nsds.Wrap(ds, datastore.NewKey("netName"))
	sf := modules.NewSlashFilter(nds)
	lstor, err := stores.NewLocal(ctx, lr, ind, []string{})
	// stor, err := modules.RemoteStorage(lstor, ind, )
	sst := statestore.New(ds)
	if err != nil {
		return err
	}
	prover, err := sectorstorage.New(ctx, lstor, nil, lr, ind, scfg, sst, sst)
	epp, err := storage.NewWinningPoStProver(v1api, prover, ffiwrapper.ProofVerifier, mid)
	if err != nil {
		return err
	}
	m := lotusminer.NewMiner(v1api, epp, miner, sf, journal.NilJournal())
	err = m.Start(ctx)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return nil
	}
}
