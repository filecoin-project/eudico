package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var atomicExecCmds = &cli.Command{
	Name:  "atomic",
	Usage: "Commands to orchestrate atomic execution between subnets",
	Subcommands: []*cli.Command{
		listAtomicExec,
		lockStateCmd,
		unlockStateCmd,
		formatMsgCmd,
		initExecCmd,
		abortCmd,
		submitExecCmd,
	},
}

var listAtomicExec = &cli.Command{
	Name:      "list-execs",
	Usage:     "list pending atomic executions for address",
	ArgsUsage: "[<target address>]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to list checkpoints from",
			Value: address.RootSubnet.String(),
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		if cctx.Args().Len() != 1 {
			return lcli.ShowHelp(cctx, fmt.Errorf("expects as argument the target address to get list of executions from"))
		}
		// If subnet not set use root. Otherwise, use flag value
		subnet := address.RootSubnet
		if cctx.String("subnet") != address.RootSubnet.String() {
			subnet = address.SubnetID(cctx.String("subnet"))
		}
		addr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return err
		}
		execs, err := api.ListAtomicExecs(ctx, subnet, addr)
		if err != nil {
			return err
		}
		if len(execs) == 0 {
			fmt.Printf("no pending executions in subnet\n")
		}
		for _, ex := range execs {
			c, err := ex.Params.Cid()
			if err != nil {
				return err
			}
			fmt.Printf("cid: %s - status=%s, submitted=%v\n", c, sca.ExecStatusStr[ex.Status], len(ex.Submitted))
		}
		return nil
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
		fmt.Fprintf(cctx.App.Writer, "Ready to exchange locked state to perform atomic execution for actor from: %s for method %v\n", subnet, method)
		return nil
	},
}

var unlockStateCmd = &cli.Command{
	Name:      "unlock-state",
	Usage:     "Unlock state for an actor",
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
		err = api.UnlockState(ctx, addr, actorAddr, subnet, method)
		if err != nil {
			return xerrors.Errorf("Error locking state: %s", err)
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully unlocked state")
		return nil
	},
}
var abortCmd = &cli.Command{
	Name:      "abort-exec",
	Usage:     "Abort atomic execution",
	ArgsUsage: "[<cid of exec>]",
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
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 1 {
			return lcli.ShowHelp(cctx, fmt.Errorf("expects cid of execution to abort as input"))
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

		subnet := address.RootSubnet
		if cctx.String("subnet") != address.RootSubnet.String() {
			subnet = address.SubnetID(cctx.String("subnet"))
		}
		idExec, err := cid.Parse(cctx.Args().Get(0))
		if err != nil {
			return err
		}
		state, err := api.AbortAtomicExec(ctx, addr, subnet, idExec)
		if err != nil {
			return xerrors.Errorf("Error aborting execution: %s", err)
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully aborted execution: %s\n", sca.ExecStatusStr[state])
		return nil
	},
}

var submitExecCmd = &cli.Command{
	Name:      "submit-exec",
	Usage:     "Compute off-chain and submit atomic execution result",
	ArgsUsage: "[<cid of exec>]",
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
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 1 {
			return lcli.ShowHelp(cctx, fmt.Errorf("expects cid of execution to submit as input"))
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

		subnet := address.RootSubnet
		if cctx.String("subnet") != address.RootSubnet.String() {
			subnet = address.SubnetID(cctx.String("subnet"))
		}
		idExec, err := cid.Parse(cctx.Args().Get(0))
		if err != nil {
			return err
		}
		state, err := api.ComputeAndSubmitExec(ctx, addr, subnet, idExec)
		if err != nil {
			return xerrors.Errorf("Error computing and submitting execution: %s", err)
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully submitted execution: %s\n", sca.ExecStatusStr[state])
		return nil
	},
}

var initExecCmd = &cli.Command{
	Name:      "init-exec",
	Usage:     "Signal initialization of atomic execution to the right subnet",
	ArgsUsage: "[]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send message from",
		},
		&cli.StringSliceFlag{
			Name:  "addr",
			Usage: "specify address involved in execution",
			Value: &cli.StringSlice{},
		},
		&cli.StringSliceFlag{
			Name:  "actor",
			Usage: "specify list of actors involved actors in the same order of the addresses provided",
			Value: &cli.StringSlice{},
		},
		&cli.StringSliceFlag{
			Name:  "locked",
			Usage: "specify the list of locked states in the order address have been provided",
			Value: &cli.StringSlice{},
		},
		&cli.StringSliceFlag{
			Name:  "msg",
			Usage: "list of messages for the atomic execution",
			Value: &cli.StringSlice{},
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("expects no arguments as input, just flags"))
		}
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		wallet, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			wallet, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		addrs := cctx.StringSlice("addr")
		actors := cctx.StringSlice("actor")
		locked := cctx.StringSlice("locked")
		msgs := cctx.StringSlice("msg")
		if len(addrs) < 2 {
			return lcli.ShowHelp(cctx, fmt.Errorf("not enough addresses provided for atomic execution"))
		}
		if len(actors) != len(addrs) || len(locked) != len(addrs) {
			return lcli.ShowHelp(cctx, fmt.Errorf("size of actors and/or locked states is not equal to the number of parties involved"))
		}
		if len(msgs) == 0 {
			return xerrors.Errorf("no messages provided for atomic execution")
		}
		inputs := make(map[string]sca.LockedState)
		for i, addr := range addrs {
			_, err := cid.Decode(locked[i])
			if err != nil {
				return xerrors.Errorf("error parsing cid of locked state: %w", err)
			}
			actor, err := address.NewFromString(actors[i])
			if err != nil {
				return xerrors.Errorf("error parsing actor address: %w", err)
			}
			inputs[addr] = sca.LockedState{Cid: locked[i], Actor: actor}
		}
		messages := make([]types.Message, len(msgs))
		for i, m := range msgs {
			msg := types.Message{}
			err = json.Unmarshal([]byte(m), &msg)
			if err != nil {
				return xerrors.Errorf("error unmarshalling mesasge: %w", err)
			}
			messages[i] = msg
		}

		c, err := api.InitAtomicExec(ctx, wallet, inputs, messages)
		if err != nil {
			return xerrors.Errorf("Error initializing atomic execution: %s", err)
		}
		cp, _ := sca.GetCommonParentForExec(inputs)
		fmt.Fprintf(cctx.App.Writer, "Successfully initialized atomic execution in subnet %s with Cid: %s\n", cp, c)
		fmt.Fprintf(cctx.App.Writer, "You can verify and submit the execution locally now.\n")
		return nil
	},
}

