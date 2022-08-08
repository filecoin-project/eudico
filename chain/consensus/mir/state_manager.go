package mir

import (
	"bytes"
	"context"
	"fmt"

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
	// Number of batches applied to the state.
	batchesApplied int

	// Length of an ISS segment.
	segmentLength int

	// The current epoch number.
	currentEpoch t.EpochNr

	// The first sequence number that belongs to the next epoch.
	// After having delivered this many batches, the app signals the end of an epoch.
	nextEpochSN int

	// For each epoch number, stores the corresponding membership.
	memberships []map[t.NodeID]t.NodeAddress

	// Channel to send messages to Eudico.
	ChainNotify chan []Messages

	MirManager *Manager

	reconfigurationVotes map[string]uint64
}

func NewStateManager(initialMembership map[t.NodeID]t.NodeAddress, segmentLength int, m *Manager) *StateManager {
	memberships := make([]map[t.NodeID]t.NodeAddress, ConfigOffset+2)
	for i := 0; i < ConfigOffset+2; i++ {
		memberships[i] = initialMembership
	}
	sm := StateManager{
		ChainNotify:          make(chan []Messages),
		MirManager:           m,
		memberships:          memberships,
		segmentLength:        SegmentLength,
		nextEpochSN:          len(initialMembership) * segmentLength,
		currentEpoch:         0,
		batchesApplied:       0,
		reconfigurationVotes: make(map[string]uint64),
	}
	return &sm
}

func (sm *StateManager) ApplyEvents(eventsIn *events.EventList) (*events.EventList, error) {
	return modules.ApplyEventsSequentially(eventsIn, sm.ApplyEvent)
}

func (sm *StateManager) ApplyEvent(event *eventpb.Event) (*events.EventList, error) {
	switch e := event.Type.(type) {
	case *eventpb.Event_Init:
	// no actions on init
	case *eventpb.Event_Deliver:
		if newEvents, err := sm.ApplyBatch(e.Deliver.Batch); err != nil {
			return nil, fmt.Errorf("app batch delivery error: %w", err)
		} else {
			return newEvents, nil
		}
	case *eventpb.Event_StateSnapshotRequest:
		data, memberships, err := sm.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("sm snapshot error: %w", err)
		}
		return (&events.EventList{}).PushBack(events.StateSnapshot(
			t.ModuleID(e.StateSnapshotRequest.Module),
			t.EpochNr(e.StateSnapshotRequest.Epoch),
			data,
			memberships,
		)), nil
	case *eventpb.Event_AppRestoreState:
		if err := sm.RestoreState(e.AppRestoreState.Snapshot); err != nil {
			return nil, fmt.Errorf("sm restore state error: %w", err)
		}
	default:
		return nil, fmt.Errorf("unexpected type of App event: %T", event.Type)
	}

	return events.EmptyList(), nil
}

// ApplyBatch applies the batch.
func (sm *StateManager) ApplyBatch(in *requestpb.Batch) (*events.EventList, error) {
	var msgs []Messages

	for _, req := range in.Requests {
		switch req.Req.Type {
		case TransportType:
			msgs = append(msgs, req.Req.Data)
		case ReconfigurationType:
			newValSet := &hierarchical.ValidatorSet{}
			err := newValSet.UnmarshalCBOR(bytes.NewReader(req.Req.Data))
			if err != nil {
				panic(err)
			}
			voted, err := sm.UpdateAndCheckValSetVotes(newValSet)
			if err != nil {
				panic(err)
			}
			if voted {
				err = sm.UpdateNextMembership(newValSet)
				if err != nil {
					panic(err)
				}
			}
		}
	}

	// Update counter of applied batches.
	sm.batchesApplied++

	// Send messages to Eudico.
	sm.ChainNotify <- msgs

	eventsOut := events.EmptyList()
	if sm.batchesApplied == sm.nextEpochSN {
		eventsOut = sm.EndEpoch()
	}

	return eventsOut, nil
}

