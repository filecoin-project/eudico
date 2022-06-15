package mir

import (
	"context"
	"crypto/sha256"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	filcrypto "github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/lib/sigs"
	t "github.com/filecoin-project/mir/pkg/types"
)

var MsgMeta = api.MsgMeta{Type: "mir-message"}

type Crypto interface {

	// Sign signs the provided data and returns the resulting signature.
	// The data to be signed is the concatenation of all the passed byte slices.
	// A signature produced by Sign is verifiable using VerifyNodeSig.
	Sign(data [][]byte) ([]byte, error)

	// VerifyNodeSig verifies a signature produced by the node with ID nodeID over data.
	// Returns nil on success (i.e., if the given signature is valid) and a non-nil error otherwise.
	VerifyNodeSig(data [][]byte, signature []byte, nodeID t.NodeID) error
}

type CryptoManager struct {
	addr   address.Address
	wallet api.Wallet
}

func NewCryptoManager(addr address.Address, wallet api.Wallet) (*CryptoManager, error) {
	if addr.Protocol() != address.SECP256K1 {
		return nil, xerrors.Errorf("must be secp address")
	}
	return &CryptoManager{addr, wallet}, nil
}

func (c *CryptoManager) Sign(data [][]byte) (bytes []byte, err error) {
	signature, err := c.wallet.WalletSign(context.Background(), c.addr, hash(data), MsgMeta)
	bytes, err = signature.MarshalBinary()
	return
}

func (c *CryptoManager) VerifyNodeSig(data [][]byte, signature []byte, nodeID t.NodeID) error {
	var sig filcrypto.Signature
	if err := sig.UnmarshalBinary(signature); err != nil {
		return err
	}

	nodeAddr, err := getAddr(nodeID)
	if err != nil {
		return err
	}

	err = sigs.Verify(&sig, nodeAddr, hash(data))
	if err != nil {
		return err
	}
	return nil
}

func hash(data [][]byte) []byte {
	h := sha256.New()
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

func getAddr(nodeID t.NodeID) (address.Address, error) {
	addrParts := strings.Split(nodeID.Pb(), ":")
	if len(addrParts) != 2 {
		return address.Undef, xerrors.Errorf("invalid node ID: %s", nodeID)
	}
	nodeAddr, err := address.NewFromString(addrParts[1])
	if err != nil {
		return address.Undef, err
	}
	return nodeAddr, nil
}
