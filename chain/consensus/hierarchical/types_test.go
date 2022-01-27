package hierarchical_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
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
