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
	Tag   []byte
	Nonce []byte
}

type RegistrationMessageResponse struct {
	Name   []byte
	Tag    []byte
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

func NewRegistrationMessageBytes(name address.SubnetID, tag, nonce []byte) ([]byte, error) {
	msg := RegistrationMessageRequest{
		Name:  []byte(name.String()),
		Tag:   tag,
		Nonce: nonce,
	}
	b, err := msg.Serialize()
	if err != nil {
		return nil, err
	}
	b = append(b, RegistrationMessageType)
	return b, nil
}

func NewSignedMessageBytes(msg, tag []byte) []byte {
	var payload []byte
	payload = append(msg, tag[:tagLength]...)
	payload = append(payload, SignedMessageType)
	return payload
}

func NewCrossMessageBytes(msg, tag []byte) []byte {
	var payload []byte
	payload = append(msg, tag[:tagLength]...)
	payload = append(payload, CrossMessageType)
	return payload
}
