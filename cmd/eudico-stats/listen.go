package main

import (
	"context"
	"github.com/ipfs/go-cid"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cmd/eudico-stats/observer"
	cbor "github.com/ipfs/go-ipld-cbor"

	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
)

const Timeout = 76587687658765876
const Confidence = 0

type SubnetStat struct {
	// The nextEpoch to resync stats with the chain
	nextEpoch abi.ChainEpoch
	// The list of subnets currently managed by the current subnet
	subnets map[address.SubnetID]bool
	// The node to be stored in db
	node SubnetNode
	// bottomUpMsgCount to each subnet
	bottomUpMsgCount map[address.SubnetID]int
	// topDownMsgCount to each parent
	topDownMsgCount map[address.SubnetID]int
}

func emptySubnetStat(id address.SubnetID) SubnetStat {
	return SubnetStat{
		nextEpoch: 0,
		subnets:   make(map[address.SubnetID]bool),
		node: idOnlySubnetNode(id),
		bottomUpMsgCount: make(map[address.SubnetID]int),
		topDownMsgCount: make(map[address.SubnetID]int),
	}
}

// ShouldReSync checks if it is needed to resync subnet stats with the current chain data
func (s *SubnetStat) ShouldReSync(curH abi.ChainEpoch) bool {
	return s.nextEpoch < curH
}

// IncreButtomUpMsgCount increments the bottom up msg count
func (s *SubnetStat) IncreBottomUpMsgCount(id address.SubnetID, count int) int {
	c, ok := s.bottomUpMsgCount[id]
	if !ok {
		c = count
		
	} else {
		c += count
	}

	s.bottomUpMsgCount[id] = c

	return c
}

type EudicoStatsListener struct {
	Events *events.Events

	// Private fields
	api         v1api.FullNode
	observer    observer.Observer
	subnetStats map[address.SubnetID]SubnetStat
}

func NewEudicoStats(ctx context.Context, api v1api.FullNode, observer observer.Observer) (EudicoStatsListener, error) {
	eventListen, err := events.NewEvents(ctx, api)
	if err != nil {
		return EudicoStatsListener{}, err
	}

	listener := EudicoStatsListener{
		Events:      eventListen,
		api:         api,
		subnetStats: make(map[address.SubnetID]SubnetStat),
		observer:    observer,
	}

	log.Infow("Initialized eudico stats")

	return listener, nil
}

// Listen TODO: placeholder for future public invocation differences compared to `listen`
func (e *EudicoStatsListener) Listen(ctx context.Context, id address.SubnetID) error {
	return e.listen(ctx, id)
}

func (e *EudicoStatsListener) listen(
	ctx context.Context,
	id address.SubnetID,
) error {
	// TODO: ideally there is no need to add read write lock. Keep in view.

	if _, ok := e.subnetStats[id]; ok {
		log.Infow("subnet id already tracked", "id", id)
		return nil
	}

	log.Infow("starting listening to subnet", "id", id)
	e.subnetStats[id] = emptySubnetStat(id)
	e.observer.Observe(SubnetNodeAdded, id)

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		changes := states.(SubnetChanges)

		log.Infow("in change handler", "id", id, "changes", changes);

		if !changes.IsUpdated() {
			log.Debugw("subnet not updated", "id", id)
			return true, nil
		}

		e.listenToNewSubnets(ctx, &changes.NodeChanges)
		e.handleRelationshipChanges(&changes.RelationshipChanges)
		shouldStopListening := e.handleNodeChanges(&changes.NodeChanges)

		if shouldStopListening {
			log.Infow("stop listening to subnet", "id", id)
			return false, nil
		}

		log.Infow("continue listening to subnet", "id", id)
		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		log.Infow("in matching function", "id", id)
		var height = newTs.Height()

		if !e.ShouldReSync(id, height) {
			log.Debugw("subnet no need update", "id", id)
			return false, nil, nil
		}

		return e.matchSubnetStateChange(ctx, id, oldTs)
	}

	err := e.Events.StateChanged(checkFunc, changeHandler, revertHandler, Confidence, Timeout, match)
	if err != nil {
		return err
	}
	return nil
}

