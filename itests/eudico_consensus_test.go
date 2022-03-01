//stm: #integration
package itests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/consensus/tspow"
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

	t.Run("testTSpoWConsensusMining", ts.testTSPoWMining)
}

func (ts *consensusSuite) testTSPoWMining(t *testing.T) {
	ctx := context.Background()

	full, _, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)
	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet keys list is empty")
	}

	go tspow.Mine(ctx, l[0], full)

	newHeads, err := full.ChainNotify(ctx)
	require.NoError(t, err)
	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	<-newHeads
}
