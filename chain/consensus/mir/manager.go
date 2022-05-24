package mir

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
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

// managerLog is a Eudico logger used by a Mir node.
var managerLog = logging.Logger("mir-manager")

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

func NewWAL(ownID, walDir string) (*simplewal.WAL, error) {
	walPath := path.Join(walDir, fmt.Sprintf("%v", ownID))
	wal, err := simplewal.Open(walPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(walPath, 0700); err != nil {
		return nil, err
	}
	return wal, nil
}

// Manager manages Mir and Eudico nodes participating in consensus.
type Manager struct {
	NetName    dtypes.NetworkName
	SubnetID   address.SubnetID
	Addr       address.Address
	MirID      string
	Validators []hierarchical.Validator
	EudicoNode v1api.FullNode
	Cache      *requestCache
	MirNode    *mir.Node
	Wal        *simplewal.WAL
	Net        *grpctransport.GrpcTransport
	App        *Application
}

func NewManager(ctx context.Context, addr address.Address, api v1api.FullNode) (*Manager, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}
	subnetID := address.SubnetID(netName)

	var validators []hierarchical.Validator

	minersEnv := os.Getenv(ValidatorsEnv)
	if minersEnv != "" {
		validators, err = hierarchical.ValidatorsFromString(minersEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to get validators from string: %s", err)
		}
	} else {
		if subnetID == address.RootSubnet {
			return nil, xerrors.New("can't be run in rootnet without validators")
		}
		validators, err = api.SubnetStateGetValidators(ctx, subnetID)
		if err != nil {
			return nil, xerrors.New("failed to get validators from state")
		}
	}
	if len(validators) == 0 {
		return nil, xerrors.New("empty validator set")
	}

	mirID := fmt.Sprintf("%s:%s", subnetID, addr)

	log.Debugf("Mir manager %v is being created", mirID)
	defer log.Debugf("Mir manager %v has been created", mirID)

	nodeIds, nodeAddrs, err := hierarchical.ValidatorMembership(validators)
	if err != nil {
		return nil, err
	}
	log.Debugf("Mir node config:\n%v\n%v", nodeIds, nodeAddrs)

	wal, err := NewWAL(mirID, "eudico-wal")
	if err != nil {
		return nil, err
	}

	net := grpctransport.NewGrpcTransport(nodeAddrs, t.NodeID(mirID), newMirLogger(managerLog))
	if err := net.Start(); err != nil {
		return nil, err
	}
	net.Connect(ctx)
	log.Debug("Mir network transport connected")

	reqStore := reqstore.NewVolatileRequestStore()

	// Instantiate the ISS protocol module with default configuration.
	issConfig := iss.DefaultConfig(nodeIds)
	issProtocol, err := iss.New(t.NodeID(mirID), issConfig, newMirLogger(managerLog))
	if err != nil {
		return nil, fmt.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	app := NewApplication(reqStore)

	node, err := mir.NewNode(
		t.NodeID(mirID),
		&mir.NodeConfig{
			Logger: newMirLogger(managerLog),
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

	a := Manager{
		Addr:       addr,
		SubnetID:   subnetID,
		NetName:    netName,
		Validators: validators,
		EudicoNode: api,
		Cache:      newRequestCache(),
		MirID:      mirID,
		MirNode:    node,
		Wal:        wal,
		Net:        net,
		App:        app,
	}

	return &a, nil
}

// Start starts an agent.
func (m *Manager) Start(ctx context.Context) chan error {
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
		errChan <- m.MirNode.Run(ctx, time.NewTicker(time.Duration(build.MirTimer)*time.Millisecond).C)
		agentCancel()
	}()

	return errChan
}

// Stop stops an agent.
func (m *Manager) Stop() {
	log.Info("Mir agent shutting down")
	defer log.Info("Mir agent stopped")

	if err := m.Wal.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	m.Net.Stop()
	close(m.App.ChainNotify)
}

func (m *Manager) SubmitRequests(ctx context.Context, refs []*RequestRef) {
	for _, r := range refs {
		err := m.MirNode.SubmitRequest(ctx, r.ClientID, r.ReqNo, r.Hash, []byte{})
		if err != nil {
			log.Errorf("unable to submit a request from %s to Mir: %v", r.ClientID, err)
			continue
		}
		log.Debugf("successfully sent message %d from %s to Mir", r.Type, r.ClientID)
	}
}

// GetRequest gets the request from the cache.
func (m *Manager) GetRequest(ref *RequestRef) (Request, bool) {
	return m.Cache.getRequest(string(ref.Hash))
}

// GetMessagesByHashes gets requests from the cache and extracts Filecoin messages.
func (m *Manager) GetMessagesByHashes(hashes []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	for _, h := range hashes {
		req, found := m.Cache.getDelRequest(string(h))
		if !found {
			log.Errorf("unable to find a request with %v hash", h)
			continue
		}
		switch m := req.(type) {
		case *types.SignedMessage:
			msgs = append(msgs, m)
		case *types.UnverifiedCrossMsg:
			crossMsgs = append(crossMsgs, m.Message)
		default:
			log.Error("got a request of unknown type")
		}
	}
	return
}

// PushSignedMessages pushes signed messages to the request cache.
func (m *Manager) AddSignedMessages(dst []*RequestRef, msgs []*types.SignedMessage) ([]*RequestRef, error) {
	for _, msg := range msgs {
		hash := msg.Cid()
		clientID := fmt.Sprintf("%s:%s", m.SubnetID, msg.Message.From.String())
		r := RequestRef{
			ClientID: t.ClientID(clientID),
			ReqNo:    t.ReqNo(msg.Message.Nonce),
			Type:     common.SignedMessageType,
			Hash:     hash.Bytes(),
		}
		m.Cache.addRequest(string(hash.Bytes()), msg)
		dst = append(dst, &r)
	}

	return dst, nil
}

// PushCrossMessages pushes cross messages to the request cache.
func (m *Manager) AddCrossMessages(dst []*RequestRef, crossMsgs []*types.UnverifiedCrossMsg) ([]*RequestRef, error) {
	for _, msg := range crossMsgs {
		hash := msg.Cid()

		msn, err := msg.Message.From.Subnet()
		if err != nil {
			log.Error("unable to get subnet from message:", err)
			continue
		}
		// client ID for this message MUST BE the same on all Eudico nodes.
		clientID := fmt.Sprintf("%s:%s", msn, msg.Message.From.String())

		r := RequestRef{
			ClientID: t.ClientID(clientID),
			ReqNo:    t.ReqNo(msg.Message.Nonce),
			Type:     common.CrossMessageType,
			Hash:     hash.Bytes(),
		}
		dst = append(dst, &r)
		m.Cache.addRequest(string(hash.Bytes()), msg)
	}

	return dst, nil
}
