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
	mirRequest "github.com/filecoin-project/mir/pkg/pb/requestpb"
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
	log.With("miner", m.MirID).Infof("Mir manager shutting down")
	defer log.With("miner", m.MirID).Info("Mir manager stopped")

	if err := m.Wal.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	m.Net.Stop()
}

func (m *Manager) SubmitRequests(ctx context.Context, requests []*mirRequest.Request) {
	if len(requests) == 0 {
		return
	}
	e := events.NewClientRequests("iss", requests)
	if err := m.MirNode.InjectEvents(ctx, (&events.EventList{}).PushBack(e)); err != nil {
		log.Errorf("failed to submit requests to Mir: %s", err)
	}
}

func parseTx(tx []byte) (interface{}, error) {
	ln := len(tx)
	// This is very simple input validation to be protected against invalid messages.
	if ln <= 2 {
		return nil, fmt.Errorf("mir tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	lastByte := tx[ln-1]
	switch lastByte {
	case common.SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case common.CrossMessageType:
		msg, err = types.DecodeUnverifiedCrossMessage(tx[:ln-1])
	case common.ConfigMessageType:
		return nil, fmt.Errorf("config message is not supported")
	default:
		err = fmt.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// GetMessages extracts Filecoin messages from a Mir batch.
func (m *Manager) GetMessages(batch []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	log.Infof("received a block with %d messages", len(msgs))
	for _, tx := range batch {

		input, err := parseTx(tx)
		if err != nil {
			log.Error("unable to decode a message in Mir block:", err)
			continue
		}

		switch msg := input.(type) {
		case *types.SignedMessage:
			h := msg.Cid()
			found := m.Pool.getDel(h)
			if !found {
				log.Errorf("unable to find a request with %v hash", h)
				continue
			}
			msgs = append(msgs, msg)
			log.Infof("got message (%s, %d) from cache", msg.Message.To, msg.Message.Nonce)
		case *types.UnverifiedCrossMsg:
			h := msg.Cid()
			found := m.Pool.getDel(h)
			if !found {
				log.Errorf("unable to find a request with %v hash", h)
				continue
			}
			crossMsgs = append(crossMsgs, msg.Message)
			log.Infof("got cross-message (%s, %d) from cache", msg.Message.To, msg.Message.Nonce)
		default:
			log.Error("got unknown type request in a block")
		}
	}
	return
}

// BatchPushSignedMessages pushes signed messages into the request pool and sends them to Mir.
func (m *Manager) BatchPushSignedMessages(ctx context.Context, msgs []*types.SignedMessage) {
	var requests []*mirRequest.Request
	for _, msg := range msgs {
		msgBytes, err := msg.Serialize()
		if err != nil {
			log.Error("unable to serialize message:", err)
			continue
		}
		h := msg.Cid()
		clientID := newMirID(m.SubnetID.String(), msg.Message.From.String())
		nonce := msg.Message.Nonce
		data := common.NewSignedMessageBytes(msgBytes, nil)

		r := mirRequest.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Data:     data,
		}
		alreadyExist := m.Pool.addIfNotExist(clientID, h)
		if !alreadyExist {
			requests = append(requests, &r)
			log.Infof("added message %s to pool", h)
		}
	}
	m.SubmitRequests(ctx, requests)
}

// BatchPushCrossMessages pushes cross messages into the request pool and sends them to Mir.
func (m *Manager) BatchPushCrossMessages(ctx context.Context, msgs []*types.UnverifiedCrossMsg) {
	var requests []*mirRequest.Request
	for _, msg := range msgs {
		msgBytes, err := msg.Serialize()
		if err != nil {
			log.Error("unable to serialize cross-message:", err)
			continue
		}
		h := msg.Cid()

		msn, err := msg.Message.From.Subnet()
		if err != nil {
			log.Error("unable to get subnet from message:", err)
			continue
		}
		clientID := newMirID(msn.String(), msg.Message.From.String())
		nonce := msg.Message.Nonce
		data := common.NewCrossMessageBytes(msgBytes, nil)
		r := mirRequest.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Data:     data,
		}
		alreadyExist := m.Pool.addIfNotExist(clientID, h)
		if !alreadyExist {
			requests = append(requests, &r)
			log.Infof("added cross-message %s to pool", h)
		}
	}
	m.SubmitRequests(ctx, requests)
}

// ID prints Manager ID.
func (m *Manager) ID() string {
	addr := m.Addr.String()
	return fmt.Sprintf("%v:%v", m.SubnetID, addr[len(addr)-6:])
}
