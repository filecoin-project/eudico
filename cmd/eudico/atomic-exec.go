package main

import (
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var atomicExecCmds = &cli.Command{
	Name:  "atomic",
	Usage: "Commands to orchestrate atomic execution between subnets",
	Subcommands: []*cli.Command{
		listAtomicExec,
		lockStateCmd,
	},
}

var listAtomicExec = &cli.Command{
	Name:  "list-execs",
	Usage: "list pending atomic executions for address",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to list checkpoints from",
			Value: address.RootSubnet.String(),
		},
	},
	Action: func(cctx *cli.Context) error {
		// api, closer, err := lcli.GetFullNodeAPI(cctx)
		// if err != nil {
		//         return err
		// }
		// defer closer()
		// ctx := lcli.ReqContext(cctx)
		//
		// // If subnet not set use root. Otherwise, use flag value
		// var subnet string
		// if cctx.String("subnet") != address.RootSubnet.String() {
		//         subnet = cctx.String("subnet")
		// }
		//
		// chs, err := api.ListCheckpoints(ctx, address.SubnetID(subnet), cctx.Int("num"))
		// if err != nil {
		//         return err
		// }
		// for _, ch := range chs {
		//         chcid, _ := ch.Cid()
		//         prev, _ := ch.PreviousCheck()
		//         lc := len(ch.CrossMsgs()) > 0
		//         fmt.Printf("epoch: %d - cid=%s, previous=%v, childs=%v, crossmsgs=%v\n", ch.Epoch(), chcid, prev, ch.LenChilds(), lc)
		// }
		// return nil
		panic("not implemented yet")
	},
}

var lockStateCmd = &cli.Command{
	Name:      "lock-state",
	Usage:     "Lock state for an actor to init atomic execution",
	ArgsUsage: "[<actor address>]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send message from",
		},
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet",
			Value: address.RootSubnet.String(),
		},
		&cli.IntFlag{
			Name:  "method",
			Usage: "specify actor method for atomic execution",
			Value: -1,
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 1 {
			return lcli.ShowHelp(cctx, fmt.Errorf("expects the actor address for atomic execution as input"))
		}
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		// Atomic execution requires an ancesto. Rootnet has no support for atomic exec.
		var subnet address.SubnetID
		if cctx.String("subnet") == address.RootSubnet.String() ||
			cctx.String("subnet") == "" {
			return xerrors.Errorf("only subnets with an ancestor can perform atomic executions")
		}
		subnet = address.SubnetID(cctx.String("subnet"))
		actorAddr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return lcli.ShowHelp(cctx, fmt.Errorf("failed to parse actor address: %w", err))
		}

		if cctx.Int("method") == -1 {
			return xerrors.Errorf("Method for atomic execution in actor nof specified")
		}
		method := abi.MethodNum(cctx.Int("method"))
		c, err := api.LockState(ctx, addr, actorAddr, subnet, method)
		if err != nil {
			return xerrors.Errorf("Error locking state: %s", err)
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully locked state with Cid: %s\n", c)
		fmt.Fprintf(cctx.App.Writer, "Ready to exchange to perform atomic execution for actor from: %s for method %v\n", subnet, method)
		return nil
	},
}