var formatMsgCmd = &cli.Command{
	Name:      "format-msg",
	Usage:     "Formats a message in a way that can be provided through the command-line to init an atomic exec",
	ArgsUsage: "[targetAddress] [amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "gas-premium",
			Usage: "specify gas price to use in AttoFIL",
			Value: "0",
		},
		&cli.StringFlag{
			Name:  "gas-feecap",
			Usage: "specify gas fee cap to use in AttoFIL",
			Value: "0",
		},
		&cli.Int64Flag{
			Name:  "gas-limit",
			Usage: "specify gas limit",
			Value: 0,
		},
		&cli.Uint64Flag{
			Name:  "nonce",
			Usage: "specify the nonce to use",
			Value: 0,
		},
		&cli.Uint64Flag{
			Name:  "method",
			Usage: "specify method to invoke",
			Value: uint64(builtin.MethodSend),
		},
		&cli.StringFlag{
			Name:  "params-json",
			Usage: "specify invocation parameters in json",
		},
		&cli.StringFlag{
			Name:  "params-hex",
			Usage: "specify invocation parameters in hex",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Deprecated: use global 'force-send'",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.IsSet("force") {
			fmt.Println("'force' flag is deprecated, use global flag 'force-send'")
		}

		if cctx.Args().Len() != 2 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'send' expects two arguments, target and amount"))
		}

		srv, err := lcli.GetFullNodeServices(cctx)
		if err != nil {
			return err
		}
		defer srv.Close() //nolint:errcheck

		ctx := lcli.ReqContext(cctx)
		var params lcli.SendParams

		params.To, err = address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return lcli.ShowHelp(cctx, fmt.Errorf("failed to parse target address: %w", err))
		}

		val, err := types.ParseFIL(cctx.Args().Get(1))
		if err != nil {
			return lcli.ShowHelp(cctx, fmt.Errorf("failed to parse amount: %w", err))
		}
		params.Val = abi.TokenAmount(val)

		if from := cctx.String("from"); from != "" {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			params.From = addr
		}

		if cctx.IsSet("gas-premium") {
			gp, err := types.BigFromString(cctx.String("gas-premium"))
			if err != nil {
				return err
			}
			params.GasPremium = &gp
		}

		if cctx.IsSet("gas-feecap") {
			gfc, err := types.BigFromString(cctx.String("gas-feecap"))
			if err != nil {
				return err
			}
			params.GasFeeCap = &gfc
		}

		if cctx.IsSet("gas-limit") {
			limit := cctx.Int64("gas-limit")
			params.GasLimit = &limit
		}

		params.Method = abi.MethodNum(cctx.Uint64("method"))

		if cctx.IsSet("params-json") {
			decparams, err := srv.DecodeTypedParamsFromJSON(ctx, params.To, params.Method, cctx.String("params-json"))
			if err != nil {
				return fmt.Errorf("failed to decode json params: %w", err)
			}
			params.Params = decparams
		}
		if cctx.IsSet("params-hex") {
			if params.Params != nil {
				return fmt.Errorf("can only specify one of 'params-json' and 'params-hex'")
			}
			decparams, err := hex.DecodeString(cctx.String("params-hex"))
			if err != nil {
				return fmt.Errorf("failed to decode hex params: %w", err)
			}
			params.Params = decparams
		}

		if cctx.IsSet("nonce") {
			n := cctx.Uint64("nonce")
			params.Nonce = &n
		}

		proto, err := srv.MessageForSend(ctx, params)
		if err != nil {
			return xerrors.Errorf("creating message prototype: %w", err)
		}

		b, err := json.Marshal(proto.Message)
		if err != nil {
			return xerrors.Errorf("error marshaling message: %w", err)
		}

		fmt.Fprintf(cctx.App.Writer, "%s\n", string(b))

		return nil
	},
}
