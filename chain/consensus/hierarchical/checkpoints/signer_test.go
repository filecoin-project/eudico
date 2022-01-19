package checkpoint_test

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	checkpoint "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/utils"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	"github.com/stretchr/testify/require"
)

var cb = schema.Linkproto

func TestSimpleSigner(t *testing.T) {
	ctx := context.Background()
	w, err := wallet.NewWallet(wallet.NewMemKeyStore())
	if err != nil {
		t.Fatal(err)
	}
	addr, err := w.WalletNew(ctx, types.KTSecp256k1)
	require.NoError(t, err)

	ver := checkpoint.NewSingleSigner()

	c1, _ := cb.Sum([]byte("a"))
	epoch := abi.ChainEpoch(1000)
	ch := schema.NewRawCheckpoint(address.RootSubnet, epoch)
	ch.SetPrevious(c1)

	// Add child checkpoints
	ch.AddListChilds(utils.GenRandChecks(3))

	// Sign without opts
	err = ver.Sign(ctx, w, addr, ch)
	require.NoError(t, err)
	require.NotEqual(t, len(ch.Signature), 0)

	// Verify
	sigAddr, err := ver.Verify(ch)
	require.Equal(t, addr, sigAddr.Addr)
	require.Equal(t, address.Undef, sigAddr.IDAddr)
	require.NoError(t, err)

	// Verification fails if something in the checkpoint changes
	ch.Data.Epoch = 120
	_, err = ver.Verify(ch)
	require.Error(t, err)

	// Sign with opts
	idaddr := tutil.NewIDAddr(t, 103)
	err = ver.Sign(ctx, w, addr, ch, []checkpoint.SigningOpts{checkpoint.IDAddr(idaddr)}...)
	require.NoError(t, err)
	require.NotEqual(t, len(ch.Signature), 0)

	// Verify
	sigAddr, err = ver.Verify(ch)
	require.Equal(t, addr, sigAddr.Addr)
	require.Equal(t, idaddr, sigAddr.IDAddr)
	require.NoError(t, err)

	// TODO: Test that if the type is not right we return and error.
}
