package subnetmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/metrics/proxy"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl/client"
	"github.com/filecoin-project/lotus/node/impl/common"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/impl/market"
	"github.com/filecoin-project/lotus/node/impl/net"
	"github.com/filecoin-project/lotus/node/impl/paych"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/lp2p"
)

type API struct {
	common.CommonAPI
	net.NetAPI
	full.ChainAPI
	client.API
	full.MpoolAPI
	full.GasAPI
	market.MarketAPI
	paych.PaychAPI
	full.StateAPI
	full.MsigAPI
	full.WalletAPI
	full.SyncAPI
	full.BeaconAPI

	DS          dtypes.MetadataDS
	NetworkName dtypes.NetworkName

	*SubnetMgr
}

func (n *API) getActorState(ctx context.Context, SubnetActor address.Address) (*subnet.SubnetState, error) {
	act, err := n.StateGetActor(ctx, SubnetActor, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	var st subnet.SubnetState
	bs := blockstore.NewAPIBlockstore(n)
	cst := cbor.NewCborStore(bs)
	if err := cst.Get(ctx, act.Head, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// PopulateAPIs populates the impl/fullNode for a subnet.
// NOTE: We may be able to do this with DI in the future.
func (sh *Subnet) populateAPIs(
	parentAPI *API,
	h host.Host,
	tsExec stmgr.Executor, // Tipset Executor of the consensus used for subnet.
) error {
	// Reusing walletAPI from parent chain
	walletAPI := parentAPI.WalletAPI
	stateAPI := full.StateAPI{
		Wallet:    parentAPI.StateAPI.Wallet,
		DefWallet: parentAPI.DefWallet,
		StateModuleAPI: &full.StateModule{
			StateManager: sh.sm,
			Chain:        sh.ch,
		},
		ProofVerifier: parentAPI.ProofVerifier,
		StateManager:  sh.sm,
		Chain:         sh.ch,
		Beacon:        parentAPI.StateAPI.Beacon,
		Consensus:     sh.cons,
		TsExec:        tsExec,
	}

	chainAPI := full.ChainAPI{
		WalletAPI: walletAPI,
		ChainModuleAPI: &full.ChainModule{
			Chain:             sh.ch,
			ExposedBlockstore: sh.bs,
		},
		Chain:  sh.ch,
		TsExec: tsExec,
		// TODO: Handle right blockstore for each purpose
		// in the future.
		ExposedBlockstore: sh.bs,
		BaseBlockstore:    sh.bs,
	}

	pcache := full.NewGasPriceCache()
	gasAPI := full.GasAPI{
		Stmgr:      sh.sm,
		Chain:      sh.ch,
		Mpool:      sh.mpool,
		PriceCache: pcache,
		GasModuleAPI: &full.GasModule{
			Stmgr:      sh.sm,
			Chain:      sh.ch,
			Mpool:      sh.mpool,
			PriceCache: pcache,
			GetMaxFee:  func() (abi.TokenAmount, error) { return types.BigInt(config.DefaultDefaultMaxFee), nil },
		},
	}

	mpoolAPI := full.MpoolAPI{
		MpoolModuleAPI: &full.MpoolModule{
			Mpool: sh.mpool,
		},
		WalletAPI:     walletAPI,
		GasAPI:        gasAPI,
		MessageSigner: messagesigner.NewMessageSigner(parentAPI.StateAPI.Wallet, sh.mpool, sh.ds),
		PushLocks:     &dtypes.MpoolLocker{},
	}

	syncAPI := full.SyncAPI{
		Syncer:  sh.syncer,
		PubSub:  sh.pubsub,
		NetName: dtypes.NetworkName(sh.ID.String()),
	}

	// NOTE: From here we are inheriting APIs from the parent subnet.
	// This can bee seen as placeholder that we can override if needed
	// for subnet functionality.
	msigAPI := parentAPI.MsigAPI
	paychAPI := parentAPI.PaychAPI
	marketAPI := parentAPI.MarketAPI
	clientAPI := client.API{
		ChainAPI:                  chainAPI,
		WalletAPI:                 walletAPI,
		PaychAPI:                  paychAPI,
		StateAPI:                  stateAPI,
		SMDealClient:              parentAPI.SMDealClient,
		Chain:                     sh.ch,
		RetDiscovery:              parentAPI.RetDiscovery,
		Retrieval:                 parentAPI.Retrieval,
		Imports:                   parentAPI.Imports,
		StorageBlockstoreAccessor: parentAPI.StorageBlockstoreAccessor,
		Repo:                      parentAPI.Repo,
		DataTransfer:              parentAPI.DataTransfer,
		Host:                      h,
		RtvlBlockstoreAccessor:    parentAPI.RtvlBlockstoreAccessor,
	}

	sh.api = &API{
		CommonAPI:   parentAPI.CommonAPI,
		NetAPI:      parentAPI.NetAPI,
		ChainAPI:    chainAPI,
		API:         clientAPI,
		MpoolAPI:    mpoolAPI,
		GasAPI:      gasAPI,
		MarketAPI:   marketAPI,
		PaychAPI:    paychAPI,
		StateAPI:    stateAPI,
		MsigAPI:     msigAPI,
		WalletAPI:   walletAPI,
		SyncAPI:     syncAPI,
		BeaconAPI:   parentAPI.BeaconAPI,
		DS:          sh.ds,
		NetworkName: dtypes.NetworkName(sh.ID.String()),
		SubnetMgr:   parentAPI.SubnetMgr,
	}

	// Register API so it can be accessed from CLI
	return sh.nodeServer(sh.ID.String(), sh.api)
}

func (n *API) CreateBackup(ctx context.Context, fpath string) error {
	panic("backup not implemented for wrapped abstraction")
}

func (n *API) NodeStatus(ctx context.Context, inclChainStatus bool) (status api.NodeStatus, err error) {
	curTs, err := n.ChainHead(ctx)
	if err != nil {
		return status, err
	}

	status.SyncStatus.Epoch = uint64(curTs.Height())
	timestamp := time.Unix(int64(curTs.MinTimestamp()), 0)
	delta := time.Since(timestamp).Seconds()
	status.SyncStatus.Behind = uint64(delta / 30)

	// get peers in the messages and blocks topics
	peersMsgs := make(map[peer.ID]struct{})
	peersBlocks := make(map[peer.ID]struct{})

	for _, p := range n.PubSub.ListPeers(build.MessagesTopic(n.NetworkName)) {
		peersMsgs[p] = struct{}{}
	}

	for _, p := range n.PubSub.ListPeers(build.BlocksTopic(n.NetworkName)) {
		peersBlocks[p] = struct{}{}
	}

	// get scores for all connected and recent peers
	scores, err := n.NetPubsubScores(ctx)
	if err != nil {
		return status, err
	}

	for _, score := range scores {
		if score.Score.Score > lp2p.PublishScoreThreshold {
			_, inMsgs := peersMsgs[score.ID]
			if inMsgs {
				status.PeerStatus.PeersToPublishMsgs++
			}

			_, inBlocks := peersBlocks[score.ID]
			if inBlocks {
				status.PeerStatus.PeersToPublishBlocks++
			}
		}
	}

	if inclChainStatus && status.SyncStatus.Epoch > uint64(build.Finality) {
		blockCnt := 0
		ts := curTs

		for i := 0; i < 100; i++ {
			blockCnt += len(ts.Blocks())
			tsk := ts.Parents()
			ts, err = n.ChainGetTipSet(ctx, tsk)
			if err != nil {
				return status, err
			}
		}

		status.ChainStatus.BlocksPerTipsetLast100 = float64(blockCnt) / 100

		for i := 100; i < int(build.Finality); i++ {
			blockCnt += len(ts.Blocks())
			tsk := ts.Parents()
			ts, err = n.ChainGetTipSet(ctx, tsk)
			if err != nil {
				return status, err
			}
		}

		status.ChainStatus.BlocksPerTipsetLastFinality = float64(blockCnt) / float64(build.Finality)

	}

	return status, nil
}

// The next two functions are exact copies from node/rpc.go to be able to handle subnet APIs
// as if they were FullNode API implementations.
// FullNodeHandler returns a full node handler, to be mounted as-is on the server.
func FullNodeHandler(prefix string, a v1api.FullNode, permissioned bool, opts ...jsonrpc.ServerOption) (http.Handler, error) {
	m := mux.NewRouter()

	serveRpc := func(path string, hnd interface{}) {
		rpcServer := jsonrpc.NewServer(opts...)
		rpcServer.Register("Filecoin", hnd)

		var handler http.Handler = rpcServer
		if permissioned {
			handler = &auth.Handler{Verify: a.AuthVerify, Next: rpcServer.ServeHTTP}
		}

		m.Handle(path, handler)
	}

	fnapi := proxy.MetricedFullAPI(a)
	if permissioned {
		fnapi = api.PermissionedFullAPI(fnapi)
	}

	serveRpc(path.Join("/", prefix, "/rpc/v1"), fnapi)
	serveRpc(path.Join("/", prefix, "/rpc/v0"), &v0api.WrapperV1Full{FullNode: fnapi})

	// Import handler
	handleImportFunc := handleImport(a.(*API))
	if permissioned {
		importAH := &auth.Handler{
			Verify: a.AuthVerify,
			Next:   handleImportFunc,
		}
		m.Handle(prefix+"/rest/v0/import", importAH)
	} else {
		m.HandleFunc(prefix+"/rest/v0/import", handleImportFunc)
	}

	return m, nil
}

func handleImport(a *API) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			w.WriteHeader(404)
			return
		}
		if !auth.HasPerm(r.Context(), nil, api.PermWrite) {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(struct{ Error string }{"unauthorized: missing write permission"})
			return
		}

		c, err := a.ClientImportLocal(r.Context(), r.Body)
		if err != nil {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(struct{ Error string }{err.Error()})
			return
		}
		w.WriteHeader(200)
		err = json.NewEncoder(w).Encode(struct{ Cid cid.Cid }{c})
		if err != nil {
			log.Errorf("/rest/v0/import: Writing response failed: %+v", err)
			return
		}
	}
}
