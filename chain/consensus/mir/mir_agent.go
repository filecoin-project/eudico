package mir

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/mir"
	mirCrypto "github.com/filecoin-project/mir/pkg/crypto"
	"github.com/filecoin-project/mir/pkg/grpctransport"
	"github.com/filecoin-project/mir/pkg/iss"
	mirLogging "github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/reqstore"
	"github.com/filecoin-project/mir/pkg/simplewal"
	t "github.com/filecoin-project/mir/pkg/types"
)

// agentLog is a logger accessed by Mir.
var agentLog = logging.Logger("mir-agent")

// mirLogger implement Mir's Log interface.
type mirLogger struct {
	logger *logging.ZapEventLogger
}

func newMirLogger(logger *logging.ZapEventLogger) *mirLogger {
	return &mirLogger{
		logger: logger,
	}
}

// Log logs a message with additional context.
func (m *mirLogger) Log(level mirLogging.LogLevel, text string, args ...interface{}) {
	switch level {
	case mirLogging.LevelError:
		m.logger.Errorw(text, args)
	case mirLogging.LevelInfo:
		m.logger.Infow(text, args)
	case mirLogging.LevelWarn:
		m.logger.Warnw(text, args)
	case mirLogging.LevelDebug:
		m.logger.Debugw(text, args)
	}
}

// MirAgent manages and provides direct access to a Mir node abstraction participating in consensus.
type MirAgent struct {
	Node     *mir.Node
	Wal      *simplewal.WAL
	Net      *grpctransport.GrpcTransport
	App      *Application
	stopChan chan struct{}
}

func NewMirAgent(id string, n int) (*MirAgent, error) {
	// TODO: Are client ID and node ID the same in this case?
	// TODO: Should mirbft use a different type for node ID?
	ownID := t.NodeID(id)
	log.Debugf("Mir agent %v is being created", ownID)
	defer log.Debugf("Mir agent %v has been created", ownID)

	nodeIds, nodeAddrs := getConfig(n)
	log.Debug("Mir node config:", nodeIds, nodeAddrs)

	walPath := path.Join("eudico-wal", fmt.Sprintf("%v", id))
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

	node, err := mir.NewNode(
		ownID,
		&mir.NodeConfig{
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

// Start starts an agent.
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
		errChan <- m.Node.Run(ctx, time.NewTicker(100*time.Millisecond).C)
		agentCancel()
	}()

	return errChan
}

// Stop stops an agent.
func (m *MirAgent) Stop() {
	log.Info("Mir agent shutting down")
	defer log.Info("Mir agent stopped")

	if err := m.Wal.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	m.Net.Stop()
	close(m.stopChan)
	close(m.App.ChainNotify)
}
