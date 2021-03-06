package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	abciserver "github.com/tendermint/tendermint/abci/server"
	tmlogger "github.com/tendermint/tendermint/libs/log"
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
	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
	"github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func NewRootTendermintConsensus(ctx context.Context, sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) (consensus.Consensus, error) {
	return tendermint.NewConsensus(ctx, sm, nil, beacon, r, verifier, genesis, netName)
}

var tendermintCmd = &cli.Command{
	Name:  "tendermint",
	Usage: "Tendermint consensus testbed",
	Subcommands: []*cli.Command{
		tendermintGenesisCmd,
		tendermintMinerCmd,
		tendermintApplicationCmd,

		daemonCmd(node.Options(
			node.Override(new(consensus.Consensus), NewRootTendermintConsensus),
			node.Override(new(store.WeightFunc), tendermint.Weight),
			node.Unset(new(*slashfilter.SlashFilter)),
			node.Override(new(stmgr.Executor), common.RootTipSetExecutor),
			node.Override(new(stmgr.UpgradeSchedule), common.DefaultUpgradeSchedule()),
		)),
	},
}

var tendermintGenesisCmd = &cli.Command{
	Name:      "genesis",
	Usage:     "Generate genesis for Tendermint consensus",
	ArgsUsage: "[outfile]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 1 {
			return xerrors.Errorf("expected 2 arguments")
		}

		fmt.Printf("GENESIS MINER ADDRESS: t0%d\n", genesis.MinerStart)

		fName := cctx.Args().First()

		if err := subnet.CreateGenesisFile(cctx.Context, fName, hierarchical.Tendermint, address.Undef); err != nil {
			return xerrors.Errorf("creating genesis: %w", err)
		}

		log.Warnf("CREATED GENESIS FILE AT %s", fName)

		return nil
	},
}

var tendermintMinerCmd = &cli.Command{
	Name:  "miner",
	Usage: "run Tendermint consensus miner",
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
		return tendermint.Mine(ctx, miner, api)
	},
}

var tendermintApplicationCmd = &cli.Command{
	Name:  "application",
	Usage: "run tendermint consensus application",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "addr",
			Value: "tcp://127.0.0.1:26658",
			Usage: "socket address",
		},
	},
	Action: func(cctx *cli.Context) error {
		app, err := tendermint.NewApplication()
		if err != nil {
			return err
		}

		logger := tmlogger.MustNewDefaultLogger(tmlogger.LogFormatPlain, tmlogger.LogLevelInfo, false)
		server := abciserver.NewSocketServer(cctx.String("addr"), app)
		server.SetLogger(logger)

		if err := server.Start(); err != nil {
			return err
		}
		defer func() {
			err = server.Stop()
		}()

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		os.Exit(0)
		return err
	},
}
