package mir

import (
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
)

type Tx []byte

type Application struct {
	reqStore    modules.RequestStore
	ChainNotify chan []Tx
}

func NewApplication(reqStore modules.RequestStore) *Application {
	app := Application{
		reqStore:    reqStore,
		ChainNotify: make(chan []Tx),
	}
	return &app
}

func (app *Application) Apply(batch *requestpb.Batch) error {
	var block []Tx

	for _, reqRef := range batch.Requests {
		msg, err := app.reqStore.GetRequest(reqRef)
		if err != nil {
			return err
		}
		block = append(block, msg)
	}

	app.ChainNotify <- block

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
