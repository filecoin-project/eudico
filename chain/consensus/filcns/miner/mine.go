package miner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-statestore"
	"github.com/filecoin-project/lotus/api"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	"github.com/filecoin-project/lotus/chain/types"
	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	sealing "github.com/filecoin-project/lotus/extern/storage-sealing"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/journal/fsjournal"
	lotusminer "github.com/filecoin-project/lotus/miner"
	storageminer "github.com/filecoin-project/lotus/miner"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage"
	market2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/market"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
	power2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/power"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	nsds "github.com/ipfs/go-datastore/namespace"
	logging "github.com/ipfs/go-log/v2"
	crypto "github.com/libp2p/go-libp2p-core/crypto"
	peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"
)

var log = logging.Logger("filcns-miner")
var scfg = sectorstorage.SealerConfig{
	ParallelFetchLimit: 1,
	AllowAddPiece:      true,
	AllowPreCommit1:    true,
	AllowPreCommit2:    true,
	AllowCommit:        true,
	AllowUnseal:        true,
}

type MinerOpts struct {
	repoPath            string
	preSealedSectorSize string
	preSealedPaths      []string
	preSealedMetadata   string
	isGenesis           bool
	minerActor          string
}

func NewOpts(minerActor string, repoPath string, pssize string, pspaths []string, psmeta string, isGenesis bool) *MinerOpts {
	return &MinerOpts{
		repoPath:            repoPath,
		preSealedSectorSize: pssize,
		preSealedPaths:      pspaths,
		preSealedMetadata:   psmeta,
		isGenesis:           isGenesis,
		minerActor:          minerActor,
	}
}

func Mine(ctx context.Context, ds datastore.Datastore, miner address.Address, v1api v1api.FullNode, mopts *MinerOpts) error {
	// TODO: Is this the right minerID.
	mid, err := modules.MinerID(dtypes.MinerAddress(miner))
	if err != nil {
		return err
	}
	log.Info("Checking if repo exists")

	var lr repo.LockedRepo
	r, err := repo.NewFS(mopts.repoPath)
	if err != nil {
		return err
	}

	ok, err := r.Exists()
	if err != nil {
		return err
	}
	if ok {
		// Assign repo if already initialized
		log.Infof("repo at '%s' is already initialized", mopts.repoPath)
		lr, err = r.Lock(repo.StorageMiner)
	} else {
		// If not initialize it
		lr, err = initMiner(ctx, v1api, r, mopts)
	}
	if err != nil {
		return err
	}

	// Get network name
	netname, err := v1api.StateNetworkName(ctx)
	if err != nil {
		return err
	}
	ind := stores.NewIndex()
	nds := nsds.Wrap(ds, datastore.NewKey(string(netname)))
	sf := modules.NewSlashFilter(nds)
	lstor, err := stores.NewLocal(ctx, lr, ind, []string{})
	// NOTE: No remote storage supported for now.
	// stor, err := modules.RemoteStorage(lstor, ind, )
	sst := statestore.New(ds)
	if err != nil {
		return err
	}
	prover, err := sectorstorage.New(ctx, lstor, nil, lr, ind, scfg, sst, sst)
	epp, err := storage.NewWinningPoStProver(v1api, prover, ffiwrapper.ProofVerifier, mid)
	if err != nil {
		return err
	}
	m := lotusminer.NewMiner(v1api, epp, miner, sf, journal.NilJournal())
	err = m.Start(ctx)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		// Closing repo
		return lr.Close()
	}
}

