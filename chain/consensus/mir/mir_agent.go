package mir

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
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

// mirLogger implements Mir's Log interface.
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
		m.logger.Errorw(text, "error", args)
	case mirLogging.LevelInfo:
		m.logger.Infow(text, "info", args)
	case mirLogging.LevelWarn:
		m.logger.Warnw(text, "warn", args)
	case mirLogging.LevelDebug:
		m.logger.Debugw(text, "debug", args)
	}
}

// MirAgent manages and provides direct access to a Mir node abstraction participating in consensus.
type MirAgent struct {
	OwnID string
	Node  *mir.Node
	Wal   *simplewal.WAL
	Net   *grpctransport.GrpcTransport
	App   *Application
}

func NewMirAgent(ctx context.Context, ownID string, clients []hierarchical.Validator) (*MirAgent, error) {
	if ownID == "" || clients == nil {
		return nil, xerrors.New("invalid node ID or clients")
	}
	log.Debugf("Mir agent %v is being created", ownID)
	defer log.Debugf("Mir agent %v has been created", ownID)

	nodeIds, nodeAddrs, err := hierarchical.ValidatorMembership(clients)
	if err != nil {
		return nil, err
	}
	log.Debugf("Mir node config:\n%v\n%v", nodeIds, nodeAddrs)

	walPath := path.Join("eudico-wal", fmt.Sprintf("%v", ownID))
	wal, err := simplewal.Open(walPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(walPath, 0700); err != nil {
		return nil, err
	}

	net := grpctransport.NewGrpcTransport(nodeAddrs, t.NodeID(ownID), newMirLogger(agentLog))
	if err := net.Start(); err != nil {
		return nil, err
	}
	net.Connect(ctx)
	log.Debug("Mir network transport connected")

	reqStore := reqstore.NewVolatileRequestStore()

	// Instantiate the ISS protocol module with default configuration.
	issConfig := iss.DefaultConfig(nodeIds)
	issProtocol, err := iss.New(t.NodeID(ownID), issConfig, newMirLogger(agentLog))
	if err != nil {
		return nil, xerrors.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	app := NewApplication(reqStore)

	node, err := mir.NewNode(
		t.NodeID(ownID),
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
		OwnID: ownID,
		Node:  node,
		Wal:   wal,
		Net:   net,
		App:   app,
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
		errChan <- m.Node.Run(ctx, time.NewTicker(50*time.Millisecond).C)
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
	close(m.App.ChainNotify)
}
