package mir

import (
	"fmt"

	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/pb/eventpb"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	t "github.com/filecoin-project/mir/pkg/types"
)

const (
	ReconfigurationBatchNumber = 32
)

type Tx []byte

type StateManager struct {
	ChainNotify  chan []Tx
	BatchCounter uint
	Api          *Manager
}

func NewStateManager(m *Manager) *StateManager {
	sm := StateManager{
		ChainNotify: make(chan []Tx),
		Api:         m,
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
		if err := sm.ApplyBatch(e.Deliver.Batch); err != nil {
			return nil, fmt.Errorf("sm batch delivery error: %w", err)
		}
	case *eventpb.Event_AppSnapshotRequest:
		data, err := sm.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("sm snapshot error: %w", err)
		}
		return (&events.EventList{}).PushBack(events.AppSnapshot(
			t.ModuleID(e.AppSnapshotRequest.Module),
			t.EpochNr(e.AppSnapshotRequest.Epoch),
			data,
		)), nil
	case *eventpb.Event_AppRestoreState:
		if err := sm.RestoreState(e.AppRestoreState.Data); err != nil {
			return nil, fmt.Errorf("sm restore state error: %w", err)
		}
	default:
		return nil, fmt.Errorf("unexpected type of App event: %T", event.Type)
	}

	return &events.EventList{}, nil
}

// ApplyBatch sends a batch consisting of data only to Eudico.
func (sm *StateManager) ApplyBatch(in *requestpb.Batch) error {
	var out []Tx

	for _, req := range in.Requests {
		out = append(out, req.Req.Data)
	}

	sm.ChainNotify <- out
	sm.BatchCounter++

	if sm.BatchCounter%ReconfigurationBatchNumber == 0 {
		err := sm.Api.CreateMirNode()
		if err != nil {
			return err
		}
	}

	return nil
}

// Snapshot returns a binary representation of the application state.
// The returned value can be passed to RestoreState().
// At the time of writing this comment, the Mir library does not support state transfer
// and Snapshot is never actually called.
// We include its implementation for completeness.
func (sm *StateManager) Snapshot() ([]byte, error) {
	return nil, nil
}

// RestoreState restores the application's state to the one represented by the passed argument.
// The argument is a binary representation of the application state returned from Snapshot().
// After the chat history is restored, RestoreState prints the whole chat history to stdout.
func (sm *StateManager) RestoreState(snapshot []byte) error {
	return nil
}

// The ImplementsModule method only serves the purpose of indicating that this is a Module and must not be called.
func (sm *StateManager) ImplementsModule() {}
