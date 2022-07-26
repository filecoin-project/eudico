package main

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/tools/stats/metrics"
	"github.com/filecoin-project/lotus/tools/stats/sync"

	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"

	"contrib.go.opencensus.io/exporter/prometheus"
	stats "go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

var log = logging.Logger("stats")

func init() {
	if err := view.Register(metrics.DefaultViews...); err != nil {
		log.Fatal(err)
	}
}

func main() {
	local := []*cli.Command{
		runCmd,
		versionCmd,
	}

	app := &cli.App{
		Name:    "eudico-stats",
		Usage:   "Collect basic information about a filecoin network using lotus",
		Version: build.UserVersion(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "lotus-path",
				EnvVars: []string{"LOTUS_PATH"},
				Value:   "~/.lotus", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:    "log-level",
				EnvVars: []string{"LOTUS_STATS_LOG_LEVEL"},
				Value:   "info",
			},
		},
		Before: func(cctx *cli.Context) error {
			return logging.SetLogLevelRegex("stats/*", cctx.String("log-level"))
		},
		Commands: local,
	}

	if err := app.Run(os.Args); err != nil {
		log.Errorw("exit in error", "err", err)
		os.Exit(1)
		return
	}
}

var versionCmd = &cli.Command{
	Name:  "version",
	Usage: "Print version",
	Action: func(cctx *cli.Context) error {
		cli.VersionPrinter(cctx)
		return nil
	},
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "no-sync",
			EnvVars: []string{"LOTUS_STATS_NO_SYNC"},
			Usage:   "do not wait for chain sync to complete",
			Value:   false,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()

		noSyncFlag := cctx.Bool("no-sync")

		exporter, err := prometheus.NewExporter(prometheus.Options{
			Namespace: "eudico_stats",
		})
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		if !noSyncFlag {
			if err := sync.SyncWait(ctx, api); err != nil {
				return err
			}
		}

		gtp, err := api.ChainGetGenesis(ctx)
		if err != nil {
			return err
		}

		genesisTime := time.Unix(int64(gtp.MinTimestamp()), 0)

		go func() {
			t := time.NewTicker(time.Second)

			for {
				select {
				case <-t.C:
					sinceGenesis := build.Clock.Now().Sub(genesisTime)
					expectedHeight := int64(sinceGenesis.Seconds()) / int64(build.BlockDelaySecs)

					stats.Record(ctx, metrics.TipsetCollectionHeightExpected.M(expectedHeight))
				}
			}
		}()

		http.Handle("/metrics", exporter)
		if err := http.ListenAndServe(":6688", nil); err != nil {
			log.Errorw("failed to start http server", "err", err)
		}

		return nil
	},
}
