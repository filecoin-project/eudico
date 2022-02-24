//stm: #integration
package itests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/types"
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

	t.Run("testDelegatedConsensusMining", ts.testDelegatedMining)
}

func (ts *consensusSuite) testDelegatedMining(t *testing.T) {
	ctx := context.Background()

	full, _, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)
	go delegcns.Mine(ctx, full)

	newHeads, err := full.ChainNotify(ctx)
	require.NoError(t, err)
	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	<-newHeads

	h2, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Greater(t, int64(h2.Height()), int64(h1.Height()))

}

func mineUntilBlock(ctx context.Context, addr addr.Address, full *kit.TestFullNode) (*types.TipSet, error) {
	b, err := full.ChainHead(ctx)
	if err != nil {
		return nil, err
	}
	h := b.Height()
	mctx, cancel := context.WithCancel(ctx)
	go delegcns.Mine(mctx, full)

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		default:
			b, err := full.ChainHead(ctx)
			if err != nil {
				cancel()
				return nil, err
			}
			hn := b.Height()
			if h < hn {
				cancel()
				return b, nil
			}
		}
	}
}
