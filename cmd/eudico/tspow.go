package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/google/uuid"
	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	cbor "github.com/ipfs/go-ipld-cbor"
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

		b, err := MakePoWGenesisBlock(context.TODO(), j, bs, syscalls, template)
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
	Usage: "run tspow conesensus miner",
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

		miner, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		log.Info("starting mining on @", head.Height())

		for {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			base, err := api.ChainHead(ctx)
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}

			expDiff := tspow.GenesisWorkTarget
			if base.Height()+1 >= tspow.MaxDiffLookback {
				lbr := base.Height() + 1 - tspow.DiffLookback(base.Height())
				lbts, err := api.ChainGetTipSetByHeight(ctx, lbr, base.Key())
				if err != nil {
					return xerrors.Errorf("failed to get lookback tipset+1: %w", err)
				}

				expDiff = tspow.Difficulty(base, lbts)
			}

			diffb, err := expDiff.Bytes()
			if err != nil {
				return err
			}

			bh, err := api.MinerCreateBlock(context.TODO(), &lapi.BlockTemplate{
				Miner:            miner,
				Parents:          types.NewTipSetKey(tspow.BestWorkBlock(base).Cid()),
				BeaconValues:     nil,
				Ticket:           &types.Ticket{VRFProof: diffb},
				Messages:         []*types.SignedMessage{}, // todo call select msgs
				Epoch:            base.Height() + 1,
				Timestamp:        uint64(time.Now().Unix()),
				WinningPoStProof: nil,
			})
			if err != nil {
				log.Errorw("creating block failed", "error", err)
				continue
			}
			if bh == nil {
				continue
			}

			log.Info("try mining at @", base.Height(), base.String())

			err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
				Header:        bh.Header,
				BlsMessages:   bh.BlsMessages,
				SecpkMessages: bh.SecpkMessages,
			})
			if err != nil {
				log.Errorw("submitting block failed", "error", err)
			}

			log.Info("mined a block! ", bh.Cid())
		}
	},
}

func MakePoWGenesisBlock(ctx context.Context, j journal.Journal, bs bstore.Blockstore, sys vm.SyscallBuilder, template genesis.Template) (*genesis2.GenesisBootstrap, error) {
	if j == nil {
		j = journal.NilJournal()
	}
	st, _, err := genesis2.MakeInitialStateTree(ctx, bs, template)
	if err != nil {
		return nil, xerrors.Errorf("make initial state tree failed: %w", err)
	}

	stateroot, err := st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("flush state tree failed: %w", err)
	}

	// temp chainstore
	//cs := store.NewChainStore(bs, bs, datastore.NewMapDatastore(), j)

	/*	// Verify PreSealed Data
		stateroot, err = VerifyPreSealedData(ctx, cs, sys, stateroot, template, keyIDs, template.NetworkVersion)
		if err != nil {
			return nil, xerrors.Errorf("failed to verify presealed data: %w", err)
		}

		stateroot, err = SetupStorageMiners(ctx, cs, sys, stateroot, template.Miners, template.NetworkVersion)
		if err != nil {
			return nil, xerrors.Errorf("setup miners failed: %w", err)
		}*/

	store := adt.WrapStore(ctx, cbor.NewCborStore(bs))
	emptyroot, err := adt0.MakeEmptyArray(store).Root()
	if err != nil {
		return nil, xerrors.Errorf("amt build failed: %w", err)
	}

	mm := &types.MsgMeta{
		BlsMessages:   emptyroot,
		SecpkMessages: emptyroot,
	}
	mmb, err := mm.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing msgmeta failed: %w", err)
	}
	if err := bs.Put(mmb); err != nil {
		return nil, xerrors.Errorf("putting msgmeta block to blockstore: %w", err)
	}

	log.Infof("Empty Genesis root: %s", emptyroot)

	wtb, err := tspow.GenesisWorkTarget.Bytes()
	if err != nil {
		return nil, err
	}

	genesisticket := &types.Ticket{
		VRFProof: wtb,
	}

	b := &types.BlockHeader{
		Miner:                 system.Address,
		Ticket:                genesisticket,
		Parents:               []cid.Cid{},
		Height:                0,
		ParentWeight:          types.NewInt(0),
		ParentStateRoot:       stateroot,
		Messages:              mmb.Cid(),
		ParentMessageReceipts: emptyroot,
		BLSAggregate:          nil,
		BlockSig:              nil,
		Timestamp:             template.Timestamp,
		ElectionProof:         new(types.ElectionProof),
		BeaconEntries: []types.BeaconEntry{
			{
				Round: 0,
				Data:  make([]byte, 32),
			},
		},
		ParentBaseFee: abi.NewTokenAmount(build.InitialBaseFee),
	}

	sb, err := b.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing block header failed: %w", err)
	}

	if err := bs.Put(sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &genesis2.GenesisBootstrap{
		Genesis: b,
	}, nil
}
