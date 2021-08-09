package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/google/uuid"
	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	"github.com/ipfs/go-merkledag"
	"github.com/ipld/go-car"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	lapi "github.com/filecoin-project/lotus/api"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/cmd/lotus-sim/simulation/mock"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/node"
)

var tpowCmd = &cli.Command{
	Name:  "tpow",
	Usage: "TipSet PoW consensus testbed",
	Subcommands: []*cli.Command{
		tpowGenesisCmd,
		tpowMinerCmd,

		daemonCmd(node.Options(
			node.Override(new(consensus.Consensus), tspow.NewTSPoWConsensus),
			node.Override(new(store.WeightFunc), tspow.Weight),
			node.Override(new(stmgr.Executor), delegcns.TipSetExecutor()), // todo
		)),
	},
}

var tpowGenesisCmd = &cli.Command{
	Name:      "genesis",
	Usage:     "Generate genesis for delegated consensus",
	ArgsUsage: "[miner secpk addr] [outfile]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return xerrors.Errorf("expected 2 arguments")
		}

		miner, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("parsing miner address: %w", err)
		}
		if miner.Protocol() != address.SECP256K1 {
			return xerrors.Errorf("must be secp address")
		}

		j := journal.NilJournal()
		bs := bstore.WrapIDStore(bstore.NewMemorySync())
		syscalls := vm.Syscalls(mock.Verifier)

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

		template := genesis.Template{
			NetworkVersion: network.Version13,
			Accounts: []genesis.Actor{{
				Type:    genesis.TAccount,
				Balance: types.FromFil(2),
				Meta:    json.RawMessage(`{"Owner":"` + miner.String() + `"}`), // correct??
			}},
			Miners:      nil,
			NetworkName: "eudico-" + uuid.New().String(),
			Timestamp:   uint64(time.Now().Unix()),

			VerifregRootKey: genesis.Actor{
				Type:    genesis.TAccount,
				Balance: types.FromFil(2),
				Meta:    json.RawMessage(`{"Owner":"` + vreg.String() + `"}`), // correct??
			},
			RemainderAccount: genesis.Actor{
				Type: genesis.TAccount,
				Meta: json.RawMessage(`{"Owner":"` + rem.String() + `"}`), // correct??
			},
		}

		b, err := MakeGenesisBlock(context.TODO(), j, bs, syscalls, template)
		if err != nil {
			return xerrors.Errorf("make genesis block: %w", err)
		}

		fmt.Printf("GENESIS MINER ADDRESS: t0%d\n", genesis2.MinerStart)

		f, err := os.OpenFile(cctx.Args().Get(1), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}

		offl := offline.Exchange(bs)
		blkserv := blockservice.New(bs, offl)
		dserv := merkledag.NewDAGService(blkserv)

		if err := car.WriteCarWithWalker(context.TODO(), dserv, []cid.Cid{b.Genesis.Cid()}, f, gen.CarWalkFunc); err != nil {
			return xerrors.Errorf("write genesis car: %w", err)
		}

		log.Warnf("WRITING GENESIS FILE AT %s", f.Name())

		if err := f.Close(); err != nil {
			return err
		}

		return nil
	},
}

var tpowMinerCmd = &cli.Command{
	Name:  "miner",
	Usage: "run delegated conesensus miner",
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := cliutil.ReqContext(cctx)

		head, err := api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting head: %w", err)
		}

		minerid, err := address.NewFromString("t0100")
		if err != nil {
			return err
		}
		miner, err := api.StateAccountKey(ctx, minerid, types.EmptyTSK)
		if err != nil {
			return err
		}

		log.Info("starting mining on @", head.Height())

		timer := time.NewTicker(time.Duration(build.BlockDelaySecs) * time.Second)
		for {
			select {
			case <-timer.C:
				base, err := api.ChainHead(ctx)
				if err != nil {
					log.Errorw("creating block failed", "error", err)
					continue
				}

				log.Info("try mining at @", base.Height())

				bh, err := api.MinerCreateBlock(context.TODO(), &lapi.BlockTemplate{
					Miner:            miner,
					Parents:          base.Key(),
					Ticket:           nil,
					Eproof:           nil,
					BeaconValues:     nil,
					Messages:         []*types.SignedMessage{}, // todo call select msgs
					Epoch:            base.Height() + 1,
					Timestamp:        base.MinTimestamp() + build.BlockDelaySecs,
					WinningPoStProof: nil,
				})
				if err != nil {
					log.Errorw("creating block failed", "error", err)
					continue
				}

				err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
					Header:        bh.Header,
					BlsMessages:   bh.BlsMessages,
					SecpkMessages: bh.SecpkMessages,
				})
				if err != nil {
					log.Errorw("submitting block failed", "error", err)
				}

				log.Info("mined a block! ", bh.Cid())
			case <-ctx.Done():
				return nil
			}
		}
	},
}
