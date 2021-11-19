package naming_test

import (
	"testing"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	tutil "github.com/filecoin-project/specs-actors/v6/support/testing"
	"github.com/stretchr/testify/require"
)

func TestNaming(t *testing.T) {
	addr1 := tutil.NewIDAddr(t, 101)
	addr2 := tutil.NewIDAddr(t, 102)
	root := naming.Root
	net1 := naming.NewSubnetID(root, addr1)
	net2 := naming.NewSubnetID(net1, addr2)

	t.Log("Test actors")
	actor1, err := net1.Actor()
	require.NoError(t, err)
	require.Equal(t, actor1, addr1)
	actor2, err := net2.Actor()
	require.NoError(t, err)
	require.Equal(t, actor2, addr2)
	actorRoot, err := root.Actor()
	require.NoError(t, err)
	require.Equal(t, actorRoot, address.Undef)

	t.Log("Test parents")
	parent1 := net1.Parent()
	require.Equal(t, root, parent1)
	parent2 := net2.Parent()
	require.Equal(t, parent2, net1)
	parentRoot := root.Parent()
	require.Equal(t, parentRoot, naming.Undef)

}
