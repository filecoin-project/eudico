package mir

import (
	"sync"

	mirrequest "github.com/filecoin-project/mir/pkg/pb/requestpb"
)

// fifoPool is a structure to implement the simplest pool that enforces FIFO policy on client messages.
// When a client sends a message we add clientID to orderingClients map and clientByCID.
// When we receive a message we find the clientID and remove it from orderingClients.
type fifoPool struct {
	lk sync.Mutex

	clientByCID     map[string]string // messageCID -> clientID
	orderingClients map[string]bool   // clientID -> bool
}

func newFIFOPool() *fifoPool {
	return &fifoPool{
		clientByCID:     make(map[string]string),
		orderingClients: make(map[string]bool),
	}
}

// addRequest adds the request if it satisfies to the FIFO policy.
func (p *fifoPool) addRequest(cid string, r *mirrequest.Request) (exist bool) {
	p.lk.Lock()
	defer p.lk.Unlock()

	_, exist = p.orderingClients[r.ClientId]
	if !exist {
		p.clientByCID[cid] = r.ClientId
		p.orderingClients[r.ClientId] = true
	}
	return
}

// isTargetRequest returns whether the request with clientID should be sent or there is a request from that client that
// is in progress of ordering.
func (p *fifoPool) isTargetRequest(clientID string) bool {
	p.lk.Lock()
	defer p.lk.Unlock()
	_, inProgress := p.orderingClients[clientID]
	return !inProgress
}

// deleteRequest deletes the target request by the key h.
func (p *fifoPool) deleteRequest(cid string) (ok bool) {
	p.lk.Lock()
	defer p.lk.Unlock()

	clientID, ok := p.clientByCID[cid]
	if ok {
		delete(p.orderingClients, clientID)
		delete(p.clientByCID, cid)
		return
	}
	return
}
