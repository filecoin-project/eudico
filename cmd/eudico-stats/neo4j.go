package main

import (
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

const NewCrossNetNodeQueryTemplate = "MATCH (n:CrossNetNode) " +
	"WHERE n.Id=$id " +
	"WITH count(n) as nodesAltered " +
	"WHERE nodesAltered = 0 " +
	"CREATE (n:CrossNetNode {Id:$id})"

const NewNodeSubnetQueryTemplate = "MATCH (n:SubnetNode) " +
	"WHERE n.Id=$id " +
	"WITH count(n) as nodesAltered " +
	"WHERE nodesAltered = 0 " +
	"CREATE (n:SubnetNode {Id:$id, SubnetCount:0, Stake:'N/A', Consensus:0})"

const UpsertSubnetQueryTemplate = "MATCH (n:SubnetNode) " +
	"WHERE n.Id=$id " +
	"SET n.SubnetCount=$subnetCount, n.Stake=$stake,n.Consensus=$consensus " +
	"WITH count(n) as nodesAltered " +
	"WHERE nodesAltered = 0 " +
	"CREATE (n:SubnetNode {Id:$id, SubnetCount:$subnetCount, Stake:$stake, Consensus:$consensus})"

const UpsertCrossNetTemplate = "MATCH (n:CrossNetNode) " +
	"WHERE n.Id=$id " +
	"SET n.TopDownNonce=$topDownNonce " +
	"WITH count(n) as nodesAltered " +
	"WHERE nodesAltered = 0 " +
	"CREATE (n:CrossNetNode {Id:$id, TopDownNonce:$topDownNonce})"

const ExistsSubnetNodeTemplate = "MATCH (a:SubnetNode{Id:$id}) WITH COUNT(a) > 0 AS nodeExists RETURN nodeExists"
const ExistsCrossNetNodeTemplate = "MATCH (a:CrossNetNode{Id:$id}) WITH COUNT(a) > 0 AS nodeExists RETURN nodeExists"

const UpsertRelationshipQueryTemplate = "MATCH (a:SubnetNode{Id:$from}) MATCH (b:SubnetNode{Id:$to}) MERGE (a)-[r:Parent]->(b)"
const UpsertTopDownRelationshipTemplate = 
	"MATCH (a:CrossNetNode{Id:$from}) " + 
	"MATCH (b:CrossNetNode{Id:$to}) " + 
	"MERGE (a)-[r:TopDown]->(b) " +
	"	ON CREATE SET r.Count = $count " +
	"	ON MATCH SET r.Count = $count"

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

// ========================= Crossnet Messages =======================
func (n *Neo4jClient) NewCrossNetNode(id address.SubnetID) error {
	session := n.session()
	defer session.Close()

	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		_, err := tx.Run(
			NewCrossNetNodeQueryTemplate,
			map[string]interface{}{
				"id": getSubnetIdString(id),
			})

		if err != nil {
			log.Errorw("cannot create cross net node", "err", err)
			return nil, err
		}
		return nil, nil
	})

	if err != nil {
		return err
	}

	return nil
}

// ======================= Subnet Info =====================
func (n *Neo4jClient) EnsureExists(id address.SubnetID, tx neo4j.Session, template string) error {
	for {
		result, err := tx.Run(
			template,
			map[string]interface{}{
				"id":          getSubnetIdString(id),
			},
		)

		if err != nil {
			log.Errorw("cannot ensure node exists", "error", err, "id", id)
			return err
		}

		for result.Next() {
			record := result.Record()
    		nodeExists, ok := record.Get("nodeExists")

			if ok && nodeExists == true {
				log.Infow("node exists", "id", id, "nodeExists", nodeExists, "ok", ok)
				return nil
			}
		}
		

		time.Sleep(50 * time.Millisecond)
	}
}

