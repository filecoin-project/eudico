package mir

import (
	"sync"

	"github.com/ipfs/go-cid"
)

// Request pool is the simplest temporal pool to store mapping between request hashes and client requests.
func newRequestPool() *requestPool {
	return &requestPool{
		pool:           make(map[cid.Cid]ClientRequest),
		handledClients: make(map[string]bool),
	}
}

type ClientRequest struct {
	ClientID string
}

type requestPool struct {
	lk sync.Mutex

	pool           map[cid.Cid]ClientRequest
	handledClients map[string]bool
}

// addIfNotExist adds the request if key h doesn't exist .
func (p *requestPool) addIfNotExist(clientID string, h cid.Cid) (exist bool) {
	p.lk.Lock()
	defer p.lk.Unlock()

	if exist = p.handledClients[clientID]; !exist {
		p.pool[h] = ClientRequest{clientID}
		p.handledClients[clientID] = true
	}
	return
}

// getDel gets the target request by the key h and deletes the keys.
func (p *requestPool) getDel(h cid.Cid) bool {
	p.lk.Lock()
	defer p.lk.Unlock()

	r, ok := p.pool[h]
	if ok {
		delete(p.handledClients, r.ClientID)
		delete(p.pool, h)
	}
	return ok
}
