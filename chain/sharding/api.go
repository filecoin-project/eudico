package sharding

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/impl/client"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/libp2p/go-libp2p-core/host"
)

// PopulateAPIs populates the impl/fullNode for a shard.
// NOTE: We may be able to do this with DI in the future.
func (sh *Shard) populateAPIs(
	parentAPI *impl.FullNodeAPI,
	h host.Host,
	tsExec stmgr.Executor, // Tipset Executor of the consensus used for shard.
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
	}

	syncAPI := full.SyncAPI{
		Syncer:  sh.syncer,
		PubSub:  sh.pubsub,
		NetName: dtypes.NetworkName(sh.netName),
	}

	// NOTE: From here we are inheriting APIs from the parent shard.
	// This can bee seen as placeholder that we can override if needed
	// for shard functionality.
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

	sh.api = &impl.FullNodeAPI{
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
		NetworkName: dtypes.NetworkName(sh.netName),
	}

	// Register API so it can be accessed from CLI
	return sh.nodeServer(sh.ID, sh.api)
}
