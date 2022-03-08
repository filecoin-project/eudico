//stm: #integration
package itests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestEudicoConsensus(t *testing.T) {
	t.Run("tspow", func(t *testing.T) {
		runTSPoWConsensusTests(t, kit.ThroughRPC(), kit.TSPoW())
	})

	t.Run("delegated", func(t *testing.T) {
		runDelegatedConsensusTests(t, kit.ThroughRPC(), kit.Delegated())
	})
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

func runTSPoWConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testTSpoWConsensusMining", ts.testTSPoWMining)
}

func (ts *eudicoConsensusSuite) testTSPoWMining(t *testing.T) {
	ctx := context.Background()

	full, _, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
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

	h2, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Greater(t, int64(h2.Height()), int64(h1.Height()))

	<-newHeads

	h3, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Greater(t, int64(h3.Height()), int64(h2.Height()))
}

func runDelegatedConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testDelegatedConsensusMining", ts.testDelegatedMining)
}

func (ts *eudicoConsensusSuite) testDelegatedMining(t *testing.T) {
	ctx := context.Background()

	full, _, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)
	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	var ki types.KeyInfo
	err = kit.ReadKeyInfoFromFile(kit.DelegatedConsensusKeyFile, &ki)
	require.NoError(t, err)

	k, err := wallet.NewKey(ki)
	require.NoError(t, err)

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
	require.Equal(t, h2.Blocks()[0].Miner, k.Address)

	<-newHeads

	h3, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Greater(t, int64(h3.Height()), int64(h2.Height()))
	require.Equal(t, h3.Blocks()[0].Miner, k.Address)
}
