package tendermint

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

type State struct {
	m              sync.Mutex
	filecoinBlocks map[[32]byte][]byte
	commitCount    int64
}

func NewState() *State {
	return &State{
		filecoinBlocks: make(map[[32]byte][]byte),
		commitCount:    0,
	}
}

func (s *State) GetBlock(id [32]byte) ([]byte, bool) {
	s.m.Lock()
	defer s.m.Unlock()

	b, ok := s.filecoinBlocks[id]

	return b, ok
}

func (s *State) AddBlock(block []byte) error {
	s.m.Lock()
	defer s.m.Unlock()

	id := sha256.Sum256(block)

	_, ok := s.filecoinBlocks[id]
	if ok {
		return fmt.Errorf("block with %s ID was already added", id)
	}

	s.filecoinBlocks[id] = block
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


func copyState(dst, src *State) {
	src.m.Lock()
	defer src.m.Unlock()

	dst.m.Lock()
	defer dst.m.Unlock()

	dst.filecoinBlocks = make(map[[32]byte][]byte, len(src.filecoinBlocks))
	for k, v := range src.filecoinBlocks {
		dst.filecoinBlocks[k] = v
	}
	dst.commitCount = src.commitCount
}