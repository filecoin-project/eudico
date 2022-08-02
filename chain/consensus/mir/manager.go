// Package mir implements ISS consensus protocol using the Mir protocol framework.
package mir

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/mir"
	mircrypto "github.com/filecoin-project/mir/pkg/crypto"
	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/iss"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/net"
	mirlibp2p "github.com/filecoin-project/mir/pkg/net/libp2p"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/simplewal"
	t "github.com/filecoin-project/mir/pkg/types"
)

// Manager manages the Eudico and Mir nodes participating in ISS consensus protocol.
type Manager struct {
	// Eudico types
	EudicoNode v1api.FullNode
	NetName    dtypes.NetworkName
	SubnetID   address.SubnetID
	Addr       address.Address
	Pool       *requestPool
	Validators []hierarchical.Validator

	// Mir types
	MirNode *mir.Node
	MirID   string
	WAL     *simplewal.WAL
	Net     net.Transport
	ISS     *iss.ISS
	Crypto  *CryptoManager
	App     *StateManager

	// Reconfiguration
	ReconfigurationVotes    map[string]uint64
	LastReconfigurationHash []byte
	ValidatorsNumber        int
	FaultyNumber            int
}

func NewManager(ctx context.Context, addr address.Address, api v1api.FullNode) (*Manager, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}

	subnetID, err := address.SubnetIDFromString(string(netName))
	if err != nil {
		return nil, err
	}

	libp2pKeyBytes, err := api.PrivKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get private key: %w", err)
	}

	libp2pKey, err := crypto.UnmarshalPrivateKey(libp2pKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal private key: %w", err)
	}

	validatorSet, err := getSubnetValidators(ctx, subnetID, api)
	if err != nil {
		return nil, fmt.Errorf("failed to get validators: %w", err)
	}
	validators := validatorSet.GetValidators()
	if len(validators) == 0 {
		return nil, fmt.Errorf("empty validator set")
	}

	nodeIDs, nodes, err := ValidatorsMembership(validators)
	if err != nil {
		return nil, fmt.Errorf("failed to build node membership: %w", err)
	}

	mirID := newMirID(subnetID.String(), addr.String())
	mirAddr := nodes[t.NodeID(mirID)]

	log.Info("Eudico node's Mir ID: ", mirID)
	log.Info("Eudico node's address in Mir: ", mirAddr)
	log.Info("Mir nodes IDs: ", nodeIDs)
	log.Info("Mir nodes addresses: ", nodes)

	peerID, err := peer.AddrInfoFromP2pAddr(mirAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get addr info: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(libp2pKey),
		libp2p.DefaultTransports,
		libp2p.ListenAddrs(peerID.Addrs[0]),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create Mir modules.

	wal, err := NewWAL(mirID, fmt.Sprintf("eudico-wal-%s", addr))
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL: %w", err)
	}

	netTransport, err := mirlibp2p.NewTransport(h, t.NodeID(mirID), newMirLogger(managerLog))
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}
	if err := netTransport.Start(); err != nil {
		return nil, fmt.Errorf("failed to start transport: %w", err)
	}
	netTransport.Connect(ctx, nodes)

	cryptoManager, err := NewCryptoManager(addr, api)
	if err != nil {
		return nil, fmt.Errorf("failed to create crypto manager: %w", err)
	}

	// Instantiate the ISS protocol module with default configuration.

	issConfig := iss.DefaultConfig(nodeIDs)
	issProtocol, err := iss.New(t.NodeID(mirID), issConfig, newMirLogger(managerLog))
	if err != nil {
		return nil, fmt.Errorf("could not instantiate ISS protocol module: %w", err)
	}

	pool := newRequestPool()

	m := Manager{
		Addr:                 addr,
		SubnetID:             subnetID,
		NetName:              netName,
		Validators:           validators,
		EudicoNode:           api,
		Pool:                 pool,
		MirID:                mirID,
		WAL:                  wal,
		Crypto:               cryptoManager,
		Net:                  netTransport,
		ISS:                  issProtocol,
		ReconfigurationVotes: make(map[string]uint64),
		ValidatorsNumber:     len(validators),
		FaultyNumber:         (len(validators) - 1) / 3,
	}

	sm := NewStateManager(&m)

	// Create a Mir node, using the default configuration and passing the modules initialized just above.
	mirModules, err := iss.DefaultModules(map[t.ModuleID]modules.Module{
		"net":    netTransport,
		"iss":    issProtocol,
		"app":    sm,
		"crypto": mircrypto.New(m.Crypto),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Mir modules: %w", err)
	}

	cfg := mir.NodeConfig{Logger: newMirLogger(managerLog)}
	newMirNode, err := mir.NewNode(t.NodeID(mirID), &cfg, mirModules, wal, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Mir node: %w", err)
	}

	m.MirNode = newMirNode
	m.App = sm

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

	if err := m.WAL.Close(); err != nil {
		log.Errorf("Could not close write-ahead log: %s", err)
	}
	log.Info("WAL closed")

	m.Net.Stop()
}

