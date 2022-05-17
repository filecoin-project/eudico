package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

func TestGetSet(t *testing.T) {
	ctx := context.Background()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	h, err := libp2p.New()
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
	out1 := &sca.CrossMsgs{}
	found, err := r.getLocal(ctx, msg.Cid(), out)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, out1.Msgs)
	require.Nil(t, out1.Metas)
	require.NoError(t, err)
	err = r.setLocal(ctx, msg.Cid(), out)
	require.NoError(t, err)
	out2 := &sca.CrossMsgs{}
	found, err = r.getLocal(ctx, msg.Cid(), out2)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, out, out2)
}

func TestSerializeResolveMsg(t *testing.T) {
	// TODO:
}

func TestResolveCross(t *testing.T) {
	ctx := context.Background()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	h, err := libp2p.New()
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
	_, found, err := r.ResolveCrossMsgs(ctx, c, address.RootSubnet)
	require.NoError(t, err)
	require.False(t, found)
	err = r.setLocal(ctx, c, out)
	require.NoError(t, err)
	pulled, found, err := r.ResolveCrossMsgs(ctx, c, address.RootSubnet)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, len(pulled), 1)

	// TODO: Test recursive resolve with Metas.
}

func TestWaitResolveCross(t *testing.T) {
	ctx := context.Background()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	h, err := libp2p.New()
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
		err := r.setLocal(ctx, c, out)
		require.NoError(t, err)
	}()

	err = <-found
	require.NoError(t, err)
}

func TestResolveLocked(t *testing.T) {
	ctx := context.Background()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	h, err := libp2p.New()
	require.NoError(t, err)
	ps, err := pubsub.NewGossipSub(context.TODO(), h)
	require.NoError(t, err)
	out := &atomic.LockedState{S: []byte("test")}
	r := NewResolver(h.ID(), ds, ps, address.RootSubnet)
	c, _ := out.Cid()
	addr := tutil.NewIDAddr(t, 101)
	_, found, err := r.ResolveLockedState(ctx, c, address.RootSubnet, addr)
	require.NoError(t, err)
	require.False(t, found)
	err = r.setLocal(ctx, c, out)
	require.NoError(t, err)
	pulled, found, err := r.ResolveLockedState(ctx, c, address.RootSubnet, addr)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, pulled, out)
}

func TestWaitResolveLocked(t *testing.T) {
	ctx := context.Background()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	h, err := libp2p.New()
	require.NoError(t, err)
	ps, err := pubsub.NewGossipSub(context.TODO(), h)
	require.NoError(t, err)
	out := &atomic.LockedState{S: []byte("test")}
	r := NewResolver(h.ID(), ds, ps, address.RootSubnet)
	c, _ := out.Cid()

	// Wait for resolution.
	addr := tutil.NewIDAddr(t, 101)
	found := r.WaitLockedStateResolved(context.TODO(), c, address.RootSubnet, addr)
	go func() {
		// Wait one second, and store cross-msgs locally
		time.Sleep(1 * time.Second)
		err := r.setLocal(ctx, c, out)
		require.NoError(t, err)
	}()

	err = <-found
	require.NoError(t, err)
}
