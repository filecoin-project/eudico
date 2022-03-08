package tendermint

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/minio/blake2b-simd"
)

type State struct {
	m sync.Mutex

	filecoinBlocks map[[32]byte][]byte
	subnets        map[string]int64
	height         int64
	hash           []byte
}

func NewState() *State {
	return &State{
		filecoinBlocks: make(map[[32]byte][]byte),
		subnets:        make(map[string]int64),
		height:         0,
	}
}

// GetBlock returns the block by the given ID.
func (s *State) GetBlock(id [32]byte) ([]byte, bool) {
	s.m.Lock()
	defer s.m.Unlock()

	b, ok := s.filecoinBlocks[id]

	return b, ok
}

// SetOrGetSubnetOffset sets the offset value for the subnet if it hasn't beet already set.
// In the latter case, the function returns the set value.
func (s *State) SetOrGetSubnetOffset(subnetName []byte) int64 {
	s.m.Lock()
	defer s.m.Unlock()

	name := string(subnetName)

	h, ok := s.subnets[name]
	if !ok {
		s.subnets[name] = s.height
		return s.subnets[name]
	}
	return h
}

// GetSubnetOffset returns the offset of the given subnet.
func (s *State) GetSubnetOffset(subnetName []byte) (int64, bool) {
	s.m.Lock()
	defer s.m.Unlock()

	name := string(subnetName)
	h, ok := s.subnets[name]
	return h, ok
}

// AddBlock adds a block into the map, if the same block has not been already added.
func (s *State) AddBlock(block []byte) error {
	s.m.Lock()
	defer s.m.Unlock()

	id := blake2b.Sum256(block)

	_, ok := s.filecoinBlocks[id]
	if ok {
		return fmt.Errorf("block with %s ID was already added", id)
	}

	s.filecoinBlocks[id] = block
	return nil
}

// Commit commits the state.
func (s *State) Commit() error {
	s.m.Lock()
	defer s.m.Unlock()

	s.height++
	appHash := make([]byte, 8)
	binary.PutVarint(appHash, int64(len(s.filecoinBlocks)))
	s.hash = appHash
	return nil
}

// Height returns the current height.
func (s *State) Height() int64 {
	s.m.Lock()
	defer s.m.Unlock()

	return s.height
}

// Hash returns the hash of the state.
func (s *State) Hash() []byte {
	s.m.Lock()
	defer s.m.Unlock()

	return s.hash
}
