package tendermint

import (
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
)

// finalityWait is the number of epochs that we will be waiting before being able to re-submit a message.
// To be able to submit messages we clear old messages that were sent finalityWait epochs ago.
const (
	finalityWait = 50
)

// Message pool is a simple temporal storage to store messages that have already been sent to Tendermint.
// It is  a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
func newMessagePool() *msgPool {
	return &msgPool{pool: make(map[string]abi.ChainEpoch)}
}

type msgPool struct {
	lk   sync.Mutex
	pool map[string]abi.ChainEpoch // An epoch when we submitted the message with ID last time.
}

// addSentMessage adds a message's ID that has been successfully sent (no error was triggered) to Tendermint.
func (p *msgPool) addSentMessage(id string, currentEpoch abi.ChainEpoch) {
	p.lk.Lock()
	defer p.lk.Unlock()

	p.pool[id] = currentEpoch
}

// shouldSubmitMessage returns true if we should send the input message in the specified epoch to Tendermint.
func (p *msgPool) shouldSendMessage(id string, currentEpoch abi.ChainEpoch) bool {
	p.lk.Lock()
	defer p.lk.Unlock()

	_, sent := p.pool[id]

	return !sent
}

func (p *msgPool) clearSentMessages(currentEpoch abi.ChainEpoch) {
	for k, sentAt := range p.pool {
		if sentAt+finalityWait > currentEpoch {
			delete(p.pool, k)
		}
	}
}

func (p *msgPool) deleteMessage(id string) {
	delete(p.pool, id)
}

func (p *msgPool) getInfo(id string) (sentAt abi.ChainEpoch, sent bool) {
	sentAt, sent = p.pool[id]
	return
}
