package tendermint

import (
	"bytes"

	"github.com/filecoin-project/lotus/chain/types"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
)

const (
	SignedMessageType       = 1
	CrossMessageType        = 2
	RegistrationMessageType = 3
)

type RegistrationMessage struct {
	Name   []byte
	Offset int64
}

func DecodeRegistrationMessage(b []byte) (*RegistrationMessage, error) {
	var msg RegistrationMessage
	if err := msg.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, err
	}

	return &msg, nil
}

func (msg *RegistrationMessage) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := msg.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func NewRegistrationMessageBytes(name hierarchical.SubnetID) ([]byte, error) {
	msg := RegistrationMessage{
		Name: []byte(name.String()),
	}
	b, err := msg.Serialize()
	if err != nil {
		return nil, err
	}
	b = append(b, RegistrationMessageType)
	return b, nil
}

type tendermintBlockInfo struct {
	timestamp uint64
	messages  []*types.SignedMessage
	crossMsgs []*types.Message
	minerAddr []byte
	hash      []byte
}
