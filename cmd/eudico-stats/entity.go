package main

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
)

const SubnetUpdated = "SubnetUpdated"
const SubnetChildAdded = "SubnetChildAdded"
const SubnetChildRemoved = "SubnetChildRemoved"
const SubnetNodeRemoved = "SubnetNodeRemoved"
const SubnetNodeUpdated = "SubnetNodeUpdated"

type Relationship struct {
	From address.SubnetID
	To   address.SubnetID
}

type SubnetRelationshipChange struct {
	Added   []Relationship
	Removed []Relationship
}

func (s *SubnetRelationshipChange) Remove(parent address.SubnetID, child address.SubnetID) {
	s.Removed = append(s.Removed, Relationship{From: parent, To: child})
}

func (s *SubnetRelationshipChange) Add(parent address.SubnetID, child address.SubnetID) {
	s.Added = append(s.Added, Relationship{From: parent, To: child})
}

func (s *SubnetRelationshipChange) IsUpdated() bool {
	return len(s.Removed) > 0 || len(s.Added) > 0
}

type NodeChange struct {
	IsNodeUpdated bool
	Node          SubnetNode
	Added         []SubnetNode
	Removed       []address.SubnetID
}

func (s *NodeChange) UpdateNode(node SubnetNode) {
	s.IsNodeUpdated = true
	s.Node = node
}

func (s *NodeChange) Remove(id address.SubnetID) {
	s.Removed = append(s.Removed, id)
}

func (s *NodeChange) Add(node SubnetNode) {
	s.Added = append(s.Added, node)
}

func (s *NodeChange) IsUpdated() bool {
	return len(s.Removed) > 0 || len(s.Added) > 0 || s.IsNodeUpdated
}

type SubnetChanges struct {
	NodeChanges         NodeChange
	RelationshipChanges SubnetRelationshipChange
}

func (s *SubnetChanges) IsUpdated() bool {
	return s.NodeChanges.IsUpdated() || s.RelationshipChanges.IsUpdated()
}

func emptySubnetChanges() SubnetChanges {
	return SubnetChanges{
		NodeChanges: NodeChange{
			IsNodeUpdated: false,
			Node:          emptySubnetNode(),
			Added:         make([]SubnetNode, 0),
			Removed:       make([]address.SubnetID, 0),
		},
		RelationshipChanges: SubnetRelationshipChange{
			Added:   make([]Relationship, 0),
			Removed: make([]Relationship, 0),
		},
	}
}

type SubnetNode struct {
	SubnetID address.SubnetID

	SubnetCount int
	Consensus   hierarchical.ConsensusType
	Subnet      sca.Subnet

	// for optimistic control
	Version uint64
}

func emptySubnetNode() SubnetNode {
	return SubnetNode{}
}

//func FromSubnetState(state *subnet.SubnetState) SubnetNode {
//	return SubnetNode{
//		SubnetID:  state,
//		SubnetCount:,
//		Consensus: Subnet, :
//		Version: 0
//	}
//}

func fromSubnetOutput(output *sca.SubnetOutput, count int) SubnetNode {
	return SubnetNode{
		SubnetID:    output.Subnet.ID,
		SubnetCount: count,
		Consensus:   output.Consensus,
		Subnet:      output.Subnet,
		Version:     0,
	}
}

func (s *SubnetNode) Equals(o *SubnetNode) bool {
	return s.SubnetID == o.SubnetID &&
		s.SubnetCount == o.SubnetCount &&
		s.Consensus == o.Consensus &&
		s.Subnet.Stake.Equals(o.Subnet.Stake) &&
		s.Subnet.Status == o.Subnet.Status
}
