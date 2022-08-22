package main

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
)

type Relationship struct {
	From address.SubnetID
	To   address.SubnetID
}

type SubnetNode struct {
	SubnetID address.SubnetID

	SubnetCount int
	Consensus   hierarchical.ConsensusType
	Subnet      sca.Subnet

	// for optimistic control
	Version uint64
}

func (s *SubnetNode) Equals(o *SubnetNode) bool {
	return s.SubnetID == o.SubnetID &&
		s.SubnetCount == o.SubnetCount &&
		s.Consensus == o.Consensus &&
		s.Subnet.Stake.Equals(o.Subnet.Stake) &&
		s.Subnet.Status == o.Subnet.Status
}
