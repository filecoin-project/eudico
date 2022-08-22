package main

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/observer"
)

const SubnetUpdated = "SubnetUpdated"
const SubnetRelationShipUpdated = "SubnetRelationShipUpdated"

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

type SubnetNodeChange struct {
	Updated []address.SubnetID
	Removed []address.SubnetID
}

func (s *SubnetNodeChange) Remove(id address.SubnetID) {
	s.Removed = append(s.Removed, id)
}

func (s *SubnetNodeChange) Update(id address.SubnetID) {
	s.Updated = append(s.Updated, id)
}

type SubnetStats struct {
	// The nextEpoch to resync stats with the chain
	nextEpoch abi.ChainEpoch
	// The list of subnets currently managed by the current subnet
	subnets map[address.SubnetID]sca.SubnetOutput
}

func emptySubnetStats() SubnetStats {
	return SubnetStats{
		nextEpoch: 0,
		subnets:   make(map[address.SubnetID]sca.SubnetOutput),
	}
}

// ShouldReSync checks if it is needed to resync subnet stats with the current chain data
func (s *SubnetStats) ShouldReSync(curH abi.ChainEpoch) bool {
	return s.nextEpoch < curH
}

type SubnetDAGNode struct {
	ID       address.SubnetID
	Children map[address.SubnetID]*SubnetDAGNode
}

type EudicoStats struct {
	SubnetAPI v0api.FullNode

	// Private fields
	root         SubnetDAGNode
	subnetStats  map[address.SubnetID]SubnetNode
	toStopListen map[address.SubnetID]bool
	observer     observer.Observer
}

func NewEudicoStats(subnetAPI v0api.FullNode, ob observer.Observer) EudicoStats {
	return EudicoStats{
		SubnetAPI:    subnetAPI,
		subnetStats:  make(map[address.SubnetID]SubnetNode),
		toStopListen: make(map[address.SubnetID]bool),
		observer:     ob,
		root:         SubnetDAGNode{ID: address.RootSubnet, Children: make(map[address.SubnetID]*SubnetDAGNode)},
	}
}

func convertToMap(subnets []sca.SubnetOutput) map[address.SubnetID]sca.SubnetOutput {
	subnetMap := make(map[address.SubnetID]sca.SubnetOutput, 0)
	for _, subnet := range subnets {
		subnetMap[subnet.Subnet.ID] = subnet
	}
	return subnetMap
}

func defaultRootOutput() sca.SubnetOutput {
	return sca.SubnetOutput{
		Subnet: sca.Subnet{
			ID:     address.RootSubnet,
			Status: sca.Active,
			Stake:  abi.NewTokenAmount(0),
		},
		Consensus: 0,
	}
}

func (e *EudicoStats) print(node *SubnetDAGNode) {
	log.Infow("parent", "node", node.ID)

	for _, c := range node.Children {
		log.Infow("child", "node", c.ID)
		e.print(c)
	}
}

func (e *EudicoStats) TraverseSubnet(ctx context.Context) {
	relationShipChange := SubnetRelationshipChange{Added: make([]Relationship, 0), Removed: make([]Relationship, 0)}
	subnetNodeChange := SubnetNodeChange{Updated: make([]address.SubnetID, 0), Removed: make([]address.SubnetID, 0)}
	output := defaultRootOutput()

	_, root, _ := e.traverseSubnet(ctx, &e.root, &output, &relationShipChange, &subnetNodeChange)
	e.root = *root

	//for id, _ := range e.subnetStats {
	//	log.Infow("ids", "id", id)
	//}

	log.Infow("relationship updates", "updates", relationShipChange)
	log.Infow("node updates", "updates", subnetNodeChange)

	e.observe(subnetNodeChange, relationShipChange)
}

