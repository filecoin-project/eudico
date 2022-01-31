package hierarchical_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/stretchr/testify/require"
)

func TestSubnetOps(t *testing.T) {
	testParentAndBottomUp(t, "/root/a", "/root/a/b", "/root/a", 2, false)
	testParentAndBottomUp(t, "/root/c/a", "/root/a/b", "/root", 1, true)
	testParentAndBottomUp(t, "/root/c/a/d", "/root/c/a/e", "/root/c/a", 3, true)
	testParentAndBottomUp(t, "/root/c/a", "/root/c/b", "/root/c", 2, true)
}

func testParentAndBottomUp(t *testing.T, from, to, parent string, exl int, bottomup bool) {
	p, l := hierarchical.CommonParent(
		address.SubnetID(from), address.SubnetID(to))
	require.Equal(t, p, address.SubnetID(parent))
	require.Equal(t, exl, l)
	require.Equal(t, hierarchical.IsBottomUp(
		address.SubnetID(from), address.SubnetID(to)), bottomup)
}