func initMiner(ctx context.Context, v1api v1api.FullNode, r *repo.FsRepo, mopts *MinerOpts) (repo.LockedRepo, error) {
	log.Info("Initializing lotus miner")

	// TODO: Consider removign this alias and fixing the code below.
	repoPath := mopts.repoPath
	genesisMiner := mopts.isGenesis
	preSealedPaths := mopts.preSealedPaths
	sectorSz := mopts.preSealedSectorSize
	presealedMeta := mopts.preSealedMetadata

	sectorSizeInt, err := units.RAMInBytes(sectorSz)
	if err != nil {
		return nil, err
	}
	ssize := abi.SectorSize(sectorSizeInt)

	// Gas premium set to zero for now.
	gasPremium := "0"
	gasPrice, err := types.BigFromString(gasPremium)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse gas-price flag: %s", err)
	}

	log.Info("Checking proof parameters")

	if err := paramfetch.GetParams(ctx, build.ParametersJSON(), build.SrsJSON(), uint64(ssize)); err != nil {
		return nil, xerrors.Errorf("fetching proof parameters: %w", err)
	}

	log.Info("Trying to connect to full node RPC")

	// TODO: This needs to be configurable and taken somewhere else.
	err = importKeyv1(ctx, v1api, "/tmp/genesis/pre-seal-t01000.key")
	if err != nil {
		fmt.Println(">>>> Couldn't import key for miner with pre-sealed data")
		panic(err)
	}
	log.Info("Checking full node sync status")

	// We need to support initializing genesis miners and new joiners to the network.
	if !genesisMiner {
		if err := SyncWait(ctx, &v0api.WrapperV1Full{FullNode: v1api}, false); err != nil {
			return nil, xerrors.Errorf("sync wait: %w", err)
		}
	}

	// If not we initialize the repo
	log.Info("Initializing repo")

	if err := r.Init(repo.StorageMiner); err != nil {
		return nil, err
	}

	{
		lr, err := r.Lock(repo.StorageMiner)
		if err != nil {
			return nil, err
		}

		var localPaths []stores.LocalPath

		if pssb := preSealedPaths; len(pssb) != 0 {
			log.Infof("Setting up storage config with presealed sectors: %v", pssb)

			for _, psp := range pssb {
				psp, err := homedir.Expand(psp)
				if err != nil {
					return nil, err
				}
				localPaths = append(localPaths, stores.LocalPath{
					Path: psp,
				})
			}
		}

		if err := lr.SetStorage(func(sc *stores.StorageConfig) {
			sc.StoragePaths = append(sc.StoragePaths, localPaths...)
		}); err != nil {
			return nil, xerrors.Errorf("set storage config: %w", err)
		}

		if err := lr.Close(); err != nil {
			return nil, err
		}
	}

	if err := storageMinerInit(ctx, v1api, r, ssize, gasPrice, mopts.minerActor, genesisMiner, presealedMeta); err != nil {
		log.Errorf("Failed to initialize lotus-miner: %+v", err)
		path, err := homedir.Expand(repoPath)
		if err != nil {
			return nil, err
		}
		log.Infof("Cleaning up %s after attempt...", path)
		if err := os.RemoveAll(path); err != nil {
			log.Errorf("Failed to clean up failed storage repo: %s", err)
		}
		return nil, xerrors.Errorf("Storage-miner init failed")
	}

	log.Info("Miner successfully initialized")

	return r.Lock(repo.StorageMiner)
}

func importKeyv1(ctx context.Context, api v1api.FullNode, f string) error {
	f, err := homedir.Expand(f)
	if err != nil {
		return err
	}

	hexdata, err := ioutil.ReadFile(f)
	if err != nil {
		return err
	}

	data, err := hex.DecodeString(strings.TrimSpace(string(hexdata)))
	if err != nil {
		return err
	}

	var ki types.KeyInfo
	if err := json.Unmarshal(data, &ki); err != nil {
		return err
	}

	addr, err := api.WalletImport(ctx, &ki)
	if err != nil {
		return err
	}

	if err := api.WalletSetDefault(ctx, addr); err != nil {
		return err
	}

	log.Infof("successfully imported key for %s", addr)
	return nil
}

