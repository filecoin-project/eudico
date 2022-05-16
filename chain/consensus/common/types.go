package common

const (
	SignedMessageType       = 1
	CrossMessageType        = 2
	RegistrationMessageType = 3 // Tendermint specific message type
)

func NewSignedMessageBytes(msg, opaque []byte) []byte {
	var payload []byte
	payload = append(msg, opaque...)
	payload = append(payload, SignedMessageType)
	return payload
}

func NewCrossMessageBytes(msg, opaque []byte) []byte {
	var payload []byte
	payload = append(msg, opaque...)
	payload = append(payload, CrossMessageType)
	return payload
}
