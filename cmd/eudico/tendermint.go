package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	abciserver "github.com/tendermint/tendermint/abci/server"
	tmlogger "github.com/tendermint/tendermint/libs/log"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/benchmark"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
	"github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func NewRootTendermintConsensus(sm *stmgr.StateManager, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return tendermint.NewConsensus(sm, nil, beacon, r, verifier, genesis, netName)
}

var tendermintCmd = &cli.Command{
	Name:  "tendermint",
	Usage: "Tendermint consensus testbed",
	Subcommands: []*cli.Command{
		tendermintGenesisCmd,
		tendermintMinerCmd,
		tendermintApplicationCmd,
		tendermintBenchCmd,

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

		fmt.Printf("GENESIS MINER ADDRESS: t0%d\n", genesis.MinerStart)

		f, err := os.OpenFile(cctx.Args().First(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}

		// TODO: Make configurable
		checkPeriod := sca.DefaultCheckpointPeriod
		if err := subnet.WriteGenesis(address.RootSubnet, hierarchical.Tendermint, address.Undef, vreg, rem, checkPeriod, uint64(time.Now().Unix()), f); err != nil {
			return xerrors.Errorf("write genesis car: %w", err)
		}

		log.Warnf("WRITING GENESIS FILE AT %s", f.Name())

		if err := f.Close(); err != nil {
			return err
		}

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

var tendermintBenchCmd = &cli.Command{
	Name:  "benchmark",
	Usage: "run Tendermint consensus benchmark",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "length",
			Value: 10,
			Usage: "benchmark length",
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Starting Tendermint benchmarks")
		defer log.Info("Stopping Tendermint benchmarks")

		ctx := cliutil.ReqContext(cctx)

		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()

		stats, err := benchmark.RunSimpleBenchmark(ctx, api, cctx.Int("length"))
		if err != nil {
			return err
		}
		log.Info(stats.String())
		return nil
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
		defer server.Stop()

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		os.Exit(0)
		return nil
	},
}
