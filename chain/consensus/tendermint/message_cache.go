package tendermint

import (
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
)

// finalityWait is the number of epochs that we will be waiting before removing a message from the cache.
// To be able to resend messages we clear old messages that were sent finalityWait epochs ago.
const (
	cacheFinalityWait = 100
)

// Message cache is the simplest temporal cache to store messages that have been already sent to Tendermint.
// It is  a workaround for this bug: https://github.com/tendermint/tendermint/issues/7185.
func newMessageCache() *messageCache {
	return &messageCache{cache: make(map[string]abi.ChainEpoch)}
}

type messageCache struct {
	lk    sync.Mutex
	cache map[string]abi.ChainEpoch // An epoch when we submitted the message with ID last time.
}

// addSentMessage adds a message's ID that has been successfully sent (no error was triggered) to Tendermint.
func (c *messageCache) addSentMessage(id string, currentEpoch abi.ChainEpoch) {
	c.lk.Lock()
	defer c.lk.Unlock()

	c.cache[id] = currentEpoch
}

// shouldSubmitMessage returns true if we should send the input message to Tendermint.
func (c *messageCache) shouldSendMessage(id string) bool {
	c.lk.Lock()
	defer c.lk.Unlock()

	_, sent := c.cache[id]

	return !sent
}

func (c *messageCache) clearSentMessages(currentEpoch abi.ChainEpoch) {
	for k, sentAt := range c.cache {
		if sentAt+cacheFinalityWait < currentEpoch {
			delete(c.cache, k)
		}
	}
}

func (c *messageCache) deleteMessage(id string) {
	delete(c.cache, id)
}

func (c *messageCache) getInfo(id string) (sentAt abi.ChainEpoch, sent bool) {
	sentAt, sent = c.cache[id]
	return
}
