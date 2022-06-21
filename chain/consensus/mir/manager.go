package mir

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/mir"
	mirCrypto "github.com/filecoin-project/mir/pkg/crypto"
	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/grpctransport"
	"github.com/filecoin-project/mir/pkg/iss"
	mirLogging "github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
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
	Pool       *requestPool
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

	validatorsEnv := os.Getenv(ValidatorsEnv)
	if validatorsEnv != "" {
		validators, err = hierarchical.ValidatorsFromString(validatorsEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to get validators from string: %w", err)
		}
	} else {
		if subnetID == address.RootSubnet {
			return nil, fmt.Errorf("can't be run in rootnet without validators")
		}
		validators, err = api.SubnetStateGetValidators(ctx, subnetID)
		if err != nil {
			return nil, fmt.Errorf("failed to get validators from state")
		}
	}
	if len(validators) == 0 {
		return nil, fmt.Errorf("empty validator set")
	}

	mirID := newMirID(subnetID.String(), addr.String())

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

	// GrpcTransport is a rather dummy one and this call blocks until all connections are established,
	// which makes it, at this point, not fault-tolerant.
	net.Connect(ctx)

	// Instantiate the ISS protocol module with default configuration.
	issConfig := iss.DefaultConfig(nodeIds)
	issProtocol, err := iss.New(t.NodeID(mirID), issConfig, newMirLogger(managerLog))
	if err != nil {
		return nil, fmt.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	app := NewApplication()
	pool := newRequestPool()
	cryptoManager, err := NewCryptoManager(addr, api)
	if err != nil {
		return nil, fmt.Errorf("failed to create crypto manager: %w", err)
	}

	node, err := mir.NewNode(
		t.NodeID(mirID),
		&mir.NodeConfig{
			Logger: newMirLogger(managerLog),
		},
		map[t.ModuleID]modules.Module{
			"net":    net,
			"iss":    issProtocol,
			"app":    app,
			"crypto": mirCrypto.New(cryptoManager),
		},
		nil)
	if err != nil {
		return nil, err
	}

	m := Manager{
		Addr:       addr,
		SubnetID:   subnetID,
		NetName:    netName,
		Validators: validators,
		EudicoNode: api,
		Pool:       pool,
		MirID:      mirID,
		MirNode:    node,
		Wal:        wal,
		Net:        net,
		App:        app,
	}

	return &m, nil
}

// Start starts the manager.
func (m *Manager) Start(ctx context.Context) chan error {
	log.Infof("Mir manager %s starting", m.MirID)

	errChan := make(chan error, 1)

	go func() {

		// Run Mir node until it stops.
		if err := m.MirNode.Run(ctx); err != nil && !errors.Is(err, mir.ErrStopped) {
			log.Infof("Mir manager %s: Mir node stopped with error: %v", m.MirID, err)
			errChan <- err
		}

		// Perform cleanup of Node's modules.
		m.Stop()
	}()

	return errChan
}

// Stop stops the manager.
func (m *Manager) Stop() {
	log.Info("Mir manager shutting down")
	defer log.Info("Mir manager stopped")

	if err := m.Wal.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	m.Net.Stop()
}

func (m *Manager) SubmitRequests(ctx context.Context, reqRefs []*RequestRef) {
	if len(reqRefs) == 0 {
		return
	}
	var reqs []*requestpb.Request
	for _, ref := range reqRefs {
		reqs = append(reqs, events.ClientRequest(ref.ClientID, ref.ReqNo, ref.Hash))
	}
	e := events.NewClientRequests("iss", reqs)
	if err := m.MirNode.InjectEvents(ctx, (&events.EventList{}).PushBack(e)); err != nil {
		log.Errorf("failed to submit requests to Mir: %s", err)
	}
}

// GetMessagesByHashes gets requests from the cache and extracts Filecoin messages.
func (m *Manager) GetMessagesByHashes(blockRequestHashes []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	log.Infof("received a block with %d hashes", len(blockRequestHashes))
	for _, h := range blockRequestHashes {
		req, found := m.Pool.getDel(string(h))
		if !found {
			log.Errorf("unable to find a request with %v hash", h)
			continue
		}

		switch msg := req.(type) {
		case *types.SignedMessage:
			msgs = append(msgs, msg)
			log.Infof("got message (%s, %d) from cache", msg.Message.To, msg.Message.Nonce)
		case *types.UnverifiedCrossMsg:
			crossMsgs = append(crossMsgs, msg.Message)
			log.Infof("got cross-message (%s, %d) from cache", msg.Message.To, msg.Message.Nonce)
		default:
			log.Error("got unknown type request in a block")
		}
	}
	return
}

// AddSignedMessages adds signed messages into the request cache.
func (m *Manager) AddSignedMessages(dst []*RequestRef, msgs []*types.SignedMessage) ([]*RequestRef, error) {
	for _, msg := range msgs {
		hash := msg.Cid()
		clientID := newMirID(m.SubnetID.String(), msg.Message.From.String())
		nonce := msg.Message.Nonce
		r := RequestRef{
			ClientID: t.ClientID(clientID),
			ReqNo:    t.ReqNo(nonce),
			Type:     common.SignedMessageType,
			Hash:     hash.Bytes(),
		}
		alreadyExist := m.Pool.addIfNotExist(clientID, string(r.Hash), msg)
		if !alreadyExist {
			log.Infof("added message %s to cache", hash.Bytes())
			dst = append(dst, &r)
		}
	}

	return dst, nil
}

// AddCrossMessages adds cross messages into the request cache.
func (m *Manager) AddCrossMessages(dst []*RequestRef, msgs []*types.UnverifiedCrossMsg) ([]*RequestRef, error) {
	for _, msg := range msgs {
		hash := msg.Cid()

		msn, err := msg.Message.From.Subnet()
		if err != nil {
			log.Error("unable to get subnet from message:", err)
			continue
		}
		clientID := newMirID(msn.String(), msg.Message.From.String())
		nonce := msg.Message.Nonce
		r := RequestRef{
			ClientID: t.ClientID(clientID),
			ReqNo:    t.ReqNo(nonce),
			Type:     common.CrossMessageType,
			Hash:     hash.Bytes(),
		}
		alreadyExist := m.Pool.addIfNotExist(clientID, string(r.Hash), msg)
		if !alreadyExist {
			log.Infof("added cross-message %s to cache", hash.Bytes())
			dst = append(dst, &r)
		}
	}

	return dst, nil
}

// GetRequest gets the request from the cache by the key.
func (m *Manager) GetRequest(h string) (Request, bool) {
	return m.Pool.getRequest(h)
}
