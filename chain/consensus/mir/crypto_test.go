package mir

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	mirTypes "github.com/filecoin-project/mir/pkg/types"
)

func TestMirCryptoManager(t *testing.T) {
	ctx := context.Background()

	w, err := wallet.NewWallet(wallet.NewMemKeyStore())
	require.NoError(t, err)

	addr, err := w.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)

	c, err := NewCryptoManager(addr, w)
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
