package subnetmgr

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
)

func TestSer(t *testing.T) {
	id, _ := address.NewIDAddress(0)
	addp := &hierarchical.ConstructParams{
		Parent: hierarchical.SubnetID{
			Parent: "/root",
			Actor:  id,
		},
		MinValidatorStake: abi.NewTokenAmount(1),
		Name:              "test",
		Consensus:         hierarchical.PoW,
		CheckPeriod:       abi.ChainEpoch(10),
		Genesis:           []byte{1},
	}

	seraddp, err := actors.SerializeParams(addp)
	require.NoError(t, err)
	fmt.Println(">>>>>", seraddp)
	fmt.Println(">>>>>>>>>< Deserialize")
	pp := &hierarchical.ConstructParams{}
	aerr := pp.UnmarshalCBOR(bytes.NewReader(seraddp))
	require.NoError(t, aerr)
	fmt.Println(">>>>>>>", pp, err)
	require.Equal(t, addp, pp)
}

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
