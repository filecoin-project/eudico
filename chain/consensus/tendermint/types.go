package tendermint

import (
	"bytes"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/lotus/chain/consensus/common"
)

type RegistrationMessageRequest struct {
	Name  []byte
	Nonce []byte
}

type RegistrationMessageResponse struct {
	Name   []byte
	Offset int64
	Nonce  []byte
}

func DecodeRegistrationMessageRequest(b []byte) (*RegistrationMessageRequest, error) {
	var msg RegistrationMessageRequest
	if err := msg.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, err
	}

	return &msg, nil
}

func DecodeRegistrationMessageResponse(b []byte) (*RegistrationMessageResponse, error) {
	var msg RegistrationMessageResponse

	if err := msg.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, err
	}

	return &msg, nil
}

func (msg *RegistrationMessageRequest) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := msg.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (msg *RegistrationMessageResponse) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := msg.MarshalCBOR(buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func NewRegistrationMessageBytes(name address.SubnetID, nonce []byte) ([]byte, error) {
	msg := RegistrationMessageRequest{
		Name:  []byte(name.String()),
		Nonce: nonce,
	}
	b, err := msg.Serialize()
	if err != nil {
		return nil, err
	}
	b = append(b, common.RegistrationMessageType)
	return b, nil
}
