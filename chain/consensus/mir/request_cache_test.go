package mir

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMirRequestCache(t *testing.T) {
	c := newRequestCache()

	c.addIfNotExist("client1", "key1", 0)

	r, ok := c.getRequest("key1")
	require.Equal(t, true, ok)
	require.Equal(t, 0, r)

	c.getDel("key1")
	require.Equal(t, true, ok)
	require.Equal(t, 0, r)

	_, ok = c.getRequest("key1")
	require.Equal(t, false, ok)
}
