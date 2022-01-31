package tendermint

import (
	"crypto/sha256"
	"sync"

	"github.com/filecoin-project/go-state-types/abi"
)

// finalityWait is the number of epochs that we will wait
// before being able to re-propose a msg.
const (
	finalityWait = 100
)

func newMessagePool() *msgPool {
	return &msgPool{pool: make(map[[32]byte]abi.ChainEpoch)}
}

//TODO: messages should be removed from the pool after some time
type msgPool struct {
	lk   sync.RWMutex
	pool map[[32]byte]abi.ChainEpoch
}

func (p *msgPool) addMessage(tx []byte, epoch abi.ChainEpoch) {
	p.lk.Lock()
	defer p.lk.Unlock()

	id := sha256.Sum256(tx)
	p.pool[id] = epoch
}

func (p *msgPool) shouldSubmitMessage(tx []byte, currentEpoch abi.ChainEpoch) bool {
	p.lk.RLock()
	defer p.lk.RUnlock()

	id := sha256.Sum256(tx)
	proposedAt, proposed := p.pool[id]

	return !proposed || proposedAt+finalityWait < currentEpoch
}
