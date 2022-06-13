package mir

import (
	"sync"
)

// TODO:
// Case 1:
// Eudico's mempool and the request pool (that is actually a proxy to the mempool) are distributed.
// If Eudico daemon is restarted or crashes and miner was not, then the messages that Eudico has already sent
// are not in mempool anymore, but their hashes are still in the request cache.
// It may happen that Eudico receives a hash from the block and the corresponding message is in the cache,
// but not not in the mempool.

// Request cache is the simplest temporal cache to store mapping between request hashes and client requests.
func newRequestCache() *requestCache {
	return &requestCache{
		cache:          make(map[string]ClientRequest),
		handledClients: make(map[string]bool),
	}
}

type ClientRequest struct {
	Request  Request
	ClientID string
}

type requestCache struct {
	lk sync.Mutex

	cache          map[string]ClientRequest
	handledClients map[string]bool
}

// addIfNotExist adds the request if key h doesn't exist .
func (c *requestCache) addIfNotExist(clientID, h string, r Request) (exist bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	if exist = c.handledClients[clientID]; !exist {
		c.cache[h] = ClientRequest{r, clientID}
		c.handledClients[clientID] = true
	}
	return
}

// getDel gets the target request by the key h and deletes the keys.
func (c *requestCache) getDel(h string) (Request, bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	r, ok := c.cache[h]
	if ok {
		delete(c.handledClients, r.ClientID)
		delete(c.cache, h)
	}
	return r.Request, ok
}

// getRequest gets the request by the key.
func (c *requestCache) getRequest(h string) (Request, bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	r, ok := c.cache[h]
	if ok {
		return r.Request, ok
	}
	return r, ok
}
