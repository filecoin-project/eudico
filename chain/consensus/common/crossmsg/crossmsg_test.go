package crossmsg

import (
	"sort"
	"testing"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/types"
)

func TestOrderMsgs(t *testing.T) {
	l := MessageArray([]*types.Message{newMsg(7), newMsg(5), newMsg(3), newMsg(9)})
	sort.Sort(l)
	require.Equal(t, l[0].Nonce, uint64(3))
	require.Equal(t, l[1].Nonce, uint64(5))
	require.Equal(t, l[2].Nonce, uint64(7))
	require.Equal(t, l[3].Nonce, uint64(9))
}

func newMsg(nonce uint64) *types.Message {
	return &types.Message{
		To:     address.Undef,
		From:   address.Undef,
		Value:  big.Zero(),
		Nonce:  nonce,
		Params: nil,
	}
}
