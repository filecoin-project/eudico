package tendermint

import (
	"bytes"

	"github.com/filecoin-project/go-address"
)

const (
	SignedMessageType       = 1
	CrossMessageType        = 2
	RegistrationMessageType = 3
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
	b = append(b, RegistrationMessageType)
	return b, nil
}

func NewSignedMessageBytes(msg []byte) []byte {
	var payload []byte
	payload = append(msg, SignedMessageType)
	return payload
}

func NewCrossMessageBytes(msg []byte) []byte {
	var payload []byte
	payload = append(msg, CrossMessageType)
	return payload
}
