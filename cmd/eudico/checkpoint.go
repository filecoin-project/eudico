package main

import (
	"fmt"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/urfave/cli/v2"
)

var checkpointCmds = &cli.Command{
	Name:  "checkpoint",
	Usage: "Commands related with subneting",
	Subcommands: []*cli.Command{
		listCheckpoints,
		validateCheckpoints,
	},
}

var listCheckpoints = &cli.Command{
	Name:  "list-checkpoints",
	Usage: "list latest checkpoints committed for subnet",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to list checkpoints from",
			Value: hierarchical.RootSubnet.String(),
		},
		&cli.IntFlag{
			Name:  "num",
			Usage: "specify the number of checkpoints to list from current tipset (default=10)",
			Value: 10,
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		// If subnet not set use root. Otherwise, use flag value
		var subnet string
		if cctx.String("subnet") != hierarchical.RootSubnet.String() {
			subnet = cctx.String("subnet")
		}

		chs, err := api.ListCheckpoints(ctx, hierarchical.SubnetID(subnet), cctx.Int("num"))
		if err != nil {
			return err
		}
		for _, ch := range chs {
			chcid, _ := ch.Cid()
			prev, _ := ch.PreviousCheck()
			fmt.Printf("epoch: %d - cid=%s, previous=%v, childs=%v\n", ch.Epoch(), chcid, prev, ch.LenChilds())
		}

		return nil
	},
}

var validateCheckpoints = &cli.Command{
	Name:  "validate",
	Usage: "validate checkpoint for certain epoch",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to list checkpoints from",
			Value: hierarchical.RootSubnet.String(),
		},
		&cli.IntFlag{
			Name:  "epoch",
			Usage: "specify checkpoint epoch (default=latest)",
			Value: -1,
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		// If subnet not set use root. Otherwise, use flag value
		var subnet string
		if cctx.String("subnet") != hierarchical.RootSubnet.String() {
			subnet = cctx.String("subnet")
		}

		ch, err := api.VerifyCheckpoint(ctx, hierarchical.SubnetID(subnet), abi.ChainEpoch(cctx.Int("epoch")))
		if err != nil {
			fmt.Println("Verified KO!")
			return err
		}
		chcid, _ := ch.Cid()
		prev, _ := ch.PreviousCheck()
		fmt.Println("Verified OK!")
		fmt.Printf("epoch: %d - cid=%s, previous=%v, childs=%v\n", ch.Epoch(), chcid, prev, ch.LenChilds())

		return nil
	},
}
