package mir

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/multiformats/go-multiaddr"
	"go4.org/sort"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/pb/commonpb"
	"github.com/filecoin-project/mir/pkg/pb/eventpb"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	t "github.com/filecoin-project/mir/pkg/types"
)

type Messages []byte

type StateManager struct {

	// The current epoch number.
	currentEpoch t.EpochNr

	// For each epoch number, stores the corresponding membership.
	memberships    []map[t.NodeID]t.NodeAddress
	membershipLock sync.Mutex

	// Channel to send messages to Eudico.
	ChainNotify chan []Messages

	MirManager *Manager

	// TODO: The vote counting is leaking memory. Resolve that in garbage collection mechanism.
	reconfigurationVotes map[string]int
}

func NewStateManager(initialMembership map[t.NodeID]t.NodeAddress, m *Manager) *StateManager {
	// Initialize the membership for the first epochs.
	// We use configOffset+2 memberships to account for:
	// - The first epoch (epoch 0)
	// - The configOffset epochs that already have a fixed membership (epochs 1 to configOffset)
	// - The membership of the following epoch (configOffset+1) initialized with the same membership,
	//   but potentially replaced during the first epoch (epoch 0) through special configuration requests.
	memberships := make([]map[t.NodeID]t.NodeAddress, ConfigOffset+2)
	for i := 0; i < ConfigOffset+2; i++ {
		memberships[i] = initialMembership
	}
	sm := StateManager{
		ChainNotify:          make(chan []Messages),
		MirManager:           m,
		memberships:          memberships,
		currentEpoch:         0,
		reconfigurationVotes: make(map[string]int),
	}
	return &sm
}

func (sm *StateManager) ApplyEvents(eventsIn *events.EventList) (*events.EventList, error) {
	return modules.ApplyEventsSequentially(eventsIn, sm.ApplyEvent)
}

func (sm *StateManager) ApplyEvent(event *eventpb.Event) (*events.EventList, error) {
	switch e := event.Type.(type) {
	case *eventpb.Event_Init:
		return events.EmptyList(), nil
	case *eventpb.Event_Deliver:
		return sm.applyBatch(e.Deliver.Batch)
	case *eventpb.Event_NewEpoch:
		return sm.applyNewEpoch(e.NewEpoch)
	case *eventpb.Event_AppSnapshotRequest:
		return sm.applySnapshotRequest(e.AppSnapshotRequest)
	case *eventpb.Event_AppRestoreState:
		return sm.applyRestoreState(e.AppRestoreState.Snapshot)
	default:
		return nil, fmt.Errorf("unexpected type of App event: %T", event.Type)
	}
}

// applyBatch applies a batch of requests to the state of the application.
func (sm *StateManager) applyBatch(in *requestpb.Batch) (*events.EventList, error) {
	var msgs []Messages

	for _, req := range in.Requests {
		switch req.Req.Type {
		case TransportType:
			msgs = append(msgs, req.Req.Data)
		case ReconfigurationType:
			err := sm.applyConfigMsg(req.Req)
			if err != nil {
				return events.EmptyList(), err
			}
		}
	}

	// Send messages to the Eudico node.
	sm.ChainNotify <- msgs

	return events.EmptyList(), nil
}

func (sm *StateManager) applyConfigMsg(in *requestpb.Request) error {
	fmt.Printf("%v - applyConfigMsg, memb len - %d\n", sm.MirManager.MirID, len(sm.memberships))
	newValSet := &hierarchical.ValidatorSet{}
	if err := newValSet.UnmarshalCBOR(bytes.NewReader(in.Data)); err != nil {
		return err
	}
	voted, err := sm.UpdateAndCheckVotes(newValSet)
	if err != nil {
		return err
	}
	if voted {
		err = sm.UpdateNextMembership(newValSet)
		if err != nil {
			return err
		}
	}
	return nil
}

func (sm *StateManager) applyNewEpoch(newEpoch *eventpb.NewEpoch) (*events.EventList, error) {
	sm.membershipLock.Lock()
	defer sm.membershipLock.Unlock()

	// Sanity check.
	if t.EpochNr(newEpoch.EpochNr) != sm.currentEpoch+1 {
		return nil, fmt.Errorf("expected next epoch to be %d, got %d", sm.currentEpoch+1, newEpoch.EpochNr)
	}

	// The base membership is the last one membership.
	newMembership := sm.memberships[newEpoch.EpochNr+ConfigOffset]

	// Append a new membership data structure to be modified throughout the new epoch.
	sm.memberships = append(sm.memberships, newMembership)

	// Update current epoch number.
	sm.currentEpoch = t.EpochNr(newEpoch.EpochNr)

	err := sm.MirManager.ReconfigureMirNode(context.TODO(), newMembership)
	if err != nil {
		return events.EmptyList(), err
	}

	// Notify ISS about the new membership.
	return events.ListOf(events.NewConfig("iss", copyMap(newMembership))), nil
}

