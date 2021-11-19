package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	shard "github.com/filecoin-project/lotus/chain/consensus/actors/rm-shard"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/filecoin-project/lotus/chain/sharding/actors/sca"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/urfave/cli/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
)

var shardingCmds = &cli.Command{
	Name:  "sharding",
	Usage: "Commands related with sharding",
	Subcommands: []*cli.Command{
		addCmd,
		joinCmd,
		listShardsCmd,
		// leaveCmd,
		// killCmd,
	},
}

type selectParams struct {
	ID []byte // This ID is a cid.Bytes()
}

var listShardsCmd = &cli.Command{
	Name:  "list-shards",
	Usage: "list all shards in the current network",
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		ts, err := lcli.LoadTipSet(ctx, cctx, api)
		if err != nil {
			return err
		}

		var st sca.SCAState

		act, err := api.StateGetActor(ctx, sca.ShardCoordActorAddr, ts.Key())
		if err != nil {
			return xerrors.Errorf("error getting actor state: %w", err)
		}
		bs := blockstore.NewAPIBlockstore(api)
		cst := cbor.NewCborStore(bs)
		s := adt.WrapStore(ctx, cst)
		if err := cst.Get(ctx, act.Head, &st); err != nil {
			return xerrors.Errorf("error getting shard state: %w", err)
		}

		shards, err := sca.ListShards(s, st)
		if err := cst.Get(ctx, act.Head, &st); err != nil {
			return xerrors.Errorf("error getting list of shards: %w", err)
		}
		for _, sh := range shards {
			fmt.Printf("%s (%s): stake=%v, status=%v\n", sh.Cid, sh.ID, sh.Status, types.FIL(sh.Stake))
		}

		return nil
	},
}

var addCmd = &cli.Command{
	Name:      "add",
	Usage:     "Spawn a new shard in network",
	ArgsUsage: "[stake amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "parent",
			Usage: "specify the ID of the parent subnet from which to add",
		},
		&cli.IntFlag{
			Name:  "consensus",
			Usage: "specify consensus for the shard (0=delegated, 1=PoW)",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "specify name for the shard",
		},
		&cli.StringFlag{
			Name:  "delegminer",
			Usage: "optionally specify miner for delegated consensus",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Deprecated: use global 'force-send'",
		},
	},
	Action: func(cctx *cli.Context) error {

		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'send' expects no arguments, just a set of flags"))
		}

		srv, err := lcli.GetFullNodeServices(cctx)
		if err != nil {
			return err
		}
		defer srv.Close() //nolint:errcheck

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		consensus := 0
		if cctx.IsSet("consensus") {
			consensus = cctx.Int("consensus")
		}

		var name string
		if cctx.IsSet("name") {
			name = cctx.String("name")
		} else {
			return lcli.ShowHelp(cctx, fmt.Errorf("no name for shard specified"))
		}

		parent := naming.Root
		if cctx.IsSet("parent") {
			parent = naming.SubnetID(cctx.String("parent"))
		}

		// FIXME: This is a horrible workaround to avoid delegminer from
		// not being set. But need to demo in 30 mins, so will fix it afterwards
		// (we all know I'll come across this comment in 2 years and laugh at it).
		delegminer := sca.ShardCoordActorAddr
		if cctx.IsSet("delegminer") {
			d := cctx.String("delegminer")
			delegminer, err = address.NewFromString(d)
			if err != nil {
				return xerrors.Errorf("couldn't parse deleg miner address: %s", err)
			}
		} else if consensus == 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("no delegated miner for delegated consensus specified"))
		}
		minerStake := abi.NewStoragePower(1e8) // TODO: Make this value configurable in a flag/argument
		actorAddr, err := api.AddShard(ctx, addr, parent, name, uint64(consensus), minerStake, delegminer)
		if err != nil {
			return err
		}

		fmt.Printf("[*] subnet actor deployed as %v and new subnet availabe with ID=%v\n\n", actorAddr, naming.NewSubnetID(parent, actorAddr))
		fmt.Printf("remember to join and register your shard for it to be discoverable")
		return nil
	},
}

func printReceiptReturn(ctx context.Context, api v0api.FullNode, m *types.Message, r types.MessageReceipt) error {
	if len(r.Return) == 0 {
		return nil
	}

	act, err := api.StateGetActor(ctx, m.To, types.EmptyTSK)
	if err != nil {
		return err
	}

	jret, err := jsonReturn(act.Code, m.Method, r.Return)
	if err != nil {
		return err
	}

	fmt.Println("Decoded return value: ", jret)

	return nil
}

