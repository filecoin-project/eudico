package main

import (
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/sharding"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/google/uuid"
	"github.com/ipfs/go-datastore"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	filcnsminer "github.com/filecoin-project/lotus/chain/consensus/filcns/miner"
	"github.com/filecoin-project/lotus/node"
)

var filCnsCmd = &cli.Command{
	Name:  "filcns",
	Usage: "Filecoin consensus testbed",
	Subcommands: []*cli.Command{
		filCnsGenesisCmd,
		filCnsMinerCmd,

		daemonCmd(node.Options(
			node.Override(new(consensus.Consensus), filcns.NewFilecoinExpectedConsensus),

			// Start sharding sub to listent to shard events
			node.Override(new(*sharding.ShardingSub), sharding.NewShardSub),
			node.Override(StartShardingSubKey, sharding.BuildShardingSub),
		)),
	},
}

var filCnsGenesisCmd = &cli.Command{
	Name:      "genesis",
	Usage:     "Generate genesis for filecoin consensus",
	ArgsUsage: "[miner secpk addr] [outfile]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return xerrors.Errorf("expected 2 arguments")
		}

		// TODO: Miner not currently being used for anything.
		// Figure out if we need it.
		miner, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("parsing miner address: %w", err)
		}
		if miner.Protocol() != address.SECP256K1 {
			return xerrors.Errorf("must be secp address")
		}

		memks := wallet.NewMemKeyStore()
		w, err := wallet.NewWallet(memks)
		if err != nil {
			return err
		}
		vreg, err := w.WalletNew(cctx.Context, types.KTBLS)
		if err != nil {
			return err
		}
		rem, err := w.WalletNew(cctx.Context, types.KTBLS)
		if err != nil {
			return err
		}

		f, err := os.OpenFile(cctx.Args().Get(1), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}

		if err := shard.WriteGenesis("eudico-"+uuid.New().String(), shard.FilCns, miner, vreg, rem, uint64(time.Now().Unix()), f); err != nil {
			return xerrors.Errorf("write genesis car: %w", err)
		}

		log.Warnf("WRITING GENESIS FILE AT %s", f.Name())

		if err := f.Close(); err != nil {
			return err
		}

		return nil
	},
}

var filCnsMinerCmd = &cli.Command{
	Name:  "miner",
	Usage: "run delegated conesensus miner",
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := cliutil.ReqContext(cctx)

		miner, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}
		if miner == address.Undef {
			return xerrors.Errorf("no miner address specified to start mining")
		}

		log.Infow("Starting mining with miner", miner)

		ds := dssync.MutexWrap(datastore.NewMapDatastore())

		return filcnsminer.Mine(ctx, ds, miner, nil, api)
		return nil
	},
}
