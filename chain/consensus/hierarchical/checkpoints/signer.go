package checkpoint

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/lotus/lib/sigs"
	"golang.org/x/xerrors"
)

var CheckpointMsgType = api.MsgMeta{Type: "checkpoint"}

// Signer implements the logic to sign and verify a checkpoint.
//
// Each subnet may choose to implement their own signature and
// verification strategies
// for checkpoints. A subnet shard looking to implement their own
// verifier will need to implement this interface with the desired logic.
type Signer interface {
	Sign(ctx context.Context, w api.Wallet, addr address.Address, c *schema.Checkpoint, opts ...SigningOpts) error
	Verify(c *schema.Checkpoint) (*AddrInfo, error)
}

type AddrInfo struct {
	Addr   address.Address
	IDAddr address.Address
}

var _ Signer = SingleSigner{}

// SingleSignVerifier is a simple verifier that checks
// if the signature envolope included in the checkpoint is valid.
type SingleSigner struct{}

func NewSingleSigner() SingleSigner {
	return SingleSigner{}
}

// signingOpts are additional options for the signature
type signingOpts struct {
	idAddr address.Address
}

// apply applies the given options to this config
func (c *signingOpts) apply(opts ...SigningOpts) error {
	for i, opt := range opts {
		if err := opt(c); err != nil {
			return fmt.Errorf("signing option %d failed: %s", i, err)
		}
	}
	return nil
}

// IDAddr to include in signature envelope
func IDAddr(id address.Address) SigningOpts {
	return func(c *signingOpts) error {
		if id.Protocol() != address.ID {
			return xerrors.Errorf("IDAddress not of type address")
		}
		c.idAddr = id
		return nil
	}
}

type SigningOpts func(*signingOpts) error

func (v SingleSigner) Sign(ctx context.Context, w api.Wallet, addr address.Address, c *schema.Checkpoint, opts ...SigningOpts) error {
	// Check if it is a pkey and not an ID.
	if addr.Protocol() != address.SECP256K1 {
		return xerrors.Errorf("must be secp address")
	}

	var cfg signingOpts
	if err := cfg.apply(opts...); err != nil {
		return err
	}

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
	sig, err := schema.NewSignature(schema.NewSingleSignEnvelope(addr, cfg.idAddr, rawSig), types.SingleSignature)
	if err != nil {
		return err
	}
	c.Signature, err = sig.MarshalBinary()

	return err
}

func (v SingleSigner) Verify(c *schema.Checkpoint) (*AddrInfo, error) {
	// Collect envelope from signature in checkpoint.
	sig := schema.Signature{}
	err := sig.UnmarshalBinary(c.Signature)
	if err != nil {
		return nil, err
	}
	// Check if the envelope has the right type.
	if sig.SignatureID != types.SingleSignature {
		return nil, xerrors.Errorf("wrong signer. Envelope is not of SingleSignType.")
	}

	// Unmarshal the envelope.
	e := schema.SingleSignEnvelope{}
	err = e.UnmarshalCBOR(sig.Sig)
	if err != nil {
		return nil, err
	}
	// Get Cid of checkpoint to check signature.
	cid, err := c.Cid()
	if err != nil {
		return nil, err
	}
	// Gather raw signature from envelope
	checkSig := &crypto.Signature{}
	err = checkSig.UnmarshalBinary(e.Signature)
	if err != nil {
		return nil, err
	}
	// Get address
	addr, err := address.NewFromString(e.Address)
	if err != nil {
		return nil, err
	}
	idAddr := address.Undef
	if e.IDAddress != "" {
		idAddr, err = address.NewFromString(e.IDAddress)
		if err != nil {
			return nil, err
		}
	}
	// Verify signature
	err = sigs.Verify(checkSig, addr, cid.Hash())
	if err != nil {
		return nil, err
	}
	return &AddrInfo{addr, idAddr}, nil
}
