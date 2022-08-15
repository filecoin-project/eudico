package hierarchical

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/submgr"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl/client"
	commonapi "github.com/filecoin-project/lotus/node/impl/common"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/impl/market"
	"github.com/filecoin-project/lotus/node/impl/net"
	"github.com/filecoin-project/lotus/node/impl/paych"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/fx"

	"github.com/filecoin-project/go-address"
)

var log = logging.Logger("eudico-stats")

const Timeout = 76587687658765876

type SubnetChange struct {
	added   []address.SubnetID
	removed []address.SubnetID
}

type SubnetStats struct {
	// The nextEpoch to resync stats with the chain
	nextEpoch abi.ChainEpoch
	// The list of subnets currently managed by the current subnet
	subnets map[address.SubnetID]bool
}

func emptySubnetStats() SubnetStats {
	return SubnetStats{
		nextEpoch: 0,
		subnets:   make(map[address.SubnetID]bool),
	}
}

func (s *SubnetChange) Add(id address.SubnetID) {
	s.added = append(s.added, id)
}

func (s *SubnetChange) Remove(id address.SubnetID) {
	s.removed = append(s.removed, id)
}

func (s *SubnetChange) IsUpdated() bool {
	return len(s.removed) != 0 || len(s.added) != 0
}

// SyncSubnets checks the current stats tracked subnet with that on chain. Returns the
// newly added subnets and also the ones to remove
func (s *SubnetStats) SyncSubnets(subnetIds []sca.SubnetOutput) SubnetChange {
	change := SubnetChange{added: make([]address.SubnetID, 0, 4), removed: make([]address.SubnetID, 0)}

	curmap := make(map[address.SubnetID]bool)
	for _, output := range subnetIds {
		if _, ok := s.subnets[output.Subnet.ID]; !ok {
			change.Add(output.Subnet.ID)
		}

		curmap[output.Subnet.ID] = true
	}

	for id, _ := range s.subnets {
		if _, ok := curmap[id]; !ok {
			change.Remove(id)
		}
	}

	if change.IsUpdated() {
		s.subnets = curmap
	}

	return change
}

// ShouldReSync checks if it is needed to resync subnet stats with the current chain data
func (s *SubnetStats) ShouldReSync(curH abi.ChainEpoch) bool {
	return s.nextEpoch < curH
}

var _ api.EudicoStats = &EudicoStats{}

type EudicoStats struct {
	Events    *events.Events
	SubnetAPI *submgr.Service

	// Private fields
	subnetStats  map[address.SubnetID]SubnetStats
	toStopListen map[address.SubnetID]bool
}

func NewEudicoStats(
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
	beaconapi full.BeaconAPI) (EudicoStats, error) {

	stats := EudicoStats{}

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
		&stats,
		s,
	}

	// Starting subnetSub to listen to events in the root chain.
	var err error
	eventListen, err := events.NewEvents(ctx, subAPI)
	if err != nil {
		return EudicoStats{}, err
	}

	log.Infow("Creating eudico stats")

	stats.Events = eventListen
	stats.SubnetAPI = s
	stats.subnetStats = make(map[address.SubnetID]SubnetStats)
	stats.toStopListen = make(map[address.SubnetID]bool)
	return stats, nil
}

func (a *EudicoStats) Listen(ctx context.Context, id address.SubnetID, epochThreshold abi.ChainEpoch) error {
	if _, ok := a.subnetStats[id]; ok {
		log.Infow("subnet id already tracked", "id", id)
		return nil
	}

	log.Infow("starting listening to subnet", "id", id)
	a.subnetStats[id] = emptySubnetStats()
	return a.listen(ctx, id, epochThreshold)
}

func (a *EudicoStats) listen(
	ctx context.Context,
	id address.SubnetID,
	epochThreshold abi.ChainEpoch,
) error {
	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		change := states.(SubnetChange)

		for _, subnetId := range change.added {
			if err == a.Listen(ctx, subnetId, epochThreshold) {
				log.Errorw("cannot listen subnet", "subnetId", subnetId, "err", err)
				continue
			}
		}

		for subnetId, _ := range a.toStopListen {
			if id == subnetId {
				delete(a.toStopListen, subnetId)
				log.Infow("stop listening subnet", "subnetId", subnetId)
				return false, nil
			}
		}

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		var height = newTs.Height()

		stats, ok := a.subnetStats[id]
		// this should not have happened, just a sanity check perhaps
		if !ok {
			log.Errorw("subnet tracking not initiated", "id", id)
			return false, nil, nil
		}

		if !stats.ShouldReSync(height) {
			return false, nil, nil
		}

		subnets, err := a.SubnetAPI.ListSubnets(ctx, id)
		if err != nil {
			return false, nil, err
		}

		changes := stats.SyncSubnets(subnets)
		for _, subnetID := range changes.removed {
			a.toStopListen[subnetID] = true
		}
		return changes.IsUpdated(), changes, nil
	}

	err := a.Events.StateChanged(checkFunc, changeHandler, revertHandler, 20, Timeout, match)
	if err != nil {
		return err
	}
	return nil
}
