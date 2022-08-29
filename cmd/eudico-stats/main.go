package main

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/tools/stats/sync"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"net/http"
	_ "net/http/pprof"
	"os"

	"contrib.go.opencensus.io/exporter/prometheus"
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
				Name:    "eudico-path",
				EnvVars: []string{"EUDICO_PATH"},
				Value:   "~/.eudico",
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
			EnvVars: []string{"EUDICO_STATS_NO_SYNC"},
			Usage:   "do not wait for chain sync to complete",
			Value:   false,
		},
		&cli.StringFlag{
			Name:    "neo4j-uri",
			EnvVars: []string{"EUDICO_STATS_NEO4J_URI"},
			Usage:   "The neo4j database uri",
			Value:   "neo4j://localhost:7474",
		},
		&cli.StringFlag{
			Name:    "neo4j-username",
			EnvVars: []string{"EUDICO_STATS_NEO4J_USERNAME"},
			Usage:   "The neo4j database username",
			Value:   "neo4j",
		},
		&cli.StringFlag{
			Name:    "neo4j-password",
			EnvVars: []string{"EUDICO_STATS_NEO4J_PASSWORD"},
			Usage:   "The neo4j database password",
			Value:   "neo4j",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()

		noSyncFlag := cctx.Bool("no-sync")
		uri := cctx.String("neo4j-uri")
		username := cctx.String("neo4j-username")
		password := cctx.String("neo4j-password")

		log.Infow("received config for neo4j", "uri", uri, "username", username, "password", password)
		client, err := NewNeo4jClient(uri, username, password)
		if err != nil {
			log.Errorw("cannot start neo4j client", "err", err)
			return err
		}

		// Register all metric views
		if err := view.Register(
			metrics.ChainNodeViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}

		exporter, err := prometheus.NewExporter(prometheus.Options{
			Namespace: "eudico_external_stats",
		})
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()

		if !noSyncFlag {
			if err := sync.SyncWait(ctx, api); err != nil {
				return err
			}
		}

		eudicoStats, err := NewEudicoStats(ctx, api, &client)
		if err != nil {
			return err
		}

		go func() {
			err = eudicoStats.Listen(ctx, address.RootSubnet)
			if err != nil {
				log.Errorw("cannot listen to root net")
				return
			}
		}()

		http.Handle("/metrics", exporter)
		if err := http.ListenAndServe(":6689", nil); err != nil {
			log.Errorw("failed to start http server", "err", err)
		}

		return nil
	},
}
