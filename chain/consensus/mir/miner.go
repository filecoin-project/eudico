package mir

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	t "github.com/filecoin-project/mir/pkg/types"
)

type miner struct {
	netName    dtypes.NetworkName
	subnetID   address.SubnetID
	addr       address.Address
	validators []hierarchical.Validator
	api        v1api.FullNode
	cache      *requestCache
}

func newMiner(ctx context.Context, addr address.Address, api v1api.FullNode) (*miner, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}
	subnetID := address.SubnetID(netName)

	var validators []hierarchical.Validator

	minersEnv := os.Getenv(MirMinersEnv)
	if minersEnv != "" {
		validators, err = hierarchical.ValidatorsFromString(minersEnv)
		if err != nil {
			return nil, xerrors.Errorf("failed to get validators from string: %s", err)
		}
	} else {
		if subnetID == address.RootSubnet {
			return nil, xerrors.New("can't be run in the rootnet without validators")
		}
		validators, err = api.SubnetStateGetValidators(ctx, subnetID)
		if err != nil {
			return nil, xerrors.New("failed to get validators from state")
		}
	}
	if len(validators) == 0 {
		return nil, xerrors.New("empty validator set")
	}

	m := miner{
		addr:       addr,
		subnetID:   subnetID,
		netName:    netName,
		validators: validators,
		api:        api,
		cache:      newRequestCache(),
	}

	return &m, nil
}

func (m *miner) mirID() string {
	return fmt.Sprintf("%s:%s", m.subnetID, m.addr)
}

// getRequest gets the request from the cache.
func (m *miner) getRequest(ref *RequestRef) (Request, bool) {
	return m.cache.getRequest(string(ref.Hash))
}

// getMessagesByHashes gets requests from the cache and extracts Filecoin messages.
func (m *miner) getMessagesByHashes(hashes []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	for _, h := range hashes {
		req, found := m.cache.getDelRequest(string(h))
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

func (m *miner) addSignedMessages(dst []*RequestRef, msgs []*types.SignedMessage) ([]*RequestRef, error) {
	for _, msg := range msgs {
		hash := msg.Cid()
		clientID := fmt.Sprintf("%s:%s", m.subnetID, msg.Message.From.String())
		r := RequestRef{
			ClientID: t.ClientID(clientID),
			ReqNo:    t.ReqNo(msg.Message.Nonce),
			Type:     common.SignedMessageType,
			Hash:     hash.Bytes(),
		}
		m.cache.addRequest(string(hash.Bytes()), msg)
		dst = append(dst, &r)
	}

	return dst, nil
}

func (m *miner) addCrossMessages(dst []*RequestRef, crossMsgs []*types.UnverifiedCrossMsg) ([]*RequestRef, error) {
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
		m.cache.addRequest(string(hash.Bytes()), msg)
	}

	return dst, nil
}
