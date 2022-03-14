package schema

import (
	"bytes"
	"encoding"

	"github.com/multiformats/go-varint"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
)

type Signature struct {
	// SignatureID defines the protocol used for the checkpoint signature
	SignatureID types.EnvelopeType
	// Signature data
	Sig []byte
}

var (
	_ encoding.BinaryMarshaler   = (*Signature)(nil)
	_ encoding.BinaryUnmarshaler = (*Signature)(nil)
)

func NewSignature(e SigEnvelope, t types.EnvelopeType) (*Signature, error) {
	b, err := e.MarshalCBOR()
	if err != nil {
		return nil, err
	}
	return &Signature{t, b}, nil
}

// Equal determines if two signature values are equal.
func (m Signature) Equal(other Signature) bool {
	return m.SignatureID == other.SignatureID && bytes.Equal(m.Sig, other.Sig)
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (m Signature) MarshalBinary() ([]byte, error) {
	varintSize := varint.UvarintSize(uint64(m.SignatureID))
	buf := make([]byte, varintSize+len(m.Sig))
	varint.PutUvarint(buf, uint64(m.SignatureID))
	if len(m.Sig) != 0 {
		copy(buf[varintSize:], m.Sig)
	}
	return buf, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (m *Signature) UnmarshalBinary(data []byte) error {
	id, sigLen, err := varint.FromUvarint(data)
	if err != nil {
		return err
	}
	m.SignatureID = types.EnvelopeType(id)

	// We can't hold onto the input data. Make a copy.
	innerData := data[sigLen:]
	m.Sig = make([]byte, len(innerData))
	copy(m.Sig, innerData)

	return nil
}
