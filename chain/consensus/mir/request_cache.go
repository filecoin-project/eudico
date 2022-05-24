package mir

import (
	"sync"
)

// Request cache is the simplest temporal cache to store mapping between request hashes and original messages.
func newRequestCache() *requestCache {
	return &requestCache{cache: make(map[string]Request)}
}

type requestCache struct {
	lk    sync.Mutex
	cache map[string]Request
}

// addRequest adds the request.
func (c *requestCache) addRequest(hash string, r Request) {
	c.lk.Lock()
	defer c.lk.Unlock()

	c.cache[hash] = r
}

// getDelRequest gets the request by the key and deletes the key.
func (c *requestCache) getDelRequest(hash string) (Request, bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	r, ok := c.cache[hash]
	if ok {
		delete(c.cache, hash)
	}
	return r, ok
}

// getRequest gets the request by the key.
func (c *requestCache) getRequest(hash string) (Request, bool) {
	c.lk.Lock()
	defer c.lk.Unlock()

	r, ok := c.cache[hash]
	return r, ok
}