// TODO: This code needs to be reorged.
func storageMinerInit(ctx context.Context, api v1api.FullNode, r repo.Repo, ssize abi.SectorSize, gasPrice types.BigInt, minerActor string, genesisMiner bool, preSealedMetadataPath string) error {
	lr, err := r.Lock(repo.StorageMiner)
	if err != nil {
		return err
	}
	defer lr.Close() //nolint:errcheck

	log.Info("Initializing libp2p identity")

	p2pSk, err := makeHostKey(lr)
	if err != nil {
		return xerrors.Errorf("make host key: %w", err)
	}

	peerid, err := peer.IDFromPrivateKey(p2pSk)
	if err != nil {
		return xerrors.Errorf("peer ID from private key: %w", err)
	}

	mds, err := lr.Datastore(context.TODO(), "/metadata")
	if err != nil {
		return err
	}

	var addr address.Address
	if act := minerActor; act != "" {
		a, err := address.NewFromString(act)
		if err != nil {
			return xerrors.Errorf("failed parsing actor flag value (%q): %w", act, err)
		}

		if genesisMiner {
			if err := mds.Put(datastore.NewKey("miner-address"), a.Bytes()); err != nil {
				return err
			}

			mid, err := address.IDFromAddress(a)
			if err != nil {
				return xerrors.Errorf("getting id address: %w", err)
			}

			sa, err := modules.StorageAuth(ctx, api)
			if err != nil {
				return err
			}

			wsts := statestore.New(namespace.Wrap(mds, modules.WorkerCallsPrefix))
			smsts := statestore.New(namespace.Wrap(mds, modules.ManagerWorkPrefix))

			si := stores.NewIndex()

			lstor, err := stores.NewLocal(ctx, lr, si, nil)
			if err != nil {
				return err
			}
			stor := stores.NewRemote(lstor, si, http.Header(sa), 10, &stores.DefaultPartialFileHandler{})

			smgr, err := sectorstorage.New(ctx, lstor, stor, lr, si, sectorstorage.SealerConfig{
				ParallelFetchLimit: 10,
				AllowAddPiece:      true,
				AllowPreCommit1:    true,
				AllowPreCommit2:    true,
				AllowCommit:        true,
				AllowUnseal:        true,
			}, wsts, smsts)
			if err != nil {
				return err
			}
			epp, err := storage.NewWinningPoStProver(api, smgr, ffiwrapper.ProofVerifier, dtypes.MinerID(mid))
			if err != nil {
				return err
			}

			j, err := fsjournal.OpenFSJournal(lr, journal.EnvDisabledEvents())
			if err != nil {
				return fmt.Errorf("failed to open filesystem journal: %w", err)
			}

			m := storageminer.NewMiner(api, epp, a, slashfilter.New(mds), j)
			{
				if err := m.Start(ctx); err != nil {
					return xerrors.Errorf("failed to start up genesis miner: %w", err)
				}

				cerr := configureStorageMiner(ctx, api, a, peerid, gasPrice)

				if err := m.Stop(ctx); err != nil {
					log.Error("failed to shut down miner: ", err)
				}

				if cerr != nil {
					return xerrors.Errorf("failed to configure miner: %w", cerr)
				}
			}

			if pssb := preSealedMetadataPath; pssb != "" {
				pssb, err := homedir.Expand(pssb)
				if err != nil {
					return err
				}

				log.Infof("Importing pre-sealed sector metadata for %s", a)

				if err := migratePreSealMeta(ctx, api, pssb, a, mds); err != nil {
					return xerrors.Errorf("migrating presealed sector metadata: %w", err)
				}
			}

			return nil
		}

		if pssb := preSealedMetadataPath; pssb != "" {
			pssb, err := homedir.Expand(pssb)
			if err != nil {
				return err
			}

			log.Infof("Importing pre-sealed sector metadata for %s", a)

			if err := migratePreSealMeta(ctx, api, pssb, a, mds); err != nil {
				return xerrors.Errorf("migrating presealed sector metadata: %w", err)
			}
		}

		if err := configureStorageMiner(ctx, api, a, peerid, gasPrice); err != nil {
			return xerrors.Errorf("failed to configure miner: %w", err)
		}

		addr = a
	} else {
		a, err := createStorageMiner(ctx, api, peerid, gasPrice, ssize)
		if err != nil {
			return xerrors.Errorf("creating miner failed: %w", err)
		}

		addr = a
	}

	log.Infof("Created new miner: %s", addr)
	if err := mds.Put(datastore.NewKey("miner-address"), addr.Bytes()); err != nil {
		return err
	}

	return nil
}