func (sm *StateManager) UpdateNextMembership(valSet *hierarchical.ValidatorSet) error {
	sm.membershipLock.Lock()
	defer sm.membershipLock.Unlock()

	_, mbs, err := ValidatorsMembership(valSet.GetValidators())
	if err != nil {
		return err
	}
	sm.memberships[sm.currentEpoch+ConfigOffset+1] = mbs
	return nil
}

// UpdateAndCheckVotes votes for the valSet and returns true if it has had enough votes for this valSet.
func (sm *StateManager) UpdateAndCheckVotes(valSet *hierarchical.ValidatorSet) (bool, error) {
	sm.membershipLock.Lock()
	defer sm.membershipLock.Unlock()

	h, err := valSet.Hash()
	if err != nil {
		return false, err
	}
	sm.reconfigurationVotes[string(h)]++
	votes := sm.reconfigurationVotes[string(h)]
	nodes := len(sm.memberships[sm.currentEpoch])
	if votes < weakQuorum(nodes) {
		return false, nil
	}
	return true, nil
}

// applySnapshotRequest produces a StateSnapshotResponse event containing the current snapshot of the chat app state.
// The snapshot is a binary representation of the application state that can be passed to applyRestoreState().
func (sm *StateManager) applySnapshotRequest(snapshotRequest *eventpb.AppSnapshotRequest) (*events.EventList, error) {
	sm.membershipLock.Lock()
	defer sm.membershipLock.Unlock()

	return events.ListOf(events.AppSnapshotResponse(
		t.ModuleID(snapshotRequest.Module),
		nil,
		snapshotRequest.Origin,
	)), nil
}

// applyRestoreState restores the application's state to the one represented by the passed argument.
// The argument is a binary representation of the application state returned from Snapshot().
func (sm *StateManager) applyRestoreState(snapshot *commonpb.StateSnapshot) (*events.EventList, error) {
	sm.membershipLock.Lock()
	defer sm.membershipLock.Unlock()

	sm.currentEpoch = t.EpochNr(snapshot.Configuration.EpochNr)
	sm.memberships = make([]map[t.NodeID]t.NodeAddress, len(snapshot.Configuration.Memberships))
	for e, mem := range snapshot.Configuration.Memberships {
		sm.memberships[e] = make(map[t.NodeID]t.NodeAddress)
		for nID, nAddr := range mem.Membership {
			var err error
			sm.memberships[e][t.NodeID(nID)], err = multiaddr.NewMultiaddr(nAddr)
			if err != nil {
				return nil, err
			}
		}
	}

	newMembership := copyMap(sm.memberships[snapshot.Configuration.EpochNr+ConfigOffset])
	sm.memberships = append(sm.memberships, newMembership)

	return events.EmptyList(), nil
}

func (sm *StateManager) OrderedValidatorsAddresses() []t.NodeID {
	sm.membershipLock.Lock()
	defer sm.membershipLock.Unlock()

	membership := sm.memberships[sm.currentEpoch]
	sortedIDs := getSortedKeys(membership)
	return sortedIDs
}

// The ImplementsModule method only serves the purpose of indicating that this is a Module and must not be called.
func (sm *StateManager) ImplementsModule() {}

func getSortedKeys(m map[t.NodeID]t.NodeAddress) []t.NodeID {
	skeys := make([]string, len(m))
	i := 0
	for k := range m {
		skeys[i] = k.Pb()
		i++
	}

	sort.Strings(skeys)

	keys := make([]t.NodeID, len(m))
	for k := range skeys {
		keys[k] = t.NodeID(skeys[k])
	}

	return keys
}

func copyMap(m map[t.NodeID]t.NodeAddress) map[t.NodeID]t.NodeAddress {
	newMap := make(map[t.NodeID]t.NodeAddress, len(m))
	for k, v := range m {
		newMap[k] = v
	}
	return newMap
}

func maxFaulty(n int) int {
	// assuming n > 3f:
	//   return max f
	return (n - 1) / 3
}

func weakQuorum(n int) int {
	// assuming n > 3f:
	//   return min q: q > f
	return maxFaulty(n) + 1
}
