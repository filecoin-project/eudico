package mir

import (
	"context"
	"crypto/sha256"
	"strings"

	libp2pcrypto "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	filcrypto "github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/lib/sigs"
	mircrypto "github.com/filecoin-project/mir/pkg/crypto"
	t "github.com/filecoin-project/mir/pkg/types"
)

var _ mircrypto.Impl = &CryptoManager{}

type NodeCrypto interface {
	Sign(ctx context.Context, id peer.ID, msg []byte) ([]byte, error)
}

// CryptoManager implements Mir's crypto interface.
// It uses the node's libp2p private key to sign Mir messages.
// libp2p public keys and IDs correspond to each other.
// Because of that we don't need trust establishment mechanisms like CA.
// We ED25519 signing algorithm, because Filecoin uses it.
// At present, libp2p doesn't support BLS. If we want to switch to BLS in the future,
// we will need to reimplement CryptoManager.
// Probably we will need to introduce our own consensus module keys and establish trust somehow.
type CryptoManager struct {
	id  peer.ID
	api NodeCrypto
}

func NewCryptoManager(id peer.ID, api NodeCrypto) (*CryptoManager, error) {
	pub, err := id.ExtractPublicKey()
	if err != nil {
		return nil, xerrors.Errorf("invalid peer ID: ", err)
	}
	if pub.Type() != libp2pcrypto.Ed25519 {
		return nil, xerrors.New("must be ED25519 public key")
	}

	return &CryptoManager{id, api}, nil
}

// Sign signs the provided data and returns the resulting signature.
// The data to be signed is the concatenation of all the passed byte slices.
// A signature produced by Sign is verifiable using VerifyNodeSig or VerifyClientSig,
// if, respectively, RegisterNodeKey or RegisterClientKey has been invoked with the corresponding public key.
// Note that the private key used to produce the signature cannot be set ("registered") through this interface.
// Storing and using the private key is completely implementation-dependent.
func (c *CryptoManager) Sign(data [][]byte) (bytes []byte, err error) {
	return c.api.Sign(context.Background(), c.id, hash(data))

}

// VerifyNodeSig verifies a signature produced by the node with numeric ID nodeID over data.
// Returns nil on success (i.e., if the given signature is valid) and a non-nil error otherwise.
// Note that RegisterNodeKey must be used to register the node's public key before calling VerifyNodeSig,
// otherwise VerifyNodeSig will fail.
func (c *CryptoManager) VerifyNodeSig(data [][]byte, signature []byte, nodeID t.NodeID) error {
	node, err := getID(nodeID.Pb())
	if err != nil {
		return err
	}
	return c.verifySig(data, signature, node)
}

// VerifyClientSig verifies a signature produced by the client with numeric ID clientID over data.
// Returns nil on success (i.e., if the given signature is valid) and a non-nil error otherwise.
// Note that RegisterClientKey must be used to register the client's public key before calling VerifyClientSig,
// otherwise VerifyClientSig will fail.
func (c *CryptoManager) VerifyClientSig(data [][]byte, signature []byte, clientID t.ClientID) error {
	client, err := getID(clientID.Pb())
	if err != nil {
		return err
	}
	return c.verifySig(data, signature, client)
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

func getID(nodeID string) (address.Address, error) {
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
