package mir

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
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

const (
	nodeBasePort = 10000
)

// agentLog is a logger accessed by Mir.
var agentLog = logging.Logger("mir-agent")

type mirLogger struct {
	logger *logging.ZapEventLogger
}

func newMirLogger(logger *logging.ZapEventLogger) *mirLogger {
	return &mirLogger{
		logger: logger,
	}
}

func (m *mirLogger) Log(level mirLogging.LogLevel, text string, args ...interface{}) {
	switch level {
	case mirLogging.LevelError:
		m.logger.Errorf(text, args)
	case mirLogging.LevelInfo:
		m.logger.Infof(text, args)
	case mirLogging.LevelWarn:
		m.logger.Warnf(text, args)
	case mirLogging.LevelDebug:
		m.logger.Debugf(text, args)
	}
}

// MirAgent manages and provides direct access to a Mir node abstraction participating in consensus.
type MirAgent struct {
	Node     *mirbft.Node
	Wal      *simplewal.WAL
	Net      *grpctransport.GrpcTransport
	App      *Application
	stopChan chan struct{}
}

func NewMirAgent(id uint64) (*MirAgent, error) {
	// TODO: Are client ID and node ID the same in this case?
	// TODO: Should mirbft use a different type for node ID?
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

	// TODO: Suggestion: Figure out how to make this a general interface where
	//  we can hook libp2p instead of making it grpc specific.
	net := grpctransport.NewGrpcTransport(nodeAddrs, ownID, nil)
	if err := net.Start(); err != nil {
		return nil, err
	}
	net.Connect()

	reqStore := reqstore.NewVolatileRequestStore()

	// Instantiate the ISS protocol module with default configuration.
	issConfig := iss.DefaultConfig(nodeIds)
	issProtocol, err := iss.New(ownID, issConfig, newMirLogger(agentLog))
	if err != nil {
		return nil, xerrors.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	app := NewApplication(reqStore)

	// TODO: write a wrapper on Eudico logger to use it in Mir.
	node, err := mirbft.NewNode(
		ownID,
		&mirbft.NodeConfig{
			Logger: newMirLogger(agentLog),
		},
		&modules.Modules{
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

	a := MirAgent{
		Node:     node,
		Wal:      wal,
		Net:      net,
		App:      app,
		stopChan: make(chan struct{}),
	}

	return &a, nil
}

func (m *MirAgent) Start(ctx context.Context) chan error {
	log.Info("Mir agent starting")

	errChan := make(chan error, 1)
	agentCtx, agentCancel := context.WithCancel(ctx)

	go func() {
		select {
		case <-ctx.Done():
			log.Debugf("Mir agent: context closed")
			m.Stop()
		case <-agentCtx.Done():
		}
	}()

	go func() {
		// TODO: add a bug into the Mir repo: use context for signalling instead of explicit channel
		errChan <- m.Node.Run(m.stopChan, time.NewTicker(100*time.Millisecond).C)
		agentCancel()
	}()

	return errChan
}

func (m *MirAgent) Stop() {
	log.Info("Mir agent shutting down")
	defer log.Info("Mir agent stopped")

	if err := m.Wal.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	m.Net.Stop()
	close(m.stopChan)
}