// ReconfigureMirNode reconfigures the Mir node.
func (m *Manager) ReconfigureMirNode(ctx context.Context) error {
	log.With("miner", m.MirID).Infof("Creating a Mir node started")
	defer log.With("miner", m.MirID).Info("Creating a Mir node finished")

	if len(m.Validators) == 0 {
		return fmt.Errorf("empty validator set")
	}
	nodeIDs, nodes, err := ValidatorsMembership(m.Validators)
	if err != nil {
		return fmt.Errorf("failed to build node membership: %w", err)
	}
	m.Net.Connect(ctx, nodes)
	err = m.MirNode.InjectEvents(ctx, events.ListOf(events.NewConfig("iss", nodeIDs)))
	if err != nil {
		return fmt.Errorf("failed to send reconfiguration event: %w", err)
	}
	m.Net.CloseOldConnections(ctx, nodes)

	return nil
}
func (m *Manager) UpdateReconfigurationVotes(vs *hierarchical.ValidatorSet) error {
	fmt.Println(">>>>")
	fmt.Println(vs)
	fmt.Println(len(vs.Validators))
	h, err := vs.Hash()
	if err != nil {
		return err
	}
	m.ReconfigurationVotes[string(h)]++
	votes := int(m.ReconfigurationVotes[string(h)])
	if votes < (2*m.FaultyNumber + 1) {
		return nil
	}
	newConfigHash, err := vs.Hash()
	if err != nil {
		return err
	}

	if bytes.Equal(m.LastReconfigurationHash, newConfigHash) {
		return nil
	}

	fmt.Println(vs)

	fmt.Println("We get enough votes - start reconfiguration: ", votes)
	m.Validators = vs.Validators
	m.ValidatorsNumber = len(m.Validators)
	m.FaultyNumber = f(m.ValidatorsNumber)
	m.LastReconfigurationHash, err = vs.Hash()
	if err != nil {
		return err
	}

	return m.ReconfigureMirNode(context.TODO())
}

func (m *Manager) SubmitRequests(ctx context.Context, requests []*mirproto.Request) {
	if len(requests) == 0 {
		return
	}
	e := events.NewClientRequests("iss", requests)
	if err := m.MirNode.InjectEvents(ctx, (&events.EventList{}).PushBack(e)); err != nil {
		log.Errorf("failed to submit requests to Mir: %s", err)
	}
	log.Infof("submitted %d requests to Mir", len(requests))
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
			found := m.Pool.deleteRequest(h.String())
			if !found {
				log.Errorf("unable to find a request with %v hash", h)
				continue
			}
			msgs = append(msgs, msg)
			log.Infof("got message: to=%s, nonce= %d", msg.Message.To, msg.Message.Nonce)
		case *types.UnverifiedCrossMsg:
			h := msg.Cid()
			found := m.Pool.deleteRequest(h.String())
			if !found {
				log.Errorf("unable to find a request with %v hash", h)
				continue
			}
			crossMsgs = append(crossMsgs, msg.Message)
			log.Infof("got cross-message: to=%s, nonce= %d", msg.Message.To, msg.Message.Nonce)
		default:
			log.Error("got unknown type request in a block")
		}
	}
	return
}

func (m *Manager) GetTransportRequests(msgs []*types.SignedMessage, crossMsgs []*types.UnverifiedCrossMsg) (
	requests []*mirproto.Request,
) {
	requests = append(requests, m.batchSignedMessages(msgs)...)
	requests = append(requests, m.batchCrossMessages(crossMsgs)...)
	return
}

func (m *Manager) newReconfigurationRequest(n uint64, payload []byte) *mirproto.Request {
	r := mirproto.Request{
		ClientId: m.MirID,
		ReqNo:    n,
		Type:     ReconfigurationType,
		Data:     payload,
	}
	return &r
}

// batchPushSignedMessages pushes signed messages into the request pool and sends them to Mir.
func (m *Manager) batchSignedMessages(msgs []*types.SignedMessage) (
	requests []*mirproto.Request,
) {
	for _, msg := range msgs {
		clientID := newMirID(m.SubnetID.String(), msg.Message.From.String())
		nonce := msg.Message.Nonce
		if !m.Pool.isTargetRequest(clientID, nonce) {
			continue
		}

		msgBytes, err := msg.Serialize()
		if err != nil {
			log.Error("unable to serialize message:", err)
			continue
		}
		data := common.NewSignedMessageBytes(msgBytes, nil)

		r := &mirproto.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Type:     TransportType,
			Data:     data,
		}

		m.Pool.addRequest(msg.Cid().String(), r)

		requests = append(requests, r)
	}
	return requests
}

// batchCrossMessages batches cross messages into the request pool and sends them to Mir.
func (m *Manager) batchCrossMessages(crossMsgs []*types.UnverifiedCrossMsg) (
	requests []*mirproto.Request,
) {
	for _, msg := range crossMsgs {
		msn, err := msg.Message.From.Subnet()
		if err != nil {
			log.Error("unable to get subnet from message:", err)
			continue
		}
		clientID := newMirID(msn.String(), msg.Message.From.String())
		nonce := msg.Message.Nonce
		if !m.Pool.isTargetRequest(clientID, nonce) {
			continue
		}

		msgBytes, err := msg.Serialize()
		if err != nil {
			log.Error("unable to serialize cross-message:", err)
			continue
		}

		data := common.NewCrossMessageBytes(msgBytes, nil)
		r := &mirproto.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Type:     TransportType,
			Data:     data,
		}
		m.Pool.addRequest(msg.Cid().String(), r)
		requests = append(requests, r)
	}
	return requests
}

// ID prints Manager ID.
func (m *Manager) ID() string {
	addr := m.Addr.String()
	return fmt.Sprintf("%v:%v", m.SubnetID, addr[len(addr)-6:])
}

func f(n int) int {
	return (n - 1) / 3
}
