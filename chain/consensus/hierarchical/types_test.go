package hierarchical_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
)

func TestBottomUp(t *testing.T) {
	testBottomUp(t, "/root/f01", "/root/f01/f02", false)
	testBottomUp(t, "/root/f03/f01", "/root/f01/f02", true)
	testBottomUp(t, "/root/f03/f01/f04", "/root/f03/f01/f05", true)
	testBottomUp(t, "/root/f03/f01", "/root/f03/f02", true)
}

func testBottomUp(t *testing.T, from, to string, bottomup bool) {
	sfrom, err := address.SubnetIDFromString(from)
	require.NoError(t, err)
	sto, err := address.SubnetIDFromString(to)
	require.NoError(t, err)
	require.Equal(t, hierarchical.IsBottomUp(sfrom, sto), bottomup)
}

func TestApplyAsBottomUp(t *testing.T) {
	testApplyAsBottomUp(t, "/root/f01", "/root", "/root/f01/f02", false)
	testApplyAsBottomUp(t, "/root/f01", "/root/f01/f02/f03", "/root/f01", true)
	testApplyAsBottomUp(t, "/root/f01", "/root/f01/f02/f03", "/root/f02/f01", true)
	testApplyAsBottomUp(t, "/root/f01", "/root/f02/f01/f03", "/root/f01/f02", false)
}

func testApplyAsBottomUp(t *testing.T, curr, from, to string, bottomup bool) {
	sfrom, err := address.SubnetIDFromString(from)
	require.NoError(t, err)
	sto, err := address.SubnetIDFromString(to)
	require.NoError(t, err)
	ff, _ := address.NewHCAddress(sfrom, tutil.NewIDAddr(t, 101))
	tt, _ := address.NewHCAddress(sto, tutil.NewIDAddr(t, 101))
	scurr, err := address.SubnetIDFromString(curr)
	require.NoError(t, err)
	bu, err := hierarchical.ApplyAsBottomUp(scurr, &types.Message{From: ff, To: tt})
	require.NoError(t, err)
	require.Equal(t, bu, bottomup)
}

func TestIsCrossMsg(t *testing.T) {
	sfrom, err := address.SubnetIDFromString("/root/f01")
	require.NoError(t, err)
	sto, err := address.SubnetIDFromString("/root/f02")
	require.NoError(t, err)
	ff, _ := address.NewHCAddress(sfrom, tutil.NewIDAddr(t, 101))
	tt, _ := address.NewHCAddress(sto, tutil.NewIDAddr(t, 101))
	msg := types.Message{From: ff, To: tt}
	require.Equal(t, hierarchical.IsCrossMsg(&msg), true)

	ff = tutil.NewIDAddr(t, 101)
	msg = types.Message{From: ff, To: tt}
	require.Equal(t, hierarchical.IsCrossMsg(&msg), false)

	ff = tutil.NewIDAddr(t, 101)
	tt = tutil.NewIDAddr(t, 102)
	msg = types.Message{From: ff, To: tt}
	require.Equal(t, hierarchical.IsCrossMsg(&msg), false)
}
