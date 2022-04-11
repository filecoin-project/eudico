package mirbft

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/hyperledger-labs/mirbft/pkg/modules"
	"github.com/hyperledger-labs/mirbft/pkg/pb/requestpb"
)

type Application struct {
	messages []string
	
	reqStore modules.RequestStore
}

func NewApplication(reqStore modules.RequestStore) *Application {
	app := Application{
		messages: make([]string, 0),
		reqStore: reqStore,
	}
	return &app
}

func (app *Application) Apply(batch *requestpb.Batch) error {

	// For each request in the batch
	for _, reqRef := range batch.Requests {

		// Extract request data from the request store and construct a printable chat message.
		reqData, err := app.reqStore.GetRequest(reqRef)
		if err != nil {
			return err
		}
		chatMessage := fmt.Sprintf("Client %d: %s", reqRef.ClientId, string(reqData))

		// Append the received chat message to the chat history.
		app.messages = append(app.messages, chatMessage)

		// Print received chat message.
		fmt.Println(chatMessage)
	}
	return nil
}

// Snapshot returns a binary representation of the application state.
// The returned value can be passed to RestoreState().
// At the time of writing this comment, the MirBFT library does not support state transfer
// and Snapshot is never actually called.
// We include its implementation for completeness.
func (app *Application) Snapshot() ([]byte, error) {

	// We use protocol buffers to serialize the application state.
	state := &AppState{
		Messages: app.messages,
	}
	return proto.Marshal(state)
}

// RestoreState restores the application's state to the one represented by the passed argument.
// The argument is a binary representation of the application state returned from Snapshot().
// After the chat history is restored, RestoreState prints the whole chat history to stdout.
func (chat *Application) RestoreState(snapshot []byte) error {
	panic("RestoreState not implemented")
}
