package mir

import (
	"testing"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"
	"github.com/stretchr/testify/require"

	mirrequest "github.com/filecoin-project/mir/pkg/pb/requestpb"
)

func TestMirRequestPool(t *testing.T) {
	p := newRequestPool()

	c1 := cid.NewCidV0(u.Hash([]byte("req1")))
	c2 := cid.NewCidV0(u.Hash([]byte("req2")))

	exist := p.addRequest(c1.String(), &mirrequest.Request{
		ClientId: "client1", ReqNo: 1, Data: []byte{},
	})
	require.Equal(t, false, exist)

	exist = p.deleteRequest(c1.String())
	require.Equal(t, true, exist)

	exist = p.deleteRequest(c2.String())
	require.Equal(t, false, exist)
}
