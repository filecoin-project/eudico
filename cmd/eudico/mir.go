package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"

	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func NewRootMirConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier storiface.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) (consensus.Consensus, error) {
	return mir.NewConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

var mirCmd = &cli.Command{
	Name:  "mir",
	Usage: "Mir consensus",
	Subcommands: []*cli.Command{
		mirGenesisCmd,
		mirMinerCmd,

		daemonCmd(node.Options(
			node.Override(new(consensus.Consensus), NewRootMirConsensus),
			node.Override(new(store.WeightFunc), mir.Weight),
			node.Unset(new(*slashfilter.SlashFilter)),
			node.Override(new(stmgr.Executor), common.RootTipSetExecutor),
			node.Override(new(stmgr.UpgradeSchedule), common.DefaultUpgradeSchedule()),
		)),
	},
}

var mirGenesisCmd = &cli.Command{
	Name:      "genesis",
	Usage:     "Generate genesis for Mir consensus",
	ArgsUsage: "[outfile]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 1 {
			return xerrors.Errorf("expected 2 arguments")
		}

		fmt.Printf("GENESIS MINER ADDRESS: t0%d\n", genesis.MinerStart)

		fName := cctx.Args().First()

		if err := subnet.CreateGenesisFile(cctx.Context, fName, hierarchical.Mir, address.Undef); err != nil {
			return xerrors.Errorf("creating genesis: %w", err)
		}

		log.Warnf("CREATED GENESIS FILE AT %s", fName)

		return nil
	},
}

var mirMinerCmd = &cli.Command{
	Name:  "miner",
	Usage: "run Mir consensus miner",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "default-key",
			Value: true,
			Usage: "use default wallet's key",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := cliutil.ReqContext(cctx)

		var miner address.Address

		if cctx.Bool("default-key") {
			miner, err = api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}
		} else {
			miner, err = address.NewFromString(cctx.Args().First())
			if err != nil {
				return err
			}
		}
		if miner == address.Undef {
			return xerrors.Errorf("no miner address specified to start mining")
		}

		log.Infow("Starting mining with miner", "miner", miner)
		return mir.Mine(ctx, miner, api)
	},
}
