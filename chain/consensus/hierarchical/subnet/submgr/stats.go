package submgr

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/observer"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
)

const MetricSubnets = "subnets"

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

// ShouldReSync checks if it is needed to resync subnet stats with the current chain data
func (s *SubnetStats) ShouldReSync(curH abi.ChainEpoch) bool {
	return s.nextEpoch < curH
}

type EudicoStats struct {
	Events    *events.Events
	SubnetAPI *Service

	// Private fields
	subnetStats  map[address.SubnetID]SubnetStats
	toStopListen map[address.SubnetID]bool
}

func (e *EudicoStats) Init(eventListen *events.Events, subnetAPI *Service) {
	e.Events = eventListen
	e.SubnetAPI = subnetAPI
	e.subnetStats = make(map[address.SubnetID]SubnetStats)
	e.toStopListen = make(map[address.SubnetID]bool)

	log.Infow("Initialized eudico stats")
}

func (e *EudicoStats) Listen(ctx context.Context, id address.SubnetID, epochThreshold abi.ChainEpoch, observerConfigs map[string]string) error {
	ob, err := observer.MakeObserver(observerConfigs)
	if err != nil {
		return err
	}
	return e.listen(ctx, id, epochThreshold, ob)
}

func (e *EudicoStats) listen(
	ctx context.Context,
	id address.SubnetID,
	epochThreshold abi.ChainEpoch,
	observer observer.Observer,
) error {
	if _, ok := e.subnetStats[id]; ok {
		log.Infow("subnet id already tracked", "id", id)
		return nil
	}

	log.Infow("starting listening to subnet", "id", id)
	e.subnetStats[id] = emptySubnetStats()

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		change := states.(SubnetChange)

		log.Infow("in change handler", "id", id)

		for _, subnetId := range change.added {
			if err == e.listen(ctx, subnetId, epochThreshold, observer) {
				log.Errorw("cannot listen subnet", "subnetId", subnetId, "err", err)
				continue
			}
			log.Infow("finished setup listen subnet", "id", id)
		}

		for subnetId, _ := range e.toStopListen {
			if id == subnetId {
				delete(e.toStopListen, subnetId)
				log.Infow("stop listening subnet", "subnetId", subnetId)
				return false, nil
			}
		}

		log.Infow("continue listening", "id", id)
		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		log.Infow("in matching function", "id", id)

		var height = newTs.Height()

		// this should not have happened, just a sanity check perhaps
		if !e.IsListening(id) {
			log.Errorw("subnet listening not initiated", "id", id)
			return false, nil, nil
		}

		if !e.ShouldReSync(id, height) {
			log.Infow("subnet no need update", "id", id)
			return false, nil, nil
		}

		subnets, err := e.SubnetAPI.ListSubnets(ctx, id)
		if err != nil {
			log.Errorw("list subnets failed", "id", id, "err", err)
			return false, nil, err
		}

		changes := e.SyncSubnets(id, subnets)
		for _, subnetID := range changes.removed {
			e.toStopListen[subnetID] = true
		}

		if changes.IsUpdated() {
			log.Infow("subnets updated", "id", id, "add", changes.added, "remove", changes.removed)
			observer.Observe(MetricSubnets, id.String(), extractMapKey(id, e.subnetStats))
		}

		return changes.IsUpdated(), changes, nil
	}

	err := e.Events.StateChanged(checkFunc, changeHandler, revertHandler, 0, Timeout, match)
	if err != nil {
		return err
	}
	return nil
}

func (e *EudicoStats) IsListening(id address.SubnetID) bool {
	_, ok := e.subnetStats[id]
	return ok
}

func (e *EudicoStats) ShouldReSync(id address.SubnetID, height abi.ChainEpoch) bool {
	stats, _ := e.subnetStats[id]
	return stats.ShouldReSync(height)
}

// SyncSubnets checks the current stats tracked subnet with that on chain. Returns the
// newly added subnets and also the ones to remove
func (e *EudicoStats) SyncSubnets(id address.SubnetID, subnetIds []sca.SubnetOutput) SubnetChange {
	stats, _ := e.subnetStats[id]

	change := SubnetChange{added: make([]address.SubnetID, 0, 4), removed: make([]address.SubnetID, 0)}

	curmap := make(map[address.SubnetID]bool)
	for _, output := range subnetIds {
		if output.Subnet.ID == id {
			continue
		}
		if _, ok := stats.subnets[output.Subnet.ID]; !ok {
			change.Add(output.Subnet.ID)
		}

		curmap[output.Subnet.ID] = true
	}

	for id, _ := range stats.subnets {
		if _, ok := curmap[id]; !ok {
			change.Remove(id)
		}
	}

	if change.IsUpdated() {
		stats.subnets = curmap
		e.subnetStats[id] = stats
	}

	return change
}

func extractMapKey(id address.SubnetID, targetMap map[address.SubnetID]SubnetStats) []string {
	mmap, ok := targetMap[id]
	if !ok {
		return make([]string, 0)
	}

	keys := make([]string, len(mmap.subnets))

	i := 0
	for k, _ := range mmap.subnets {
		keys[i] = k.String()
		i++
	}

	return keys
}
