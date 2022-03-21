package main

import (
	"github.com/urfave/cli/v2"

	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"

	"github.com/filecoin-project/lotus/chain/consensus/benchmark"
)

var benchmarkCmd = &cli.Command{
	Name:  "benchmark",
	Usage: "Benchmark tools",
	Subcommands: []*cli.Command{
		runConsensusBenchmarkCmd,
	},
}

var runConsensusBenchmarkCmd = &cli.Command{
	Name:  "consensus",
	Usage: "run consensus benchmark",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "length",
			Value: 10,
			Usage: "benchmark length",
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Starting benchmarks")
		defer log.Info("Stopping benchmarks")

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
