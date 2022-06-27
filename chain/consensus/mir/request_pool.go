package mir

import (
	"sync"

	mirrequest "github.com/filecoin-project/mir/pkg/pb/requestpb"
)

// Request pool enforces the policy on what requests should be sent.
// The current policy is FIFO.
func newRequestPool() *requestPool {
	return &requestPool{
		cache:            make(map[string]string),
		processedClients: make(map[string]bool),
	}
}

type requestPool struct {
	lk sync.Mutex

	cache            map[string]string
	processedClients map[string]bool
}

// addRequest adds the request if it satisfies to the policy.
func (p *requestPool) addRequest(h string, r *mirrequest.Request) (exist bool) {
	p.lk.Lock()
	defer p.lk.Unlock()

	_, exist = p.processedClients[r.ClientId]
	if !exist {
		p.cache[h] = r.ClientId
		p.processedClients[r.ClientId] = true
	}
	return
}

// isTargetRequest returns whether the request with clientID and nonce should be sent.
func (p *requestPool) isTargetRequest(clientID string, nonce uint64) bool {
	p.lk.Lock()
	defer p.lk.Unlock()
	_, exist := p.processedClients[clientID]
	return !exist
}

// deleteRequest deletes the target request by the key h.
func (p *requestPool) deleteRequest(h string) (ok bool) {
	p.lk.Lock()
	defer p.lk.Unlock()

	clientID, ok := p.cache[h]
	if ok {
		delete(p.processedClients, clientID)
		delete(p.cache, h)
		return
	}
	return
}