func makeHostKey(lr repo.LockedRepo) (crypto.PrivKey, error) {
	pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}

	ks, err := lr.KeyStore()
	if err != nil {
		return nil, err
	}

	kbytes, err := crypto.MarshalPrivateKey(pk)
	if err != nil {
		return nil, err
	}

	if err := ks.Put("libp2p-host", types.KeyInfo{
		Type:       "libp2p-host",
		PrivateKey: kbytes,
	}); err != nil {
		return nil, err
	}

	return pk, nil
}

func configureStorageMiner(ctx context.Context, api v1api.FullNode, addr address.Address, peerid peer.ID, gasPrice types.BigInt) error {
	mi, err := api.StateMinerInfo(ctx, addr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getWorkerAddr returned bad address: %w", err)
	}

	enc, err := actors.SerializeParams(&miner2.ChangePeerIDParams{NewID: abi.PeerID(peerid)})
	if err != nil {
		return err
	}

	msg := &types.Message{
		To:         addr,
		From:       mi.Worker,
		Method:     miner.Methods.ChangePeerID,
		Params:     enc,
		Value:      types.NewInt(0),
		GasPremium: gasPrice,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return err
	}

	log.Info("Waiting for message: ", smsg.Cid())
	ret, err := api.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, lapi.LookbackNoLimit, true)
	if err != nil {
		return err
	}

	if ret.Receipt.ExitCode != 0 {
		return xerrors.Errorf("update peer id message failed with exit code %d", ret.Receipt.ExitCode)
	}

	return nil
}
func migratePreSealMeta(ctx context.Context, api v1api.FullNode, metadata string, maddr address.Address, mds dtypes.MetadataDS) error {
	metadata, err := homedir.Expand(metadata)
	if err != nil {
		return xerrors.Errorf("expanding preseal dir: %w", err)
	}

	b, err := ioutil.ReadFile(metadata)
	if err != nil {
		return xerrors.Errorf("reading preseal metadata: %w", err)
	}

	psm := map[string]genesis.Miner{}
	if err := json.Unmarshal(b, &psm); err != nil {
		return xerrors.Errorf("unmarshaling preseal metadata: %w", err)
	}

	meta, ok := psm[maddr.String()]
	if !ok {
		return xerrors.Errorf("preseal file didn't contain metadata for miner %s", maddr)
	}

	maxSectorID := abi.SectorNumber(0)
	for _, sector := range meta.Sectors {
		sectorKey := datastore.NewKey(sealing.SectorStorePrefix).ChildString(fmt.Sprint(sector.SectorID))

		dealID, err := findMarketDealID(ctx, api, sector.Deal)
		if err != nil {
			return xerrors.Errorf("finding storage deal for pre-sealed sector %d: %w", sector.SectorID, err)
		}
		commD := sector.CommD
		commR := sector.CommR

		info := &sealing.SectorInfo{
			State:        sealing.Proving,
			SectorNumber: sector.SectorID,
			Pieces: []sealing.Piece{
				{
					Piece: abi.PieceInfo{
						Size:     abi.PaddedPieceSize(meta.SectorSize),
						PieceCID: commD,
					},
					DealInfo: &lapi.PieceDealInfo{
						DealID:       dealID,
						DealProposal: &sector.Deal,
						DealSchedule: lapi.DealSchedule{
							StartEpoch: sector.Deal.StartEpoch,
							EndEpoch:   sector.Deal.EndEpoch,
						},
					},
				},
			},
			CommD:            &commD,
			CommR:            &commR,
			Proof:            nil,
			TicketValue:      abi.SealRandomness{},
			TicketEpoch:      0,
			PreCommitMessage: nil,
			SeedValue:        abi.InteractiveSealRandomness{},
			SeedEpoch:        0,
			CommitMessage:    nil,
		}

		b, err := cborutil.Dump(info)
		if err != nil {
			return err
		}

		if err := mds.Put(sectorKey, b); err != nil {
			return err
		}

		if sector.SectorID > maxSectorID {
			maxSectorID = sector.SectorID
		}

		/* // TODO: Import deals into market
		pnd, err := cborutil.AsIpld(sector.Deal)
		if err != nil {
			return err
		}

		dealKey := datastore.NewKey(deals.ProviderDsPrefix).ChildString(pnd.Cid().String())

		deal := &deals.MinerDeal{
			MinerDeal: storagemarket.MinerDeal{
				ClientDealProposal: sector.Deal,
				ProposalCid: pnd.Cid(),
				State:       storagemarket.StorageDealActive,
				Ref:         &storagemarket.DataRef{Root: proposalCid}, // TODO: This is super wrong, but there
				// are no params for CommP CIDs, we can't recover unixfs cid easily,
				// and this isn't even used after the deal enters Complete state
				DealID: dealID,
			},
		}

		b, err = cborutil.Dump(deal)
		if err != nil {
			return err
		}

		if err := mds.Put(dealKey, b); err != nil {
			return err
		}*/
	}

	buf := make([]byte, binary.MaxVarintLen64)
	size := binary.PutUvarint(buf, uint64(maxSectorID))
	return mds.Put(datastore.NewKey(modules.StorageCounterDSPrefix), buf[:size])
}