// EndEpoch ends a Mir epoch.
func (sm *StateManager) EndEpoch() *events.EventList {
	eventsOut := events.EmptyList()
	fmt.Println("end of epoch")
	sm.currentEpoch++

	newMembership := sm.memberships[sm.currentEpoch+ConfigOffset]
	fmt.Println("new membership:")
	fmt.Println(newMembership)

	// Initialize new membership to be updated throughput the new epoch if reconfiguration transactions received.
	sm.memberships = append(sm.memberships, newMembership)

	fmt.Printf(">>> Starting epoch %v.\nMembership:\n%v\n", sm.currentEpoch, sm.memberships[sm.currentEpoch])

	sm.nextEpochSN += len(sm.memberships[sm.currentEpoch]) * sm.segmentLength

	eventsOut.PushBack(events.NewConfig("iss", getSortedKeys(newMembership)))

	err := sm.MirManager.ReconfigureMirNode(context.TODO(), newMembership)
	if err != nil {
		panic(err)
	}

	return eventsOut
}

func (sm *StateManager) UpdateNextMembership(valSet *hierarchical.ValidatorSet) error {
	_, mbs, err := ValidatorsMembership(valSet.GetValidators())
	if err != nil {
		return err
	}
	sm.memberships[sm.currentEpoch+ConfigOffset+1] = mbs
	return nil
}

// UpdateAndCheckValSetVotes votes for the valSet and returns true if it has had enough votes for this valSet.
func (sm *StateManager) UpdateAndCheckValSetVotes(valSet *hierarchical.ValidatorSet) (bool, error) {
	h, err := valSet.Hash()
	if err != nil {
		return false, err
	}
	sm.reconfigurationVotes[string(h)]++
	votes := int(sm.reconfigurationVotes[string(h)])
	if votes < f(len(sm.memberships[sm.currentEpoch]))+1 {
		return false, nil
	}
	return true, nil
}

// Snapshot returns a binary representation of the application state.
// The returned value can be passed to RestoreState().
// At the time of writing this comment, the Mir library does not support state transfer
// and Snapshot is never actually called.
// We include its implementation for completeness.
func (sm *StateManager) Snapshot() ([]byte, []map[t.NodeID]t.NodeAddress, error) {
	return nil, nil, nil
	return nil, sm.memberships[:len(sm.memberships)-1], nil
}

// RestoreState restores the application's state to the one represented by the passed argument.
// The argument is a binary representation of the application state returned from Snapshot().
// After the chat history is restored, RestoreState prints the whole chat history to stdout.
func (sm *StateManager) RestoreState(snapshot *commonpb.StateSnapshot) error {
	return nil
	sm.currentEpoch = t.EpochNr(snapshot.Epoch)
	sm.memberships = make([]map[t.NodeID]t.NodeAddress, len(snapshot.Memberships)+1)
	for e, mem := range snapshot.Memberships {
		sm.memberships[e] = make(map[t.NodeID]t.NodeAddress)
		for nID, nAddr := range mem.Membership {
			var err error
			sm.memberships[e][t.NodeID(nID)], err = multiaddr.NewMultiaddr(nAddr)
			if err != nil {
				return err
			}
		}
	}
	sm.memberships[len(sm.memberships)-1] = copyMap(sm.memberships[len(sm.memberships)-2])

	return nil
}

func (sm *StateManager) OrderedValidatorsAddresses() []t.NodeID {
	membership := sm.memberships[sm.currentEpoch]
	sortedIDs := getSortedKeys(membership)
	return sortedIDs
}

// The ImplementsModule method only serves the purpose of indicating that this is a Module and must not be called.
func (sm *StateManager) ImplementsModule() {}

func serializeMessages(messages []string) []byte {
	data := make([]byte, 0)
	for _, msg := range messages {
		data = append(data, []byte(msg)...)
		data = append(data, 0)
	}

	fmt.Printf("Serialized messages (%d): \n\n%s\n\n", len(messages), string(data))

	return data
}

func deserializeMessages(data []byte) []string {
	messages := make([]string, 0)

	if len(data) == 0 {
		return messages
	}

	for _, msg := range bytes.Split(data[:len(data)-1], []byte{0}) { // len(data)-1 to strip off the last zero byte
		messages = append(messages, string(msg))
	}

	fmt.Printf("Deserialized messages (%d): \n\n%s\n\n", len(messages), string(data))

	return messages
}

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

func f(n int) int {
	return (n - 1) / 3
}

func majority(n int) int {
	return 2*f(n) + 1
}
