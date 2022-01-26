package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
)

func TestGetSet(t *testing.T) {
	ds := datastore.NewMapDatastore()
	h, err := libp2p.New(context.TODO())
	require.NoError(t, err)
	ps, err := pubsub.NewGossipSub(context.TODO(), h)
	require.NoError(t, err)
	addr := tutil.NewIDAddr(t, 101)
	msg := ltypes.Message{
		To:         addr,
		From:       addr,
		Value:      abi.NewTokenAmount(1),
		Nonce:      2,
		GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
		GasFeeCap:  ltypes.NewInt(0),
		GasPremium: ltypes.NewInt(0),
		Params:     nil,
	}
	out := &sca.CrossMsgs{Msgs: []ltypes.Message{msg}}
	r := NewResolver(h.ID(), ds, ps, address.RootSubnet)
	out1, found, err := r.getLocal(msg.Cid())
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, out1)
	require.NoError(t, err)
	err = r.setLocal(msg.Cid(), out)
	require.NoError(t, err)
	out2, found, err := r.getLocal(msg.Cid())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, out, out2)
}

func TestResolve(t *testing.T) {
	ds := datastore.NewMapDatastore()
	h, err := libp2p.New(context.TODO())
	require.NoError(t, err)
	ps, err := pubsub.NewGossipSub(context.TODO(), h)
	require.NoError(t, err)
	addr := tutil.NewIDAddr(t, 101)
	msg := ltypes.Message{
		To:         addr,
		From:       addr,
		Value:      abi.NewTokenAmount(1),
		Nonce:      2,
		GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
		GasFeeCap:  ltypes.NewInt(0),
		GasPremium: ltypes.NewInt(0),
		Params:     nil,
	}
	out := &sca.CrossMsgs{Msgs: []ltypes.Message{msg}}
	r := NewResolver(h.ID(), ds, ps, address.RootSubnet)
	c, _ := out.Cid()
	_, found, err := r.ResolveCrossMsgs(c, address.RootSubnet)
	require.NoError(t, err)
	require.False(t, found)
	err = r.setLocal(c, out)
	require.NoError(t, err)
	pulled, found, err := r.ResolveCrossMsgs(c, address.RootSubnet)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, len(pulled), 1)

	// TODO: Test recursive resolve with Metas.
}

func TestWaitResolve(t *testing.T) {
	ds := datastore.NewMapDatastore()
	h, err := libp2p.New(context.TODO())
	require.NoError(t, err)
	ps, err := pubsub.NewGossipSub(context.TODO(), h)
	require.NoError(t, err)
	addr := tutil.NewIDAddr(t, 101)
	msg := ltypes.Message{
		To:         addr,
		From:       addr,
		Value:      abi.NewTokenAmount(1),
		Nonce:      2,
		GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
		GasFeeCap:  ltypes.NewInt(0),
		GasPremium: ltypes.NewInt(0),
		Params:     nil,
	}
	out := &sca.CrossMsgs{Msgs: []ltypes.Message{msg}}
	r := NewResolver(h.ID(), ds, ps, address.RootSubnet)
	c, _ := out.Cid()

	// Wait for resolution.
	found := r.WaitCrossMsgsResolved(context.TODO(), c, address.RootSubnet)
	go func() {
		// Wait one second, and store cross-msgs locally
		time.Sleep(1 * time.Second)
		err = r.setLocal(c, out)
		require.NoError(t, err)
	}()

	err = <-found
	require.NoError(t, err)
}