func findMarketDealID(ctx context.Context, api v1api.FullNode, deal market2.DealProposal) (abi.DealID, error) {
	// TODO: find a better way
	//  (this is only used by genesis miners)

	deals, err := api.StateMarketDeals(ctx, types.EmptyTSK)
	if err != nil {
		return 0, xerrors.Errorf("getting market deals: %w", err)
	}

	for k, v := range deals {
		if v.Proposal.PieceCID.Equals(deal.PieceCID) {
			id, err := strconv.ParseUint(k, 10, 64)
			return abi.DealID(id), err
		}
	}

	return 0, xerrors.New("deal not found")
}

func createStorageMiner(ctx context.Context, api v1api.FullNode, peerid peer.ID, gasPrice types.BigInt, ssize abi.SectorSize) (address.Address, error) {
	var err error
	var owner address.Address
	// NOTE: Always using default address here
	//if cctx.String("owner") != "" {
	//	owner, err = address.NewFromString(cctx.String("owner"))
	//} else {
	owner, err = api.WalletDefaultAddress(ctx)
	//}
	if err != nil {
		return address.Undef, err
	}

	worker := owner
	/* NOTE: No support for workers for now.
	if cctx.String("worker") != "" {
		worker, err = address.NewFromString(cctx.String("worker"))
	} else if cctx.Bool("create-worker-key") { // TODO: Do we need to force this if owner is Secpk?
		worker, err = api.WalletNew(ctx, types.KTBLS)
	}
	*/
	if err != nil {
		return address.Address{}, err
	}

	// make sure the worker account exists on chain
	_, err = api.StateLookupID(ctx, worker, types.EmptyTSK)
	if err != nil {
		signed, err := api.MpoolPushMessage(ctx, &types.Message{
			From:  owner,
			To:    worker,
			Value: types.NewInt(0),
		}, nil)
		if err != nil {
			return address.Undef, xerrors.Errorf("push worker init: %w", err)
		}

		log.Infof("Initializing worker account %s, message: %s", worker, signed.Cid())
		log.Infof("Waiting for confirmation")

		mw, err := api.StateWaitMsg(ctx, signed.Cid(), build.MessageConfidence, lapi.LookbackNoLimit, true)
		if err != nil {
			return address.Undef, xerrors.Errorf("waiting for worker init: %w", err)
		}
		if mw.Receipt.ExitCode != 0 {
			return address.Undef, xerrors.Errorf("initializing worker account failed: exit code %d", mw.Receipt.ExitCode)
		}
	}

	nv, err := api.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return address.Undef, xerrors.Errorf("getting network version: %w", err)
	}

	spt, err := miner.SealProofTypeFromSectorSize(ssize, nv)
	if err != nil {
		return address.Undef, xerrors.Errorf("getting seal proof type: %w", err)
	}

	params, err := actors.SerializeParams(&power2.CreateMinerParams{
		Owner:         owner,
		Worker:        worker,
		SealProofType: spt,
		Peer:          abi.PeerID(peerid),
	})
	if err != nil {
		return address.Undef, err
	}

	sender := owner
	/* TODO: From not configurable
	if fromstr := cctx.String("from"); fromstr != "" {
		faddr, err := address.NewFromString(fromstr)
		if err != nil {
			return address.Undef, fmt.Errorf("could not parse from address: %w", err)
		}
		sender = faddr
	}
	*/

	createStorageMinerMsg := &types.Message{
		To:    power.Address,
		From:  sender,
		Value: big.Zero(),

		Method: power.Methods.CreateMiner,
		Params: params,

		GasLimit:   0,
		GasPremium: gasPrice,
	}

	signed, err := api.MpoolPushMessage(ctx, createStorageMinerMsg, nil)
	if err != nil {
		return address.Undef, xerrors.Errorf("pushing createMiner message: %w", err)
	}

	log.Infof("Pushed CreateMiner message: %s", signed.Cid())
	log.Infof("Waiting for confirmation")

	mw, err := api.StateWaitMsg(ctx, signed.Cid(), build.MessageConfidence, lapi.LookbackNoLimit, true)
	if err != nil {
		return address.Undef, xerrors.Errorf("waiting for createMiner message: %w", err)
	}

	if mw.Receipt.ExitCode != 0 {
		return address.Undef, xerrors.Errorf("create miner failed: exit code %d", mw.Receipt.ExitCode)
	}

	var retval power2.CreateMinerReturn
	if err := retval.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		return address.Undef, err
	}

	log.Infof("New miners address is: %s (%s)", retval.IDAddress, retval.RobustAddress)
	return retval.IDAddress, nil
}

