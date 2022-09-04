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
const SubnetNodeAdded = "SubnetNodeAdded"

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
	CrossNetChanges CrossnetRelationshipChange
}

func (s *SubnetChanges) IsUpdated() bool {
	return s.NodeChanges.IsUpdated() || s.RelationshipChanges.IsUpdated() || s.CrossNetChanges.IsUpdated()
}

func emptySubnetChanges(nodeId address.SubnetID) SubnetChanges {
	return SubnetChanges{
		NodeChanges: NodeChange{
			IsNodeUpdated: false,
			Node:          idOnlySubnetNode(nodeId),
			Added:         make([]SubnetNode, 0),
			Removed:       make([]address.SubnetID, 0),
		},
		RelationshipChanges: SubnetRelationshipChange{
			Added:   make([]Relationship, 0),
			Removed: make([]Relationship, 0),
		},
		CrossNetChanges: CrossnetRelationshipChange{
			TopDownAdded:   make([]CrossNetRelationship, 0),
			Removed: make([]Relationship, 0),
		},
	}
}

type SubnetNode struct {
	SubnetID address.SubnetID
	SubnetCount int
	Consensus   hierarchical.ConsensusType
	Stake       string
	Status      sca.Status

	// for optimistic control
	Version uint64
}

func idOnlySubnetNode(nodeId address.SubnetID) SubnetNode {
	return SubnetNode{
		SubnetID:    nodeId,
		SubnetCount: 0,
		Consensus:   0,
		Stake: "N/A",
		Version: 0,
	}
}

func rootSubnetNode() SubnetNode {
	return SubnetNode{
		SubnetID: address.RootSubnet,
	}
}

//func FromSubnetState(state *subnet.SubnetState) SubnetNode {
//	return SubnetNode{
//		SubnetID:  state,
//		SubnetCount:,
//		Consensus: Subnet, :
//		Version: 0
//	}
//}

func fromSubnetOutput(id address.SubnetID, output *sca.SubnetOutput, count int) SubnetNode {
	return SubnetNode{
		SubnetID:    id,
		SubnetCount: count,
		Consensus:   output.Consensus,
		Stake:       output.Subnet.Stake.String(),
		Status:      output.Subnet.Status,
		Version:     0,
	}
}

func (s *SubnetNode) Equals(o *SubnetNode) bool {
	return s.SubnetID == o.SubnetID &&
		s.SubnetCount == o.SubnetCount &&
		s.Consensus == o.Consensus &&
		s.Stake == o.Stake &&
		s.Status == o.Status
}