var joinCmd = &cli.Command{
	Name:      "join",
	Usage:     "Join or add additional stake to a shard",
	ArgsUsage: "[<actor address> <stake amount>]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "specify name for the shard",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Deprecated: use global 'force-send'",
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 1 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'send' expects one argument, amount to stake, and a set of flags"))
		}

		srv, err := lcli.GetFullNodeServices(cctx)
		if err != nil {
			return err
		}
		defer srv.Close() //nolint:errcheck

		ctx := lcli.ReqContext(cctx)
		var params lcli.SendParams

		params.To = shard.ShardActorAddr

		val, err := types.ParseFIL(cctx.Args().Get(0))
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

		addp := selectParams{}
		if cctx.IsSet("name") {
			c, err := shard.ShardID([]byte(cctx.String("name")))
			if err != nil {
				return lcli.ShowHelp(cctx, fmt.Errorf("could not generate CID for shard with that name"))
			}
			addp.ID = c.Bytes()
		} else {
			return lcli.ShowHelp(cctx, fmt.Errorf("no name for shard specified"))
		}

		paramsJson, err := json.Marshal(&addp)
		if err != nil {
			return xerrors.Errorf("failed marshalling addParams: %w", err)
		}

		// Join method
		params.Method = abi.MethodNum(3)
		decparams, err := srv.DecodeTypedParamsFromJSON(ctx, params.To, params.Method, string(paramsJson))
		if err != nil {
			return fmt.Errorf("failed to decode json params: %w", err)
		}
		params.Params = decparams

		proto, err := srv.MessageForSend(ctx, params)
		if err != nil {
			return xerrors.Errorf("creating message prototype: %w", err)
		}

		sm, err := lcli.InteractiveSend(ctx, cctx, srv, proto)
		if err != nil {
			return err
		}

		fmt.Fprintf(cctx.App.Writer, "%s\n", sm.Cid())
		return nil
	},
}

/*
var leaveCmd = &cli.Command{
	Name:      "leave",
	Usage:     "Leave a shard and take your stake back",
	ArgsUsage: "[]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "specify name for the shard",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Deprecated: use global 'force-send'",
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 0 {
			// NOTE: We may need to add an amount argument when we support partial
			// withdrawal of stake.
			return lcli.ShowHelp(cctx, fmt.Errorf("'send' expects no arguments, but a set of flags"))
		}

		srv, err := lcli.GetFullNodeServices(cctx)
		if err != nil {
			return err
		}
		defer srv.Close() //nolint:errcheck

		ctx := lcli.ReqContext(cctx)
		var params lcli.SendParams

		params.To = shard.ShardActorAddr

		if from := cctx.String("from"); from != "" {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			params.From = addr
		}

		addp := selectParams{}
		if cctx.IsSet("name") {
			c, err := shard.ShardID([]byte(cctx.String("name")))
			if err != nil {
				return lcli.ShowHelp(cctx, fmt.Errorf("could not generate CID for shard with that name"))
			}
			addp.ID = c.Bytes()
		} else {
			return lcli.ShowHelp(cctx, fmt.Errorf("no name for shard specified"))
		}

		paramsJson, err := json.Marshal(&addp)
		if err != nil {
			return xerrors.Errorf("failed marshalling addParams: %w", err)
		}

		// Leave method
		params.Method = abi.MethodNum(4)
		decparams, err := srv.DecodeTypedParamsFromJSON(ctx, params.To, params.Method, string(paramsJson))
		if err != nil {
			return fmt.Errorf("failed to decode json params: %w", err)
		}
		params.Params = decparams

		proto, err := srv.MessageForSend(ctx, params)
		if err != nil {
			return xerrors.Errorf("creating message prototype: %w", err)
		}

		sm, err := lcli.InteractiveSend(ctx, cctx, srv, proto)
		if err != nil {
			return err
		}

		fmt.Fprintf(cctx.App.Writer, "%s\n", sm.Cid())
		return nil
	},
}
*/
func MustSerialize(i cbg.CBORMarshaler) []byte {
	buf := new(bytes.Buffer)
	if err := i.MarshalCBOR(buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func jsonReturn(code cid.Cid, method abi.MethodNum, ret []byte) (string, error) {
	methodMeta, found := filcns.NewActorRegistry().Methods[code][method] // TODO: use remote
	if !found {
		return "", fmt.Errorf("method %d not found on actor %s", method, code)
	}
	re := reflect.New(methodMeta.Ret.Elem())
	p := re.Interface().(cbg.CBORUnmarshaler)
	if err := p.UnmarshalCBOR(bytes.NewReader(ret)); err != nil {
		return "", err
	}

	b, err := json.MarshalIndent(p, "", "  ")
	return string(b), err
}
