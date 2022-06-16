package mir

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	filcrypto "github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/lib/sigs"
	mirTypes "github.com/filecoin-project/mir/pkg/types"
)

type cryptoNode struct {
	w    *wallet.LocalWallet
	addr address.Address
}

var msgMeta = api.MsgMeta{Type: "mir-request"}

func newCryptoNode() (*cryptoNode, error) {
	w, err := wallet.NewWallet(wallet.NewMemKeyStore())
	if err != nil {
		return nil, err
	}

	addr, err := w.WalletNew(context.Background(), types.KTSecp256k1)
	if err != nil {
		return nil, err
	}
	return &cryptoNode{
		w, addr,
	}, nil
}

func (n *cryptoNode) WalletSign(ctx context.Context, k address.Address, msg []byte) (signature *filcrypto.Signature, err error) {
	if k.Protocol() != address.SECP256K1 {
		return nil, xerrors.New("must be SECP address")
	}
	if k != n.addr {
		return nil, xerrors.New("wrong address")
	}
	signature, err = n.w.WalletSign(ctx, k, msg, msgMeta)
	return
}
func (n *cryptoNode) WalletVerify(ctx context.Context, k address.Address, msg []byte, sig *filcrypto.Signature) (bool, error) {
	err := sigs.Verify(sig, k, msg)
	return err == nil, err
}

func TestCryptoManager(t *testing.T) {
	node, err := newCryptoNode()
	require.NoError(t, err)

	addr := node.addr
	c, err := NewCryptoManager(addr, node)
	require.NoError(t, err)

	data := [][]byte{{1, 2, 3}, {1, 2, 3}, {1, 2, 3}, {1, 2, 3}}
	sigBytes, err := c.Sign(data)
	require.NoError(t, err)

	nodeID := mirTypes.NodeID(newMirID("/root", addr.String()))
	err = c.VerifyNodeSig([][]byte{{1, 2, 3}}, sigBytes, nodeID)
	require.Error(t, err)

	clientID := mirTypes.ClientID(newMirID("/root", addr.String()))
	err = c.VerifyClientSig([][]byte{{1, 2, 3}}, sigBytes, clientID)
	require.Error(t, err)

	err = c.VerifyNodeSig([][]byte{{1, 2, 3}}, sigBytes, nodeID)
	require.Error(t, err)

	err = c.VerifyNodeSig(data, []byte{1, 2, 3}, nodeID)
	require.Error(t, err)

	nodeID = mirTypes.NodeID(addr.String())
	err = c.VerifyNodeSig(data, sigBytes, nodeID)
	require.Error(t, err)

	nodeID = mirTypes.NodeID(newMirID("/root:", addr.String()))
	err = c.VerifyNodeSig(data, sigBytes, nodeID)
	require.Error(t, err)
}