func SyncWait(ctx context.Context, napi v0api.FullNode, watch bool) error {
	tick := time.Second / 4

	lastLines := 0
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	samples := 8
	i := 0
	var firstApp, app, lastApp uint64

	state, err := napi.SyncState(ctx)
	if err != nil {
		return err
	}
	firstApp = state.VMApplied

	for {
		state, err := napi.SyncState(ctx)
		if err != nil {
			return err
		}

		if len(state.ActiveSyncs) == 0 {
			time.Sleep(time.Second)
			continue
		}

		head, err := napi.ChainHead(ctx)
		if err != nil {
			return err
		}

		working := -1
		for i, ss := range state.ActiveSyncs {
			switch ss.Stage {
			case api.StageSyncComplete:
			default:
				working = i
			case api.StageIdle:
				// not complete, not actively working
			}
		}

		if working == -1 {
			working = len(state.ActiveSyncs) - 1
		}

		ss := state.ActiveSyncs[working]
		workerID := ss.WorkerID

		var baseHeight abi.ChainEpoch
		var target []cid.Cid
		var theight abi.ChainEpoch
		var heightDiff int64

		if ss.Base != nil {
			baseHeight = ss.Base.Height()
			heightDiff = int64(ss.Base.Height())
		}
		if ss.Target != nil {
			target = ss.Target.Cids()
			theight = ss.Target.Height()
			heightDiff = int64(ss.Target.Height()) - heightDiff
		} else {
			heightDiff = 0
		}

		for i := 0; i < lastLines; i++ {
			fmt.Print("\r\x1b[2K\x1b[A")
		}

		fmt.Printf("Worker: %d; Base: %d; Target: %d (diff: %d)\n", workerID, baseHeight, theight, heightDiff)
		fmt.Printf("State: %s; Current Epoch: %d; Todo: %d\n", ss.Stage, ss.Height, theight-ss.Height)
		lastLines = 2

		if i%samples == 0 {
			lastApp = app
			app = state.VMApplied - firstApp
		}
		if i > 0 {
			fmt.Printf("Validated %d messages (%d per second)\n", state.VMApplied-firstApp, (app-lastApp)*uint64(time.Second/tick)/uint64(samples))
			lastLines++
		}

		_ = target // todo: maybe print? (creates a bunch of line wrapping issues with most tipsets)

		if !watch && time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs) {
			fmt.Println("\nDone!")
			return nil
		}

		select {
		case <-ctx.Done():
			fmt.Println("\nExit by user")
			return nil
		case <-ticker.C:
		}

		i++
	}
}
