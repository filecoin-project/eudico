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
	mir "github.com/filecoin-project/mir/pkg/modules"
	t "github.com/filecoin-project/mir/pkg/types"
)

var MsgMeta = api.MsgMeta{Type: "mir-request"}

var _ mir.Crypto = &CryptoManager{}

type CryptoManager struct {
	addr   address.Address
	wallet api.Wallet
}

func NewCryptoManager(addr address.Address, wallet api.Wallet) (*CryptoManager, error) {
	if addr.Protocol() != address.SECP256K1 {
		return nil, xerrors.New("must be SECP address")
	}
	return &CryptoManager{addr, wallet}, nil
}

// Sign signs the provided data and returns the resulting signature.
// The data to be signed is the concatenation of all the passed byte slices.
// A signature produced by Sign is verifiable using VerifyNodeSig or VerifyClientSig,
// if, respectively, RegisterNodeKey or RegisterClientKey has been invoked with the corresponding public key.
// Note that the private key used to produce the signature cannot be set ("registered") through this interface.
// Storing and using the private key is completely implementation-dependent.
func (c *CryptoManager) Sign(data [][]byte) (bytes []byte, err error) {
	signature, err := c.wallet.WalletSign(context.Background(), c.addr, hash(data), MsgMeta)
	return signature.MarshalBinary()
}

// VerifyNodeSig verifies a signature produced by the node with numeric ID nodeID over data.
// Returns nil on success (i.e., if the given signature is valid) and a non-nil error otherwise.
// Note that RegisterNodeKey must be used to register the node's public key before calling VerifyNodeSig,
// otherwise VerifyNodeSig will fail.
func (c *CryptoManager) VerifyNodeSig(data [][]byte, signature []byte, nodeID t.NodeID) error {
	nodeAddr, err := getAddr(nodeID.Pb())
	if err != nil {
		return err
	}
	return c.verifySig(data, signature, nodeAddr)
}

// RegisterNodeKey associates a public key with a numeric node ID.
// The representation of the key is implementation-dependent.
// Calls to VerifyNodeSig will fail until RegisterNodeKey is successfully called with the corresponding node ID.
// Returns nil on success, a non-nil error on failure.
func (c *CryptoManager) RegisterNodeKey(pubKey []byte, nodeID t.NodeID) error {
	panic("not implemented")
}

// RegisterClientKey associates a public key with a numeric client ID.
// The representation of the key is implementation-dependent.
// Calls to VerifyClientSig will fail until RegisterClientKey is successfully called with the corresponding client ID.
// Returns nil on success, a non-nil error on failure.
func (c *CryptoManager) RegisterClientKey(pubKey []byte, clientID t.ClientID) error {
	panic("not implemented")
}

// DeleteNodeKey removes the public key associated with nodeID from the module's state.
// Any subsequent call to VerifyNodeSig(..., nodeID) will fail.
func (c *CryptoManager) DeleteNodeKey(nodeID t.NodeID) {
	panic("not implemented")
}

// DeleteClientKey removes the public key associated with clientID from the module's state.
// Any subsequent call to VerifyClientSig(..., clientID) will fail.
func (c *CryptoManager) DeleteClientKey(clientID t.ClientID) {
	panic("not implemented")
}

// VerifyClientSig verifies a signature produced by the client with numeric ID clientID over data.
// Returns nil on success (i.e., if the given signature is valid) and a non-nil error otherwise.
// Note that RegisterClientKey must be used to register the client's public key before calling VerifyClientSig,
// otherwise VerifyClientSig will fail.
func (c *CryptoManager) VerifyClientSig(data [][]byte, signature []byte, clientID t.ClientID) error {
	clientAddr, err := getAddr(clientID.Pb())
	if err != nil {
		return err
	}
	return c.verifySig(data, signature, clientAddr)
}

func (c *CryptoManager) verifySig(data [][]byte, signature []byte, addr address.Address) error {
	var sig filcrypto.Signature
	if err := sig.UnmarshalBinary(signature); err != nil {
		return err
	}

	return sigs.Verify(&sig, addr, hash(data))
}

func hash(data [][]byte) []byte {
	h := sha256.New()
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

func getAddr(nodeID string) (address.Address, error) {
	addrParts := strings.Split(nodeID, ":")
	if len(addrParts) != 2 {
		return address.Undef, xerrors.Errorf("invalid node ID: %s", nodeID)
	}
	nodeAddr, err := address.NewFromString(addrParts[1])
	if err != nil {
		return address.Undef, err
	}
	return nodeAddr, nil
}
