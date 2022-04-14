package mirbft

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/hyperledger-labs/mirbft"
	mirCrypto "github.com/hyperledger-labs/mirbft/pkg/crypto"
	"github.com/hyperledger-labs/mirbft/pkg/grpctransport"
	"github.com/hyperledger-labs/mirbft/pkg/iss"
	mirLogging "github.com/hyperledger-labs/mirbft/pkg/logging"
	"github.com/hyperledger-labs/mirbft/pkg/modules"
	"github.com/hyperledger-labs/mirbft/pkg/reqstore"
	"github.com/hyperledger-labs/mirbft/pkg/simplewal"
	t "github.com/hyperledger-labs/mirbft/pkg/types"
	"golang.org/x/xerrors"
)

const (
	nodeBasePort = 10000
)

type mir struct {
	Node  *mirbft.Node
	OwnID t.NodeID
	Wal   *simplewal.WAL
	Net   *grpctransport.GrpcTransport
	App   *Application
}

func newMir(id uint64) (*mir, error) {
	ownID := t.NodeID(id)
	nodeIds := []t.NodeID{ownID}

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
	issProtocol, err := iss.New(ownID, issConfig, mirLogging.NilLogger)
	if err != nil {
		return nil, xerrors.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	app := NewApplication(reqStore)

	node, err := mirbft.NewNode(ownID, &mirbft.NodeConfig{Logger: mirLogging.NilLogger}, &modules.Modules{
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

	n := mir{
		Node:  node,
		OwnID: ownID,
		Wal:   wal,
		Net:   net,
		App:   app,
	}

	return &n, nil
}

func (n *mir) serve(ctx context.Context) {
	log.Info("MirBFT node serving started")
	defer log.Info("MirBFT node serving stopped")

	stopC := make(chan struct{})

	nodeCtx, nodeCancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-ctx.Done():
			log.Info("MirBFT node: context closed, shutting down")
			n.stop()
			close(stopC)
		case <-nodeCtx.Done():
			n.stop()
		}

		log.Info("MirBFT node was stopped")
	}()

	go func() {
		// TODO: add a bug into the MirBFT repo: use context for signalling
		if err := n.Node.Run(stopC, time.NewTicker(100*time.Millisecond).C); err != nil {
			log.Errorf("MirBFT node error: %s", err)
			nodeCancel()
		}
	}()
}

func (n *mir) stop() {
	if err := n.Wal.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	n.Net.Stop()
}
