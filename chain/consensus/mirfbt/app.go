package mirbft

import (
	"sync"

	"github.com/hyperledger-labs/mirbft/pkg/modules"
	"github.com/hyperledger-labs/mirbft/pkg/pb/requestpb"
)

type Tx []byte

type Application struct {
	mu sync.Mutex

	reqStore modules.RequestStore
	height   int64

	ChainNotify chan []Tx
}

func NewApplication(reqStore modules.RequestStore) *Application {
	app := Application{
		reqStore:    reqStore,
		height:      0,
		ChainNotify: make(chan []Tx),
	}
	return &app
}

/*
func (app *Application) Block() []Tx {
	app.mu.Lock()
	defer app.mu.Unlock()

	block := make([]Tx, len(app.cache))
	copy(block, app.cache)
	app.height++
	app.cache = nil

	return block
}

*/

func (app *Application) Apply(batch *requestpb.Batch) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	var block []Tx

	for _, reqRef := range batch.Requests {
		msg, err := app.reqStore.GetRequest(reqRef)
		if err != nil {
			return err
		}
		block = append(block, msg)
	}

	app.ChainNotify <- block
	app.height++

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
