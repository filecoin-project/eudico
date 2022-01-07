package subnetmgr

import (
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/stretchr/testify/require"
)

func TestPoolTopDown(t *testing.T) {
	cm := newCrossMsgPool()
	id := hierarchical.SubnetID("test")
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
	height += finalityThreshold
	cm.applyTopDown(nonce+3, id, height)
	require.False(t, cm.isTopDownApplied(nonce+2, id, height))
	require.False(t, cm.isTopDownApplied(nonce+3, id, height))
	height++
	require.True(t, cm.isTopDownApplied(nonce+3, id, height))
}

func TestPoolDownTop(t *testing.T) {
	cm := newCrossMsgPool()
	id := hierarchical.SubnetID("test")
	var nonce uint64 = 3
	height := abi.ChainEpoch(5)
	cm.applyDownTop(nonce, id, height)
	cm.applyDownTop(nonce+1, id, height)
	require.False(t, cm.isDownTopApplied(nonce, id, height))
	require.False(t, cm.isDownTopApplied(nonce+1, id, height))
	require.False(t, cm.isDownTopApplied(nonce+2, id, height))

	// In the next epoch we consider the previous ones as applied
	height++
	cm.applyDownTop(nonce+2, id, height)
	require.True(t, cm.isDownTopApplied(nonce, id, height))
	require.True(t, cm.isDownTopApplied(nonce+1, id, height))
	require.False(t, cm.isDownTopApplied(nonce+2, id, height))

	// After the finality threshold we are free to re-propose previous ones if needed.
	// (state changes would probably have propagated already).
	height += finalityThreshold
	cm.applyDownTop(nonce+3, id, height)
	require.False(t, cm.isDownTopApplied(nonce+2, id, height))
	require.False(t, cm.isDownTopApplied(nonce+3, id, height))
	height++
	require.True(t, cm.isDownTopApplied(nonce+3, id, height))
}
