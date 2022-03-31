package tendermint

import (
	"encoding/binary"
	"sync"
)

type State struct {
	m sync.Mutex

	subnets map[string]int64
	height  int64
	hash    []byte
}

func NewState() *State {
	return &State{
		subnets: make(map[string]int64),
		height:  0,
	}
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

	h, ok := s.subnets[string(subnetName)]
	return h, ok
}

// IsSubnetSet returns true if a subnet has already been set.
func (s *State) IsSubnetSet() bool {
	s.m.Lock()
	defer s.m.Unlock()

	return len(s.subnets) == 1
}

// Commit commits the state.
func (s *State) Commit() error {
	s.m.Lock()
	defer s.m.Unlock()

	s.height++
	appHash := make([]byte, 8)
	binary.PutVarint(appHash, s.height)
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
