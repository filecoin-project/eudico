package common

const (
	ConfigMessageType       = 0 // Mir config request
	SignedMessageType       = 1 // Eudico signed message
	CrossMessageType        = 2 // Eudico cross-message
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