func (e *EudicoStats) observe(
	subnetChange SubnetNodeChange,
	relationshipChange SubnetRelationshipChange,
) {
	nodesUpdated := make([]SubnetNode, len(subnetChange.Updated))
	for _, id := range subnetChange.Updated {
		node, _ := e.subnetStats[id]
		nodesUpdated = append(nodesUpdated, node)
	}
	e.observer.Observe(SubnetUpdated, &nodesUpdated)

	e.observer.Observe(SubnetRelationShipUpdated, &relationshipChange.Added)
}

func (e *EudicoStats) traverseSubnet(
	ctx context.Context,
	node *SubnetDAGNode,
	subnetOutput *sca.SubnetOutput,
	relationShipChange *SubnetRelationshipChange,
	subnetNodeChange *SubnetNodeChange,
) (bool, *SubnetDAGNode, error) {
	log.Infow("processing subnet", "id", subnetOutput.Subnet.ID)

	isUpdated := false

	if node == nil {
		isUpdated = true
		node = &SubnetDAGNode{ID: subnetOutput.Subnet.ID, Children: make(map[address.SubnetID]*SubnetDAGNode)}
		log.Infow("is nil", "check", node.Children == nil, "id", node.ID)
	}

	subnets, err := e.SubnetAPI.ListSubnets(ctx, subnetOutput.Subnet.ID)
	if err != nil {
		log.Errorw("subnets cannot be listed at height", "subnet", subnetOutput.Subnet.ID, "err", err)
		//return false, node, nil
		subnets = make([]sca.SubnetOutput, 0)
	}

	log.Infow("listed subnets", "id", subnetOutput.Subnet.ID, "subnets", subnets, "nodeId", node.ID)

	newChildrenMap := convertToMap(subnets)

	// TODO: recheck node remove logic
	for child, _ := range node.Children {
		if _, ok := newChildrenMap[child]; !ok {
			relationShipChange.Remove(node.ID, child)
			delete(node.Children, child)
			subnetNodeChange.Remove(child)
		}
	}

	for child, output := range newChildrenMap {
		if _, ok := node.Children[child]; !ok {
			relationShipChange.Add(node.ID, child)
		}
		var childNode *SubnetDAGNode
		log.Infow("before is nil", "check", node.Children == nil, "id", node.ID, "child", child, "subnetOutput", subnetOutput.Subnet.ID)
		isUpdated, childNode, err = e.traverseSubnet(ctx, node.Children[child], &output, relationShipChange, subnetNodeChange)
		if err != nil {
			continue
		}
		log.Infow("is nil", "check", node.Children == nil, "id", node.ID, "child", child, "subnetOutput", subnetOutput.Subnet.ID)
		node.Children[child] = childNode
	}

	// first check if the node itself has changed
	e.updateSubnetStats(*subnetOutput, len(newChildrenMap), subnetNodeChange)

	return isUpdated, node, nil
}

func (e *EudicoStats) updateSubnetStats(subnetOutput sca.SubnetOutput, subnetsCount int, subnetNodeChange *SubnetNodeChange) {
	newNode := newSubnetNode(subnetOutput, subnetsCount)
	old, ok := e.subnetStats[subnetOutput.Subnet.ID]
	if !ok {
		log.Infow("new subnet created", "id", subnetOutput.Subnet.ID)
		e.subnetStats[subnetOutput.Subnet.ID] = newNode
		subnetNodeChange.Update(subnetOutput.Subnet.ID)
		return
	}

	if !old.Equals(&newNode) {
		log.Infow("new subnet updated", "id", subnetOutput.Subnet.ID)
		e.subnetStats[subnetOutput.Subnet.ID] = newNode
		subnetNodeChange.Update(subnetOutput.Subnet.ID)
		return
	}
}

func newSubnetNode(output sca.SubnetOutput, count int) SubnetNode {
	return SubnetNode{
		SubnetID:    output.Subnet.ID,
		SubnetCount: count,
		Consensus:   output.Consensus,
		Subnet:      output.Subnet,
		Version:     0,
	}
}
