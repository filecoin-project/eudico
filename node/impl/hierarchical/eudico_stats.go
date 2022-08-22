package hierarchical

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/submgr"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/node/impl/client"
	commonapi "github.com/filecoin-project/lotus/node/impl/common"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/impl/market"
	"github.com/filecoin-project/lotus/node/impl/net"
	"github.com/filecoin-project/lotus/node/impl/paych"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"go.uber.org/fx"
)

type EudicoStatsAPI struct {
	eudicoStats submgr.EudicoStats
}

func NewEudicoStatsAPI(
	s *submgr.Service,
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	ds dtypes.MetadataDS,
	commonapi commonapi.CommonAPI,
	netapi net.NetAPI,
	chainapi full.ChainAPI,
	clientapi client.API,
	mpoolapi full.MpoolAPI,
	gasapi full.GasAPI,
	marketapi market.MarketAPI,
	paychapi paych.PaychAPI,
	stateapi full.StateAPI,
	msigapi full.MsigAPI,
	walletapi full.WalletAPI,
	netName dtypes.NetworkName,
	syncapi full.SyncAPI,
	beaconapi full.BeaconAPI) (EudicoStatsAPI, error) {

	eudicoStats := submgr.EudicoStats{}

	ctx := helpers.LifecycleCtx(mctx, lc)

	subAPI := &submgr.API{
		commonapi,
		netapi,
		chainapi,
		clientapi,
		mpoolapi,
		gasapi,
		marketapi,
		paychapi,
		stateapi,
		msigapi,
		walletapi,
		syncapi,
		beaconapi,
		ds,
		netName,
		&eudicoStats,
		s,
	}

	// Starting subnetSub to listen to events in the root chain.
	var err error
	eventListen, err := events.NewEvents(ctx, subAPI)
	if err != nil {
		return EudicoStatsAPI{}, err
	}

	eudicoStats.Init(eventListen, s)

	return EudicoStatsAPI{eudicoStats}, nil
}

func (e *EudicoStatsAPI) Listen(ctx context.Context, id address.SubnetID, epochThreshold abi.ChainEpoch, observerConfig map[string]string) error {
	return e.eudicoStats.Listen(ctx, id, epochThreshold, observerConfig)
}
