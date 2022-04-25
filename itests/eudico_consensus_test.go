// stm: #integration
package itests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/ideal"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/itests/kit"
	mapi "github.com/filecoin-project/mir"
)

func TestEudicoConsensus(t *testing.T) {
	t.Run("ideal", func(t *testing.T) {
		runIdealConsensusTests(t, kit.ThroughRPC(), kit.RootIdeal())
	})

	t.Run("mir", func(t *testing.T) {
		runMirConsensusTests(t, kit.ThroughRPC(), kit.RootMir())
	})

	t.Run("tspow", func(t *testing.T) {
		runTSPoWConsensusTests(t, kit.ThroughRPC(), kit.RootTSPoW())
	})

	t.Run("delegated", func(t *testing.T) {
		runDelegatedConsensusTests(t, kit.ThroughRPC(), kit.RootDelegated())
	})

	t.Run("filcns", func(t *testing.T) {
		runFilcnsConsensusTests(t, kit.ThroughRPC(), kit.RootFilcns())
	})

	if os.Getenv("TENDERMINT_ITESTS") != "" {
		t.Run("tendermint", func(t *testing.T) {
			runTendermintConsensusTests(t, kit.ThroughRPC(), kit.RootTendermint())
		})
	}
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

func runIdealConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testIdealMining", ts.testIdealMining)
}

func (ts *eudicoConsensusSuite) testIdealMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, _ := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	go func() {
		err = ideal.Mine(ctx, l[0], full)
		require.NoError(t, err)
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 10, address.RootSubnet, full)
	require.NoError(t, err)
}

func runMirConsensusTests(t *testing.T, opts ...interface{}) {
	if err := os.Setenv("EUDICO_MIR_ID", "0"); err != nil {
		require.NoError(t, err)
	}
	if err := os.Setenv("EUDICO_MIR_NODES", "1"); err != nil {
		require.NoError(t, err)
	}
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testMirMining", ts.testMirMining)
}

func (ts *eudicoConsensusSuite) testMirMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, _ := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	go func() {
		err = mir.Mine(ctx, l[0], full)
		if xerrors.Is(mapi.ErrStopped, err) {
			return
		}
		require.NoError(t, err)
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 10, address.RootSubnet, full)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)
}

func runTSPoWConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testTSpoWMining", ts.testTSPoWMining)
}

func (ts *eudicoConsensusSuite) testTSPoWMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, _ := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	go func() {
		err = tspow.Mine(ctx, l[0], full)
		require.NoError(t, err)
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 3, address.RootSubnet, full)
	require.NoError(t, err)
}

func runDelegatedConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}
	t.Run("testDelegatedMining", ts.testDelegatedMining)
}

func (ts *eudicoConsensusSuite) testDelegatedMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, _ := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

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

	go func() {
		err = delegcns.Mine(ctx, address.Undef, full)
		require.NoError(t, err)
	}()

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

func runTendermintConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}
	t.Run("testTendermintMining", ts.testTendermintMining)
}

func (ts *eudicoConsensusSuite) testTendermintMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, ens := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)
	var err error
	defer func() {
		err = ens.Stop()
		require.NoError(t, err)
	}()

	ki, err := tendermint.GetSecp256k1TendermintKey(kit.TendermintConsensusKeyFile)
	require.NoError(t, err)
	k, err := wallet.NewKey(*ki)
	require.NoError(t, err)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	go func() {
		err = tendermint.Mine(ctx, l[0], full)
		require.NoError(t, err)
	}()

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

func runFilcnsConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}
	t.Run("testFilcnsMining", ts.testFilcnsMining)
}

func (ts *eudicoConsensusSuite) testFilcnsMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, miner, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)

	bm := kit.NewBlockMiner(t, miner)
	bm.MineBlocks(ctx, 1*time.Second)

	err := kit.SubnetPerformHeightCheckForBlocks(ctx, 3, address.RootSubnet, full)
	require.NoError(t, err)
}
