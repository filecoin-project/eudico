package hierarchical_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
	tutil "github.com/filecoin-project/specs-actors/v7/support/testing"
	"github.com/stretchr/testify/require"
)

func TestBottomUp(t *testing.T) {
	testBottomUp(t, "/root/a", "/root/a/b", false)
	testBottomUp(t, "/root/c/a", "/root/a/b", true)
	testBottomUp(t, "/root/c/a/d", "/root/c/a/e", true)
	testBottomUp(t, "/root/c/a", "/root/c/b", true)
}

func testBottomUp(t *testing.T, from, to string, bottomup bool) {
	require.Equal(t, hierarchical.IsBottomUp(
		address.SubnetID(from), address.SubnetID(to)), bottomup)
}

func TestApplyAsBottomUp(t *testing.T) {
	testApplyAsBottomUp(t, "/root/a", "/root", "/root/a/b", false)
	testApplyAsBottomUp(t, "/root/a", "/root/a/b/c", "/root/a", true)
	testApplyAsBottomUp(t, "/root/a", "/root/a/b/c", "/root/b/a", true)
	testApplyAsBottomUp(t, "/root/a", "/root/b/a/c", "/root/a/b", false)
}

func testApplyAsBottomUp(t *testing.T, curr, from, to string, bottomup bool) {
	ff, _ := address.NewHAddress(address.SubnetID(from), tutil.NewIDAddr(t, 101))
	tt, _ := address.NewHAddress(address.SubnetID(to), tutil.NewIDAddr(t, 101))
	bu, err := hierarchical.ApplyAsBottomUp(address.SubnetID(curr), &types.Message{From: ff, To: tt})
	require.NoError(t, err)
	require.Equal(t, bu, bottomup)
}
