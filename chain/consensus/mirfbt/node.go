package mirbft

import (
	"context"
	"crypto"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/hyperledger-labs/mirbft"
	mirCrypto "github.com/hyperledger-labs/mirbft/pkg/crypto"
	"github.com/hyperledger-labs/mirbft/pkg/dummyclient"
	"github.com/hyperledger-labs/mirbft/pkg/grpctransport"
	"github.com/hyperledger-labs/mirbft/pkg/iss"
	mirLogging "github.com/hyperledger-labs/mirbft/pkg/logging"
	"github.com/hyperledger-labs/mirbft/pkg/modules"
	"github.com/hyperledger-labs/mirbft/pkg/reqstore"
	"github.com/hyperledger-labs/mirbft/pkg/requestreceiver"
	"github.com/hyperledger-labs/mirbft/pkg/simplewal"
	t "github.com/hyperledger-labs/mirbft/pkg/types"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

const (
	nodeBasePort        = 10000
	reqReceiverBasePort = 20000
)

type Node struct {
	Node             *mirbft.Node
	RequestReceiver  *requestreceiver.RequestReceiver
	Client           *dummyclient.DummyClient
	OwnID            t.NodeID
	ReqReceiverAddrs map[t.NodeID]string
	Wal              *simplewal.WAL
	Net              *grpctransport.GrpcTransport
	logger           *logging.ZapEventLogger
	App              *Application
}

func NewNode(id uint64) (*Node, error) {
	ownID := t.NodeID(id)
	nodeIds := []t.NodeID{ownID}
	logger := mirLogging.ConsoleDebugLogger

	nodeAddrs := make(map[t.NodeID]string)
	for _, i := range nodeIds {
		nodeAddrs[i] = fmt.Sprintf("127.0.0.1:%d", nodeBasePort+i)
	}

	walPath := path.Join("eudico-wal", fmt.Sprintf("%d", id))
	wal, err := simplewal.Open(walPath)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(walPath, 0700); err != nil {
		return nil, err
	}

	net := grpctransport.NewGrpcTransport(nodeAddrs, ownID, nil)
	if err := net.Start(); err != nil {
		return nil, err
	}
	net.Connect()

	reqStore := reqstore.NewVolatileRequestStore()

	// Instantiate the ISS protocol module with default configuration.
	issConfig := iss.DefaultConfig(nodeIds)
	issProtocol, err := iss.New(ownID, issConfig, logger)
	if err != nil {
		return nil, xerrors.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	app := NewApplication(reqStore)

	node, err := mirbft.NewNode(ownID, &mirbft.NodeConfig{Logger: logger}, &modules.Modules{
		Net:          net,
		WAL:          wal,
		RequestStore: reqStore,
		Protocol:     issProtocol,
		App:          app,
		Crypto:       &mirCrypto.DummyCrypto{DummySig: []byte{0}},
	})
	if err != nil {
		return nil, err
	}

	reqReceiver := requestreceiver.NewRequestReceiver(node, logger)

	client := dummyclient.NewDummyClient(
		t.ClientID(ownID),
		crypto.SHA256,
		&mirCrypto.DummyCrypto{DummySig: []byte{0}},
		logger,
	)

	n := Node{
		Node:            node,
		RequestReceiver: reqReceiver,
		Client:          client,
		OwnID:           ownID,
		Wal:             wal,
		Net:             net,
		App:             app,
	}

	return &n, nil
}

func (n *Node) Serve(ctx context.Context) error {
	log.Info("MirBFT node serving started")
	defer log.Info("MirBFT node serving stopped")

	stopC := make(chan struct{})

	go func() {
		<-ctx.Done()

		log.Info("MirBFT node serve: context closed, shutting down")

		if err := n.Wal.Close(); err != nil {
			log.Errorf("Could not close write-ahead log: %s", err)
		}
		n.Client.Disconnect()
		n.RequestReceiver.Stop()
		n.Net.Stop()

		close(stopC)

		log.Info("MirBFT node was stopped")
	}()

	// Start the node in a separate goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := n.Node.Run(stopC, time.NewTicker(100*time.Millisecond).C); err != nil {
			log.Infof("Node error: %s", err)
		}
		wg.Done()
	}()

	if err := n.RequestReceiver.Start(reqReceiverBasePort + int(n.OwnID)); err != nil {
		return err
	}

	n.Client.Connect(ctx, n.ReqReceiverAddrs)

	wg.Wait()
	return nil
}
