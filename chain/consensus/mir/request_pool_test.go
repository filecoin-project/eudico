package mir

import (
	"testing"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"
	"github.com/stretchr/testify/require"
)

func TestMirRequestPool(t *testing.T) {
	p := newRequestPool()

	c1 := cid.NewCidV0(u.Hash([]byte("req1")))
	c2 := cid.NewCidV0(u.Hash([]byte("req2")))
	c3 := cid.NewCidV0(u.Hash([]byte("req3")))

	ok := p.addIfNotExist("client1", c1)
	require.Equal(t, false, ok)

	ok = p.getDel(c1)
	require.Equal(t, true, ok)

	ok = p.addIfNotExist("client1", c2)
	require.Equal(t, false, ok)
	ok = p.addIfNotExist("client1", c3)
	require.Equal(t, true, ok)

}
