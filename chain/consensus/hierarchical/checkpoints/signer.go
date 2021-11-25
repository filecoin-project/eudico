package checkpoint

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/lotus/lib/sigs"
	"golang.org/x/xerrors"
)

var CheckpointMsgType = api.MsgMeta{Type: "checkpoint"}

const (
	checkSignCodec  = "/fil/hierarchical/checkpoint"
	checkSignDomain = "fil"
)

// Signer implements the logic to sign and verify a checkpoint.
//
// Each subnet may choose to implement their own signature and
// verification strategies
// for checkpoints. A subnet shard looking to implement their own
// verifier will need to implement this interface with the desired logic.
type Signer interface {
	Sign(ctx context.Context, w api.Wallet, addr address.Address, c *schema.Checkpoint) error
	Verify(c *schema.Checkpoint) (address.Address, error)
}

var _ Signer = SingleSigner{}

// SingleSignVerifier is a simple verifier that checks
// if the signature envolope included in the checkpoint is valid.
type SingleSigner struct{}

func NewSingleSigner() SingleSigner {
	return SingleSigner{}
}

func (v SingleSigner) Sign(ctx context.Context, w api.Wallet, addr address.Address, c *schema.Checkpoint) error {
	// Get CID of checkpoint
	cid, err := c.Cid()
	if err != nil {
		return err
	}
	// Create raw signature
	sign, err := w.WalletSign(ctx, addr, cid.Hash(), CheckpointMsgType)
	if err != nil {
		return err
	}
	rawSig, err := sign.MarshalBinary()
	if err != nil {
		return err
	}
	// Package it inside an envelope and the signature of the checkpoint
	sig, err := schema.NewSignature(schema.NewSingleSignEnvelope(addr, rawSig), types.SingleSignature)
	if err != nil {
		return err
	}
	c.Signature, err = sig.MarshalBinary()

	return err
}

func (v SingleSigner) Verify(c *schema.Checkpoint) (address.Address, error) {
	// Collect envelope from signature in checkpoint.
	sig := schema.Signature{}
	err := sig.UnmarshalBinary(c.Signature)
	if err != nil {
		return address.Undef, err
	}
	// Check if the envelope has the right type.
	if sig.SignatureID != types.SingleSignature {
		return address.Undef, xerrors.Errorf("wrong signer. Envelope is not of SingleSignType.")
	}
	// Unmarshal the envelope.
	e := schema.SingleSignEnvelope{}
	err = e.UnmarshalCBOR(sig.Sig)
	if err != nil {
		return address.Undef, err
	}
	// Get Cid of checkpoint to check signature.
	cid, err := c.Cid()
	if err != nil {
		return address.Undef, err
	}
	// Gather raw signature from envelope
	checkSig := &crypto.Signature{}
	err = checkSig.UnmarshalBinary(e.Signature)
	if err != nil {
		return address.Undef, err
	}
	// Get address
	addr, err := address.NewFromString(e.Address)
	if err != nil {
		return address.Undef, err
	}
	// Verify signature
	err = sigs.Verify(checkSig, addr, cid.Hash())
	if err != nil {
		return address.Undef, err
	}
	return addr, nil
}
