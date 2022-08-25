package main

import (
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

const UpsertSubnetQueryTemplate = "MATCH (n:SubnetNode) " +
	"WHERE n.Id=$id " +
	"SET n.SubnetCount=$subnetCount, n.Stake=$stake,n.Consensus=$consensus " +
	"WITH count(n) as nodesAltered " +
	"WHERE nodesAltered = 0 " +
	"CREATE (n:SubnetNode {Id:$id, SubnetCount:$subnetCount, Stake:$stake, Consensus:$consensus})"

const UpsertRelationshipQueryTemplate = "MATCH " +
	"(a:SubnetNode),(b:SubnetNode) " +
	"WHERE a.Id = $from AND b.Id = $to " +
	"CREATE (a)-[r:Parent]->(b)"

// Neo4jClient connects to the neo4j db instance
// TODO: use connection pool maybe?
type Neo4jClient struct {

	// internal fields
	driver neo4j.Driver
}

func NewNeo4jClient(uri, username, password string) (Neo4jClient, error) {
	driver, err := createDriver(uri, username, password)
	if err != nil {
		return Neo4jClient{}, err
	}
	return Neo4jClient{driver: driver}, nil
}

func (n *Neo4jClient) UpsertSubnet(subnets *[]SubnetNode) error {
	session := n.session()
	defer session.Close()

	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		for _, subnet := range *subnets {
			_, err := tx.Run(
				UpsertSubnetQueryTemplate,
				map[string]interface{}{
					"id":          subnet.SubnetID.String(),
					"subnetCount": subnet.SubnetCount,
					"stake":       subnet.Subnet.Stake.String(),
					"consensus":   subnet.Consensus,
				})
			// In face of driver native errors, make sure to return them directly.
			// Depending on the error, the driver may try to execute the function again.
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (n *Neo4jClient) SetParentRelationship(relationships *[]Relationship) error {
	session := n.session()
	defer session.Close()

	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		for _, relationship := range *relationships {
			_, err := tx.Run(
				UpsertRelationshipQueryTemplate,
				map[string]interface{}{
					"from": relationship.From.String(),
					"to":   relationship.To.String(),
				})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (n *Neo4jClient) session() neo4j.Session {
	return n.driver.NewSession(neo4j.SessionConfig{})
}

func (n *Neo4jClient) DeleteRelationship(aID string, bID string, relationship string) error {
	return nil
}

func createDriver(uri, username, password string) (neo4j.Driver, error) {
	return neo4j.NewDriver(uri, neo4j.BasicAuth(username, password, ""))
}

// call on application exit
func closeDriver(driver neo4j.Driver) error {
	return driver.Close()
}

func (n *Neo4jClient) Observe(metric string, value ...interface{}) {
	if metric == SubnetNodeUpdated {
		nodes := value[0].(*[]SubnetNode)
		log.Infow("received subnet updated", "nodes", nodes)
		if err := n.UpsertSubnet(nodes); err != nil {
			log.Errorw("cannot update nodes", "err", err, "nodes", nodes)
		}
	} else if metric == SubnetChildAdded {
		relationships := value[0].(*[]Relationship)
		log.Infow("received relationships added", "relationships", relationships)
		if err := n.SetParentRelationship(relationships); err != nil {
			log.Errorw("cannot update relationships", "err", err, "relationships", relationships)
		}
	}
	return
}
