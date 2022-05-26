package mir

import (
	"sync"
)

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

// getDel gets the target request by the key k and deletes the keys.
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
func (c *requestCache) getRequestItem(k string) (Request, bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	r, ok := c.cache[k]
	if ok {
		return r.Request, ok
	}
	return r, ok
}
