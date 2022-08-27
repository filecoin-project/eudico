package main

import (
	"context"
	"os"

	logging "github.com/ipfs/go-log/v2"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/trace"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	"github.com/filecoin-project/lotus/lib/tracing"
	"github.com/filecoin-project/lotus/node/repo"
)

// TODO EUDICO Dedup with cmd/lotus

var eudCmds = []*cli.Command{
	lcli.WithCategory("daemon", delegatedCmd),
	lcli.WithCategory("daemon", tpowCmd),
	lcli.WithCategory("daemon", filcnsCmd),
	lcli.WithCategory("daemon", mirCmd),
	lcli.WithCategory("subnet", subnetCmds),
	lcli.WithCategory("benchmark", benchmarkCmd),
}

var log = logging.Logger("eudico")

var AdvanceBlockCmd *cli.Command

func main() {
	api.RunningNodeType = api.NodeFull

	lotuslog.SetupLogLevels()

	local := eudCmds
	if AdvanceBlockCmd != nil {
		local = append(local, AdvanceBlockCmd)
	}

	jaeger := tracing.SetupJaegerTracing("eudico")
	defer func() {
		if jaeger != nil {
			_ = jaeger.ForceFlush(context.Background())
		}
	}()

	for _, cmd := range local {
		cmd := cmd
		originBefore := cmd.Before
		cmd.Before = func(cctx *cli.Context) error {
			if jaeger != nil {
				_ = jaeger.Shutdown(cctx.Context)
			}
			jaeger = tracing.SetupJaegerTracing("lotus/" + cmd.Name)

			if originBefore != nil {
				return originBefore(cctx)
			}
			return nil
		}
	}
	ctx, span := trace.StartSpan(context.Background(), "/cli")
	defer span.End()

	interactiveDef := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	app := &cli.App{
		Name:                 "lotus",
		Usage:                "Filecoin decentralized storage network client",
		Version:              build.UserVersion(),
		EnableBashCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"EUDICO_PATH"},
				Hidden:  true,
				Value:   "~/.eudico", // TODO: Consider XDG_DATA_HOME
			},
			&cli.BoolFlag{
				Name:  "interactive",
				Usage: "setting to false will disable interactive functionality of commands",
				Value: interactiveDef,
			},
			&cli.BoolFlag{
				Name:  "force-send",
				Usage: "if true, will ignore pre-send checks",
			},
			&cli.StringFlag{
				Name:  "subnet-api",
				Usage: "set to point to the API of a specific subnet",
			},
			cliutil.FlagVeryVerbose,
		},

		Commands: append(local, lcli.Commands...),
	}

	app.Setup()
	app.Metadata["traceContext"] = ctx
	app.Metadata["repoType"] = repo.FullNode

	lcli.RunApp(app)
}
