package mir

import (
	"sync"
)

// Request pool is the simplest temporal pool to store mapping between request hashes and client requests.
func newRequestPool() *requestPool {
	return &requestPool{
		cache:          make(map[string]ClientRequest),
		handledClients: make(map[string]bool),
	}
}

type ClientRequest struct {
	ClientID string
}

type requestPool struct {
	lk sync.Mutex

	cache          map[string]ClientRequest
	handledClients map[string]bool
}

// addIfNotExist adds the request if key h doesn't exist .
func (c *requestPool) addIfNotExist(clientID, h string) (exist bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	if exist = c.handledClients[clientID]; !exist {
		c.cache[h] = ClientRequest{clientID}
		c.handledClients[clientID] = true
	}
	return
}

// getDel gets the target request by the key h and deletes the keys.
func (c *requestPool) getDel(h string) bool {
	c.lk.Lock()
	defer c.lk.Unlock()

	r, ok := c.cache[h]
	if ok {
		delete(c.handledClients, r.ClientID)
		delete(c.cache, h)
	}
	return ok
}