func (e *EudicoStatsListener) handleRelationshipChanges(changes *SubnetRelationshipChange) {
	if len(changes.Added) > 0 {
		log.Infow("to add relationship changes", "changes", changes.Added)
		e.observer.Observe(SubnetChildAdded, &changes.Added)
	}
	if len(changes.Removed) > 0 {
		log.Infow("to removed relationship changes", "changes", changes.Removed)
		e.observer.Observe(SubnetChildRemoved, &changes.Removed)
	}
}

func (e *EudicoStatsListener) handleNodeChanges(changes *NodeChange) bool {
	if !changes.IsNodeUpdated {
		log.Infow("node not updated", "id", changes.Node.SubnetID)
		return false
	}

	if changes.Node.Status == sca.Killed {
		log.Infow("node KILLED", "id", changes.Node.SubnetID)
		e.observer.Observe(SubnetNodeRemoved, changes.Node.SubnetID)
		return true
	}

	log.Infow("node updated", "id", changes.Node.SubnetID, "node", changes.Node)
	e.observer.Observe(SubnetNodeUpdated, &[]SubnetNode{changes.Node})

	return false
}

func (e *EudicoStatsListener) listenToNewSubnets(ctx context.Context, changes *NodeChange) {
	if len(changes.Added) == 0 {
		log.Infow("no new subnet added", "id", changes.Node.SubnetID)
		return
	}

	for _, node := range changes.Added {
		node := node
		go func() {
			err := e.listen(ctx, node.SubnetID)
			if err != nil {
				log.Errorw("cannot start listen to subnet", "id", node.SubnetID)
			} else {
				log.Infow("started listening to subnet", "id", node.SubnetID)
			}
		}()
	}
}

func (e *EudicoStatsListener) IsListening(id address.SubnetID) bool {
	_, ok := e.subnetStats[id]
	return ok
}

func (e *EudicoStatsListener) ShouldReSync(id address.SubnetID, height abi.ChainEpoch) bool {
	stats, _ := e.subnetStats[id]
	return stats.ShouldReSync(height)
}

func (e *EudicoStatsListener) detectAddedSubnetChildren(
	stats *SubnetStat,
	curmap *map[address.SubnetID]*sca.SubnetOutput,
	change *SubnetChanges,
) {
	for id, _ := range *curmap {
		if _, ok := stats.subnets[id]; !ok {
			log.Infow("new child added to subnet", "subnetId", stats.node.SubnetID, "child", id)
			change.RelationshipChanges.Add(stats.node.SubnetID, id)
			stats.subnets[id] = true
		}
	}
}

func (e *EudicoStatsListener) detectRemovedSubnetChildren(
	stats *SubnetStat,
	curSubnetMap *map[address.SubnetID]*sca.SubnetOutput,
	change *SubnetChanges,
) {
	for id, _ := range stats.subnets {
		if _, ok := (*curSubnetMap)[id]; !ok {
			log.Debugw("new child remove in subnet", "subnetId", stats.node.SubnetID, "child", id)
			change.RelationshipChanges.Remove(stats.node.SubnetID, id)
			delete(stats.subnets, id)
		}
	}
}

func (e *EudicoStatsListener) detectAddedSubnets(
	curmap *map[address.SubnetID]*sca.SubnetOutput,
	change *SubnetChanges,
) {
	for id, subnetOutput := range *curmap {
		if !e.IsListening(id) {
			log.Infow("subnet is not tracked by stats", "id", id, "node", fromSubnetOutput(id, subnetOutput, 0))
			// setting to 0 as we are not sure how many at this point, will trigger count as a separate go func
			change.NodeChanges.Add(fromSubnetOutput(id, subnetOutput, 0))
			continue
		}
	}
}

