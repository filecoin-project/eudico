//stm: #integration
package itests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/itests/kit"
)

func TestConsensus(t *testing.T) {
	//stm: @CHAIN_STATE_MINER_INFO_001
	t.Run("direct", func(t *testing.T) {
		runConsensusTest(t)
	})
	t.Run("rpc", func(t *testing.T) {
		runConsensusTest(t, kit.ThroughRPC())
	})
}

type consensusSuite struct {
	opts []interface{}
}

// runConsensusTest is the entry point to consensus test suite
func runConsensusTest(t *testing.T, opts ...interface{}) {
	ts := consensusSuite{opts: opts}

	t.Run("testTSPoWMining", ts.testTSPoWMining)
}

func (ts *consensusSuite) testTSPoWMining(t *testing.T) {
	ctx := context.Background()

	full, miner, _ := kit.EnsembleMinimal(t, ts.opts...)

	newHeads, err := full.ChainNotify(ctx)
	require.NoError(t, err)

	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	bm := kit.NewBlockMiner(t, miner)
	bm.MineUntilBlock(ctx, full, nil)
	require.NoError(t, err)

	<-newHeads

	h2, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Greater(t, int64(h2.Height()), int64(h1.Height()))

	bm.MineUntilBlock(ctx, full, nil)
	require.NoError(t, err)

	<-newHeads

	h3, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Greater(t, int64(h3.Height()), int64(h2.Height()))
}