func (n *Neo4jClient) UpsertSubnet(subnets *[]SubnetNode) error {
	session := n.session()
	defer session.Close()

	for _, subnet := range *subnets {
		n.EnsureExists(subnet.SubnetID, session, ExistsSubnetNodeTemplate)
	}

	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		for _, subnet := range *subnets {
			_, err := tx.Run(
				UpsertSubnetQueryTemplate,
				map[string]interface{}{
					"id":          getSubnetIdString(subnet.SubnetID),
					"subnetCount": subnet.SubnetCount,
					"stake":       subnet.Stake,
					"consensus":   subnet.Consensus,
					"status":   subnet.Status,
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

func (n *Neo4jClient) NewSubnet(id address.SubnetID) error {
	session := n.session()
	defer session.Close()

	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		_, err := tx.Run(
			NewNodeSubnetQueryTemplate,
			map[string]interface{}{
				"id": getSubnetIdString(id),
			})
		// In face of driver native errors, make sure to return them directly.
		// Depending on the error, the driver may try to execute the function again.
		if err != nil {
			log.Errorw("cannot create subnet node", "err", err)
			return nil, err
		}

		_, err = tx.Run(
			NewCrossNetNodeQueryTemplate,
			map[string]interface{}{
				"id": getSubnetIdString(id),
			})
		// In face of driver native errors, make sure to return them directly.
		// Depending on the error, the driver may try to execute the function again.
		if err != nil {
			log.Errorw("cannot create crossnet node", "err", err)
			return nil, err
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

	for _, relationship := range *relationships {
		n.EnsureExists(relationship.From, session, ExistsSubnetNodeTemplate)
		n.EnsureExists(relationship.To, session, ExistsSubnetNodeTemplate)
	}
	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		for _, relationship := range *relationships {
			_, err := tx.Run(
				UpsertRelationshipQueryTemplate,
				map[string]interface{}{
					"from": getSubnetIdString(relationship.From),
					"to":   getSubnetIdString(relationship.To),
				})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	if err != nil {
		log.Errorw("cannot insert relationships to neo4j", "relationshiops", relationships)
		return err
	}

	return nil
}

func (n *Neo4jClient) SetTopDownRelationship(relationships *[]CrossNetRelationship) error {
	session := n.session()
	defer session.Close()

	for _, relationship := range *relationships {
		n.EnsureExists(relationship.From, session, ExistsCrossNetNodeTemplate)
		n.EnsureExists(relationship.To, session, ExistsCrossNetNodeTemplate)
	}

	_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		for _, relationship := range *relationships {
			_, err := tx.Run(
				UpsertTopDownRelationshipTemplate,
				map[string]interface{}{
					"from": getSubnetIdString(relationship.From),
					"to":   getSubnetIdString(relationship.To),
					"count":   relationship.Count,
				})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	if err != nil {
		log.Errorw("cannot insert topdown relationships to neo4j", "relationshiops", relationships)
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
	} else if metric == SubnetNodeAdded {
		id := value[0].(address.SubnetID)
		log.Infow("received node added", "id", id)
		if err := n.NewSubnet(id); err != nil {
			log.Errorw("add node failed", "err", err, "id", id)
		}
	} else if metric == SubnetChildAdded {
		relationships := value[0].(*[]Relationship)
		log.Infow("received relationships added", "relationships", relationships)
		if err := n.SetParentRelationship(relationships); err != nil {
			log.Errorw("cannot update relationships", "err", err, "relationships", relationships)
		}
	} else if metric == TopDownMsgUpdated {
		relationships := value[0].(*[]CrossNetRelationship)
		log.Infow("received top down relationships added", "relationships", relationships)
		if err := n.SetTopDownRelationship(relationships); err != nil {
			log.Errorw("cannot update relationships", "err", err, "relationships", relationships)
		}
	}
	return
}

func getSubnetIdString(id address.SubnetID) string {
	if id == address.RootSubnet {
		return "/root"
	}
	return id.String()
}
