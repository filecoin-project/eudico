package tendermint

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
)

type State struct {
	m              sync.Mutex
	filecoinBlocks map[string][]byte
	tx map[[32]byte]bool
	commitCount    int64
}

func NewState() *State {
	return &State{
		filecoinBlocks: make(map[string][]byte),
		tx: make(map[[32]byte]bool),
		commitCount:    0,
	}
}

func (s *State) ExistTx(id [32]byte) bool {
	s.m.Lock()
	defer s.m.Unlock()

	_, ok := s.tx[id]
	return ok
}

func (s *State) AddTx(id [32]byte) {
	s.m.Lock()
	defer s.m.Unlock()
	s.tx[id] = true
}

func (s *State) GetBlock(id string) ([]byte, bool) {
	s.m.Lock()
	defer s.m.Unlock()

	b, ok := s.filecoinBlocks[id]
	if !ok {
		return nil, false
	}
	return b, true
}

func (s *State) AddBlock(id string, block []byte) error {
	s.m.Lock()
	defer s.m.Unlock()

	_, ok := s.filecoinBlocks[id]
	if ok {
		return fmt.Errorf("block with %s ID was already added", id)
	}

	h := sha256.Sum256(block)
	s.filecoinBlocks[id] = h[:]
	return nil
}

func (s *State) Commit() error {
	s.m.Lock()
	defer s.m.Unlock()

	s.commitCount++
	return nil
}

func (s *State) Commits() int64 {
	s.m.Lock()
	defer s.m.Unlock()

	return s.commitCount
}


func (s *State) GetLastBlockHash() []byte {
	s.m.Lock()
	defer s.m.Unlock()

	id := strconv.FormatInt(s.commitCount, 10)
	return s.filecoinBlocks[id]
}

func copyState(dst, src *State) {
	src.m.Lock()
	defer src.m.Unlock()

	dst.m.Lock()
	defer dst.m.Unlock()

	dst.filecoinBlocks = make(map[string][]byte, len(src.filecoinBlocks))
	for k, v := range src.filecoinBlocks {
		dst.filecoinBlocks[k] = v
	}
	dst.commitCount = src.commitCount
}