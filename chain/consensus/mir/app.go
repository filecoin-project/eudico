package mir

import (
	"fmt"

	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/pb/eventpb"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	t "github.com/filecoin-project/mir/pkg/types"
)

type Tx []byte

type Application struct {
	ChainNotify chan []Tx
}

func NewApplication() *Application {
	app := Application{
		ChainNotify: make(chan []Tx),
	}
	return &app
}

func (app *Application) ApplyEvents(eventsIn *events.EventList) (*events.EventList, error) {
	return modules.ApplyEventsSequentially(eventsIn, app.ApplyEvent)
}

func (app *Application) ApplyEvent(event *eventpb.Event) (*events.EventList, error) {
	switch e := event.Type.(type) {
	case *eventpb.Event_Init:
		// no actions on init
	case *eventpb.Event_Deliver:
		if err := app.ApplyBatch(e.Deliver.Batch); err != nil {
			return nil, fmt.Errorf("app batch delivery error: %w", err)
		}
	case *eventpb.Event_AppSnapshotRequest:
		data, err := app.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("app snapshot error: %w", err)
		}
		return (&events.EventList{}).PushBack(events.AppSnapshot(
			t.ModuleID(e.AppSnapshotRequest.Module),
			t.EpochNr(e.AppSnapshotRequest.Epoch),
			data,
		)), nil
	case *eventpb.Event_AppRestoreState:
		if err := app.RestoreState(e.AppRestoreState.Data); err != nil {
			return nil, fmt.Errorf("app restore state error: %w", err)
		}
	default:
		return nil, fmt.Errorf("unexpected type of App event: %T", event.Type)
	}

	return &events.EventList{}, nil
}

// ApplyBatch sends a batch consisting of data only to Eudico.
func (app *Application) ApplyBatch(in *requestpb.Batch) error {
	var out []Tx

	for _, req := range in.Requests {
		out = append(out, req.Req.Data)
	}

	app.ChainNotify <- out

	return nil
}

// Snapshot returns a binary representation of the application state.
// The returned value can be passed to RestoreState().
// At the time of writing this comment, the Mir library does not support state transfer
// and Snapshot is never actually called.
// We include its implementation for completeness.
func (app *Application) Snapshot() ([]byte, error) {
	return nil, nil
}

// RestoreState restores the application's state to the one represented by the passed argument.
// The argument is a binary representation of the application state returned from Snapshot().
// After the chat history is restored, RestoreState prints the whole chat history to stdout.
func (app *Application) RestoreState(snapshot []byte) error {
	return nil
}

// The ImplementsModule method only serves the purpose of indicating that this is a Module and must not be called.
func (app *Application) ImplementsModule() {}