func (e *EudicoStatsListener) detectNodeChange(
	stats *SubnetStat,
	state *subnet.SubnetState,
	subnetCount int,
	change *SubnetChanges,
) {
	node := stats.node
	updated := false

	// TODO: what's the diff btw chain/consensus/hierarchical/actors/subnet/subnet_state.go:40
	// TODO and chain/consensus/hierarchical/actors/sca/sca_state.go:40
	if state != nil {
		status := convertStatus(state.Status)
		if node.Status != status {
			log.Infow("node status updated", "node.Status", node.Status, "Status", state.Status)
			updated = true
			node.Status = status
		}

		if node.Consensus != state.Consensus {
			log.Infow("node Consensus updated", "node.Consensus", node.Consensus, "Consensus", state.Consensus)
			updated = true
			node.Consensus = state.Consensus
		}

		if node.Stake != state.TotalStake.String() {
			log.Infow("node Stake updated", "node.Stake", node.Stake, "Stake", state.TotalStake.String())
			updated = true
			node.Stake = state.TotalStake.String()
		}
	}

	if node.SubnetCount != subnetCount {
		updated = true
		log.Infow("node count updated", "node.SubnetCount", node.SubnetCount, "count", subnetCount)
		node.SubnetCount = subnetCount
		log.Infow("node count after", "node.SubnetCount", node.SubnetCount, "count", subnetCount)
	}

	if updated {
		change.NodeChanges.UpdateNode(node)
		stats.node = node
	}
}

// detectChanges checks the current stats tracked subnet with that on chain. Returns the
// newly added subnets and also the ones to remove
func (e *EudicoStatsListener) detectChanges(
	ctx context.Context,
	sid address.SubnetID,
	subnetOutputs []sca.SubnetOutput,
	state *subnet.SubnetState,
	oldTs *types.TipSet,
 ) SubnetChanges {
	// stats should always exist
	stats, _ := e.subnetStats[sid]

	change := emptySubnetChanges(sid)

	latestSubnetMap := make(map[address.SubnetID]*sca.SubnetOutput)
	for _, output := range subnetOutputs {
		if output.Subnet.ID == sid {
			continue
		}
		latestSubnetMap[output.Subnet.ID] = &output
	}

	e.detectAddedSubnets(&latestSubnetMap, &change)
	e.detectAddedSubnetChildren(&stats, &latestSubnetMap, &change)
	e.detectRemovedSubnetChildren(&stats, &latestSubnetMap, &change)

	e.detectNodeChange(&stats, state, len(subnetOutputs), &change)

	e.detectCrossNetMsgChanges(ctx, sid, &stats, oldTs, &change.CrossNetChanges)
	e.subnetStats[sid] = stats

	return change
}

func (e *EudicoStatsListener) matchSubnetStateChange(ctx context.Context, id address.SubnetID, oldTs *types.TipSet) (bool, SubnetChanges, error) {
	subnets, err := e.api.ListSubnets(ctx, id)
	if err != nil {
		log.Warnw("list subnets failed", "id", id, "err", err)
		subnets = make([]sca.SubnetOutput, 0)
	}

	var snstPnt *subnet.SubnetState
	snst, err := e.obtainSubnetState(ctx, id)
	if err != nil {
		log.Warnw("cannot get subnet state", "subnet", id, "err", err)
		snstPnt = nil
	} else {
		snstPnt = &snst
	}

	changes := e.detectChanges(ctx, id, subnets, snstPnt, oldTs)
	log.Infow("change detection", "isUpdated", changes.IsUpdated(), "id", id)
	return changes.IsUpdated(), changes, nil
}

func (e *EudicoStatsListener) obtainSubnetState(ctx context.Context, id address.SubnetID) (subnet.SubnetState, error) {
	subnetActAddr := id.GetActor()

	// Get state of subnet actor in parent for heaviest tipset
	subnetAct, err := e.api.SubnetStateGetActor(ctx, id, subnetActAddr, types.EmptyTSK)
	if err != nil {
		log.Warnw("cannot get subnet actor", "subnet", id, "err", err)
		return subnet.SubnetState{}, err
	}

	var snst subnet.SubnetState
	pbs := blockstore.NewAPIBlockstore(e.api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &snst); err != nil {
		log.Warnw("cannot get subnet state", "subnet", id)
		return subnet.SubnetState{}, err
	}

	return snst, nil
}

func (e *EudicoStatsListener) obtainCrossNetMsgs(ctx context.Context, msgCid cid.Cid) (*adt.Array, error) {
	pbs := blockstore.NewAPIBlockstore(e.api)
	pcst := cbor.NewCborStore(pbs)
	wrapped := adt.WrapStore(ctx, pcst)
	return adt.AsArray(wrapped, msgCid, sca.CrossMsgsAMTBitwidth)
}

func (e *EudicoStatsListener) obtainStore(ctx context.Context, id address.SubnetID, ts *types.TipSet) adt.Store {
	bs := blockstore.NewAPIBlockstore(e.api)
	cst := cbor.NewCborStore(bs)
	return adt.WrapStore(ctx, cst)
}

