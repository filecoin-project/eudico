package types

import (
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/stretchr/testify/require"
)

func TestEpochs(t *testing.T) {
	period := abi.ChainEpoch(100)
	epoch := abi.ChainEpoch(120)
	require.Equal(t, CheckpointEpoch(epoch, period), abi.ChainEpoch(100))
	require.Equal(t, WindowEpoch(epoch, period), abi.ChainEpoch(200))
}
