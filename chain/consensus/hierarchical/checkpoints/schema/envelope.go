package schema

import (
	"bytes"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/node/bindnode"
	"github.com/ipld/go-ipld-prime/schema"
	"golang.org/x/xerrors"
)

type SigEnvelope interface {
	Type() types.EnvelopeType
	MarshalCBOR() ([]byte, error)
	UnmarshalCBOR(b []byte) error
}

var SingleSignEnvSchema schema.Type

type SingleSignEnvelope struct {
	Address   string
	IDAddress string
	Signature []byte
}

func init() {
	SingleSignEnvSchema = initSingleSignEnvSchema()
}

var _ SigEnvelope = &SingleSignEnvelope{}

func initSingleSignEnvSchema() schema.Type {
	ts := schema.TypeSystem{}
	ts.Init()
	ts.Accumulate(schema.SpawnString("String"))
	ts.Accumulate(schema.SpawnBytes("Bytes"))

	ts.Accumulate(schema.SpawnStruct("SingleSignEnvelope",
		[]schema.StructField{
			schema.SpawnStructField("Address", "String", false, false),
			schema.SpawnStructField("IDAddress", "String", false, false),
			schema.SpawnStructField("Signature", "Bytes", false, false),
		},
		schema.SpawnStructRepresentationMap(map[string]string{}),
	))

	return ts.TypeByName("SingleSignEnvelope")
}

func NewSingleSignEnvelope(addr address.Address, idAddr address.Address, sig []byte) *SingleSignEnvelope {
	return &SingleSignEnvelope{addr.String(), idAddr.String(), sig}
}

func (s *SingleSignEnvelope) Type() types.EnvelopeType {
	return types.SingleSignature
}

// MarshalCBOR the envelope
func (s *SingleSignEnvelope) MarshalCBOR() ([]byte, error) {
	node := bindnode.Wrap(s, SingleSignEnvSchema)
	nodeRepr := node.Representation()
	var buf bytes.Buffer
	err := dagcbor.Encode(nodeRepr, &buf)
	if err != nil {
		return nil, err
	}
	// TODO: Consider returning io.Writer
	return buf.Bytes(), nil
}

// UnmarshalCBOR the envelope
// TODO: Consider accepting io.Reader as input
func (s *SingleSignEnvelope) UnmarshalCBOR(b []byte) error {
	nb := bindnode.Prototype(s, SingleSignEnvSchema).NewBuilder()
	err := dagcbor.Decode(nb, bytes.NewReader(b))
	if err != nil {
		return err
	}
	n := bindnode.Unwrap(nb.Build())

	sg, ok := n.(*SingleSignEnvelope)
	if !ok {
		return xerrors.Errorf("Unmarshalled node not of type SingleSignEnvelope")
	}
	*s = *sg

	return nil
}