func (e *EudicoStatsListener) obtainSCAState(ctx context.Context, id address.SubnetID, ts *types.TipSet, s *adt.Store) (sca.SCAState, error) {
	subnetActAddr := id.GetActor()

	// Get state of subnet actor in parent for heaviest tipset
	subnetAct, err := e.api.SubnetStateGetActor(ctx, id, subnetActAddr, ts.Key())
	if err != nil {
		log.Warnw("cannot get subnet actor", "subnet", id, "err", err)
		return sca.SCAState{}, err
	}

	var scast sca.SCAState

	if err := (*s).Get(ctx, subnetAct.Head, &scast); err != nil {
		return sca.SCAState{}, err
	}

	return scast, nil
}

// func (e *EudicoStatsListener) obtainBottomUpMsg(
// 	ctx context.Context,
// 	id address.SubnetID,
	
//  ) ([]*schema.CrossMsgMeta, error) {
// 	store := e.obtainStore(ctx, id, oldTs)

// 	oldScast, err := e.obtainSCAState(ctx, id, oldTs, &store)
// 	if err != nil {
// 		return make([]*schema.CrossMsgMeta, 0), err
// 	}

// 	oldNonce := oldScast.AppliedBottomUpNonce
	
// 	return oldScast.BottomUpMsgFromNonce(store, oldNonce)
// }

// func (e *EudicoStatsListener) detectTopDownMsgChanges(
// 	ctx context.Context,
// 	id address.SubnetID,
// 	ts *types.TipSet,
// 	store *adt.Store,
// 	change *CrossnetChanges,
// ) (error) {
// 	subnetActAddr := id.GetActor()

// 	// Get state of subnet actor in parent for heaviest tipset
// 	subnetAct, err := e.api.SubnetStateGetActor(ctx, id, subnetActAddr, ts.Key())
// 	if err != nil {
// 		log.Warnw("cannot get subnet actor", "subnet", id, "err", err)
// 		return err
// 	}

// 	var snst subnet.SubnetState
// 	if err := (*store).Get(ctx, subnetAct.Head, &snst); err != nil {
// 		log.Warnw("cannot get subnet state", "subnet", id)
// 		return subnet.SubnetState{}, err
// 	}
// }

func (e *EudicoStatsListener) detectBottomUpMsgChanges(
	ctx context.Context,
	id address.SubnetID,
	stats *SubnetStat,
	ts *types.TipSet,
	store *adt.Store,
	change *CrossnetRelationshipChange,
) (error) {
	state, err := e.obtainSCAState(ctx, id, ts, store)
	if err == nil {
		return err
	}

	msgMetas, err := state.BottomUpMsgFromNonce((*store), state.BottomUpNonce)
	if err != nil {
		return err
	}

	// 
	for _, msgMeta := range(msgMetas) {
		to, err := address.SubnetIDFromString(msgMeta.To)
		if err != nil {
			log.Errorw("cannot parse hc address", "address", to)
			continue
		}

		from, err := address.SubnetIDFromString(msgMeta.From)
		if err != nil {
			log.Errorw("cannot parse hc address", "address", from)
			continue
		}
		if from != id {
			log.Errorw("data inconsistent", "from", from, "expected", id)
			continue
		}

		c := stats.IncreBottomUpMsgCount(to, len(msgMeta.MsgsCid))
		change.Add(to, from, BottomUpType, c)
	}

	return nil
}

// TODO: old tipset nonce but new tipset key
func (e *EudicoStatsListener) detectCrossNetMsgChanges(
	ctx context.Context,
	id address.SubnetID,
	stats *SubnetStat,
	oldTs *types.TipSet,
	changes *CrossnetRelationshipChange,
) (error) {
	store := e.obtainStore(ctx, id, oldTs)

	e.detectBottomUpMsgChanges(ctx, id, stats, oldTs, &store, changes)

	return nil
}

func extractMapKey(id address.SubnetID, targetMap map[address.SubnetID]SubnetStat) []string {
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

func convertStatus(status subnet.Status) sca.Status {
	if status == subnet.Active {
		return sca.Active
	}
	if status == subnet.Killed {
		return sca.Killed
	}

	// default to Inactive as some status is not mapped
	return sca.Inactive
}
