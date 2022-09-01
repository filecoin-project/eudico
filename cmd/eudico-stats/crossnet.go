package main

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
)

const BottomUpType = "ButtomUp"
const CrossnetUpdated = "CrossnetUpdated"
const CrossnetChildAdded = "CrossnetChildAdded"
const CrossnetChildRemoved = "CrossnetChildRemoved"
const CrossnetNodeRemoved = "CrossnetNodeRemoved"
const CrossnetNodeUpdated = "CrossnetNodeUpdated"
const CrossnetNodeAdded = "CrossnetNodeAdded"

type CrossNetRelationship struct {
	From address.SubnetID
	To   address.SubnetID
	Type string
	Count int
}

type CrossnetRelationshipChange struct {
	Added   []CrossNetRelationship
	Removed []Relationship
}

func (s *CrossnetRelationshipChange) Remove(parent address.SubnetID, child address.SubnetID) {
	s.Removed = append(s.Removed, Relationship{From: parent, To: child})
}

func (s *CrossnetRelationshipChange) Add(parent address.SubnetID, child address.SubnetID, msgType string, count int) {
	r := CrossNetRelationship{
		From: parent,
		To: child,
		Type: msgType,
		Count: count,
	}
	s.Added = append(s.Added, r)
}

func (s *CrossnetRelationshipChange) IsUpdated() bool {
	return len(s.Removed) > 0 || len(s.Added) > 0
}

type CrossNetNodeChange struct {
	IsNodeUpdated bool
	Node          CrossnetNode
	Added         []CrossnetNode
	Removed       []address.SubnetID
}

func (s *CrossNetNodeChange) UpdateNode(node CrossnetNode) {
	s.IsNodeUpdated = true
	s.Node = node
}

func (s *CrossNetNodeChange) Remove(id address.SubnetID) {
	s.Removed = append(s.Removed, id)
}

func (s *CrossNetNodeChange) Add(node CrossnetNode) {
	s.Added = append(s.Added, node)
}

func (s *CrossNetNodeChange) IsUpdated() bool {
	return len(s.Removed) > 0 || len(s.Added) > 0 || s.IsNodeUpdated
}

type CrossnetChanges struct {
	NodeChanges         CrossNetNodeChange
	RelationshipChanges CrossnetRelationshipChange
}

func (s *CrossnetChanges) IsUpdated() bool {
	return s.NodeChanges.IsUpdated() || s.RelationshipChanges.IsUpdated()
}

func emptyCrossnetChanges(nodeId address.SubnetID) CrossnetChanges {
	return CrossnetChanges{
		NodeChanges: CrossNetNodeChange{
			IsNodeUpdated: false,
			Node:          idOnlyCrossnetNode(nodeId),
			Added:         make([]CrossnetNode, 0),
			Removed:       make([]address.SubnetID, 0),
		},
		RelationshipChanges: CrossnetRelationshipChange{
			Added:   make([]CrossNetRelationship, 0),
			Removed: make([]Relationship, 0),
		},
	}
}

type CrossnetNode struct {
	SubnetID address.SubnetID
	CrossnetCount int
	Consensus   hierarchical.ConsensusType
	Stake       string
	Status      sca.Status

	// for optimistic control
	Version uint64
}

func idOnlyCrossnetNode(nodeId address.SubnetID) CrossnetNode {
	return CrossnetNode{
		SubnetID:    nodeId,
		CrossnetCount: 0,
		Consensus:   0,
		Stake: "N/A",
		Version: 0,
	}
}

func rootCrossnetNode() CrossnetNode {
	return CrossnetNode{
		SubnetID: address.RootSubnet,
	}
}

func (s *CrossnetNode) Equals(o *CrossnetNode) bool {
	return s.SubnetID == o.SubnetID &&
		s.CrossnetCount == o.CrossnetCount &&
		s.Consensus == o.Consensus &&
		s.Stake == o.Stake &&
		s.Status == o.Status
}
