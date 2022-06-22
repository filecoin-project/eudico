package submgr

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

func TestPoolTopDown(t *testing.T) {
	cm := newCrossMsgPool()
	id := address.SubnetID("test")
	var nonce uint64 = 3
	height := abi.ChainEpoch(5)
	cm.applyTopDown(nonce, id, height)
	cm.applyTopDown(nonce+1, id, height)
	require.False(t, cm.isTopDownApplied(nonce, id, height))
	require.False(t, cm.isTopDownApplied(nonce+1, id, height))
	require.False(t, cm.isTopDownApplied(nonce+2, id, height))

	// In the next epoch we consider the previous ones as applied
	height++
	cm.applyTopDown(nonce+2, id, height)
	require.True(t, cm.isTopDownApplied(nonce, id, height))
	require.True(t, cm.isTopDownApplied(nonce+1, id, height))
	require.False(t, cm.isTopDownApplied(nonce+2, id, height))

	// After the finality threshold we are free to re-propose previous ones if needed.
	// (state changes would probably have propagated already).
	height += finalityWait
	cm.applyTopDown(nonce+3, id, height)
	require.False(t, cm.isTopDownApplied(nonce+2, id, height))
	require.False(t, cm.isTopDownApplied(nonce+3, id, height))
	height++
	require.True(t, cm.isTopDownApplied(nonce+3, id, height))
}

func TestPoolBottomUp(t *testing.T) {
	cm := newCrossMsgPool()
	id := address.SubnetID("test")
	var nonce uint64 = 3
	height := abi.ChainEpoch(5)
	cm.applyBottomUp(nonce, id, height)
	cm.applyBottomUp(nonce+1, id, height)
	require.False(t, cm.isBottomUpApplied(nonce, id, height))
	require.False(t, cm.isBottomUpApplied(nonce+1, id, height))
	require.False(t, cm.isBottomUpApplied(nonce+2, id, height))

	// In the next epoch we consider the previous ones as applied
	height++
	cm.applyBottomUp(nonce+2, id, height)
	require.True(t, cm.isBottomUpApplied(nonce, id, height))
	require.True(t, cm.isBottomUpApplied(nonce+1, id, height))
	require.False(t, cm.isBottomUpApplied(nonce+2, id, height))

	// After the finality threshold we are free to re-propose previous ones if needed.
	// (state changes would probably have propagated already).
	height += finalityWait
	cm.applyBottomUp(nonce+3, id, height)
	require.False(t, cm.isBottomUpApplied(nonce+2, id, height))
	require.False(t, cm.isBottomUpApplied(nonce+3, id, height))
	height++
	require.True(t, cm.isBottomUpApplied(nonce+3, id, height))
}
