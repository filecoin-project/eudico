package tendermint

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
)

func TestTendermintMessageCache(t *testing.T) {
	c := newMessageCache()

	goodMessage := "1"
	badMessage := "2"

	shouldSend := c.shouldSendMessage(goodMessage)
	require.Equal(t, true, shouldSend)
	_, sent := c.getInfo(goodMessage)
	require.Equal(t, false, sent)

	c.addSentMessage(goodMessage, abi.ChainEpoch(1))
	sentAt, sent := c.getInfo(goodMessage)
	require.Equal(t, true, sent)
	require.Equal(t, abi.ChainEpoch(1), sentAt)

	shouldSend = c.shouldSendMessage(goodMessage)
	require.Equal(t, false, shouldSend)
	_, sent = c.getInfo(goodMessage)
	require.Equal(t, true, sent)

	// ---

	shouldSend = c.shouldSendMessage(badMessage)
	require.Equal(t, true, shouldSend)
	_, sent = c.getInfo(badMessage)
	require.Equal(t, false, sent)

	c.clearSentMessages(finalityWait + 2)
	shouldSend = c.shouldSendMessage(badMessage)
	require.Equal(t, true, shouldSend)
	_, sent = c.getInfo(badMessage)
	require.Equal(t, false, sent)

}
