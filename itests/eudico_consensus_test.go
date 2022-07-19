// stm: #integration
package itests

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/dummy"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/itests/kit"
	mapi "github.com/filecoin-project/mir"
)

func TestEudicoConsensus(t *testing.T) {
	t.Run("dummy", func(t *testing.T) {
		runDummyConsensusTests(t, kit.ThroughRPC(), kit.RootDummy())
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

func runDummyConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testDummyMining", ts.testDummyMining)
}

func (ts *eudicoConsensusSuite) testDummyMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, _ := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	wg.Add(1)
	go func() {
		t.Log("[*] miner started")
		defer func() {
			t.Log("[*] miner stopped")
			wg.Done()
		}()
		err := dummy.Mine(ctx, l[0], full)
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 5, address.RootSubnet, full)
	require.NoError(t, err)
}

func runMirConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	// t.Run("testMirMining", ts.testMirMining)
	t.Run("testMirLibp2pMining", ts.testMirLibp2pMining)
	t.Run("testMirTwoNodes", ts.testMirTwoNodes)
}

func (ts *eudicoConsensusSuite) testMirTwoNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	one, two, ens := kit.EudicoEnsembleTwoNodes(t, ts.opts...)
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
		err := ens.Stop()
		require.NoError(t, err)
	}()

	// Fail if genesis blocks are different
	gen1, err := one.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := two.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gen1.String(), gen2.String())

	// Fail if no peers
	p, err := one.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node one has peers")

	p, err = two.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node two has peers")

	ens.Connect(one, two)

	peers, err := one.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node one doesn't have a peer")

	peers, err = two.NetPeers(ctx)
	require.NoError(t, err)
	require.Lenf(t, peers, 1, "node two doesn't have a peer")

	l1, err := one.WalletList(ctx)
	require.NoError(t, err)
	if len(l1) != 1 {
		t.Fatal("one's wallet key list is empty")
	}
	oneAddr := l1[0]

	l2, err := two.WalletList(ctx)
	require.NoError(t, err)
	if len(l2) != 1 {
		t.Fatal("two's wallet key list is empty")
	}
	twoAddr := l2[0]

	msg1 := &types.Message{
		From:  oneAddr,
		To:    twoAddr,
		Value: big.Zero(),
	}

	msg2 := &types.Message{
		From:  twoAddr,
		To:    oneAddr,
		Value: big.Zero(),
	}

	mirNodeOneAddr, err := kit.NodeLibp2pAddr(one)
	require.NoError(t, err)
	mirNodeTwoAddr, err := kit.NodeLibp2pAddr(two)
	require.NoError(t, err)

	mirNodeOne := fmt.Sprintf("%s:%s", address.RootSubnet, l1[0].String())
	mirNodeTwo := fmt.Sprintf("%s:%s", address.RootSubnet, l2[0].String())
	env := fmt.Sprintf("%s@%s,%s@%s",
		mirNodeOne, mirNodeOneAddr,
		mirNodeTwo, mirNodeTwoAddr,
	)
	err = os.Setenv(mir.ValidatorsEnv, env)
	require.NoError(t, err)
	defer os.Unsetenv(mir.ValidatorsEnv) // nolint

	wg.Add(2)
	go func() {
		t.Log("[*] node one started")
		defer func() {
			t.Log("[*] node one stopped")
			wg.Done()
		}()
		err := mir.Mine(ctx, oneAddr, one)
		if xerrors.Is(mapi.ErrStopped, err) {
			return
		}
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	go func() {
		t.Log("[*] node two started")
		defer func() {
			t.Log("[*] node two stopped")
			wg.Done()
		}()
		err := mir.Mine(ctx, twoAddr, two)
		if xerrors.Is(mapi.ErrStopped, err) {
			return
		}
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	err = kit.WaitForBalance(ctx, oneAddr, 4, one)
	require.NoError(t, err)

	err = kit.WaitForBalance(ctx, twoAddr, 4, two)
	require.NoError(t, err)

	// Send first message

	smsg1, err := one.MpoolPushMessage(ctx, msg1, nil)
	require.NoError(t, err)

	res, err := one.StateWaitMsg(ctx, smsg1.Cid(), 1, lapi.LookbackNoLimit, true)
	require.NoError(t, err)

	require.Equal(t, exitcode.Ok, res.Receipt.ExitCode, "msg1 not successful")

	res, err = two.StateWaitMsg(ctx, smsg1.Cid(), 1, lapi.LookbackNoLimit, true)
	require.NoError(t, err)

	require.Equal(t, exitcode.Ok, res.Receipt.ExitCode, "msg1 not successful")

	// Send second message

	smsg2, err := two.MpoolPushMessage(ctx, msg2, nil)
	require.NoError(t, err)

	res, err = two.StateWaitMsg(ctx, smsg2.Cid(), 1, lapi.LookbackNoLimit, true)
	require.NoError(t, err)

	require.Equal(t, exitcode.Ok, res.Receipt.ExitCode, "msg2 not successful")

	res, err = one.StateWaitMsg(ctx, smsg2.Cid(), 1, lapi.LookbackNoLimit, true)
	require.NoError(t, err)

	require.Equal(t, exitcode.Ok, res.Receipt.ExitCode, "msg2 not successful")
}

func (ts *eudicoConsensusSuite) testMirLibp2pMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, ens := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)
	defer func() {
		err := ens.Stop()
		require.NoError(t, err)
	}()

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	libp2pPrivKeyBytes, err := full.PrivKey(ctx)
	require.NoError(t, err)

	mirNodeID := fmt.Sprintf("%s:%s", address.RootSubnet, l[0].String())

	addr, err := kit.GetLibp2pAddr(libp2pPrivKeyBytes)
	require.NoError(t, err)

	err = os.Setenv(mir.ValidatorsEnv, fmt.Sprintf("%s@%s", mirNodeID, addr))
	require.NoError(t, err)
	defer os.Unsetenv(mir.ValidatorsEnv) // nolint

	wg.Add(1)
	go func() {
		t.Log("[*] miner started")
		defer func() {
			t.Log("[*] miner stopped")
			wg.Done()
		}()
		err := mir.Mine(ctx, l[0], full)
		if xerrors.Is(mapi.ErrStopped, err) {
			return
		}
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 10, address.RootSubnet, full)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)
}

func runTSPoWConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testTSPoWMining", ts.testTSPoWMining)
}

func (ts *eudicoConsensusSuite) testTSPoWMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, _ := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	wg.Add(1)
	go func() {
		t.Log("[*] miner started")
		defer func() {
			t.Log("[*] miner stopped")
			wg.Done()
		}()
		err := tspow.Mine(ctx, l[0], full)
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	err = kit.SubnetPerformHeightCheckForBlocks(ctx, 3, address.RootSubnet, full)
	require.NoError(t, err)
}

func runDelegatedConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}
	t.Run("testDelegatedMining", ts.testDelegatedMining)
}

func (ts *eudicoConsensusSuite) testDelegatedMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

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

	wg.Add(1)
	go func() {
		t.Log("[*] miner started")
		defer func() {
			t.Log("[*] miner stopped")
			wg.Done()
		}()
		err := delegcns.Mine(ctx, address.Undef, full)
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	newHeads, err := full.ChainNotify(ctx)
	require.NoError(t, err)
	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	h2 := <-newHeads
	require.Equal(t, 1, len(h2))
	require.Greater(t, int64(h2[0].Val.Height()), int64(h1.Height()))
	require.Equal(t, h2[0].Val.Blocks()[0].Miner, k.Address)

	h3 := <-newHeads
	require.Equal(t, 1, len(h3))
	require.Greater(t, int64(h3[0].Val.Height()), int64(h2[0].Val.Height()))
	require.Equal(t, h3[0].Val.Blocks()[0].Miner, k.Address)

	h4 := <-newHeads
	require.Equal(t, 1, len(h4))
	require.Greater(t, int64(h4[0].Val.Height()), int64(h3[0].Val.Height()))
	require.Equal(t, h4[0].Val.Blocks()[0].Miner, k.Address)
}

func runTendermintConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}
	t.Run("testTendermintMining", ts.testTendermintMining)
}

func (ts *eudicoConsensusSuite) testTendermintMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, ens := kit.EudicoEnsembleFullNodeOnly(t, ts.opts...)
	var err error
	defer func() {
		t.Log("[*] stopping test ensemble")
		defer t.Log("[*] ensemble stopped")
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

	wg.Add(1)
	go func() {
		t.Log("[*] miner started")
		defer func() {
			t.Log("[*] miner stopped")
			wg.Done()
		}()
		err := tendermint.Mine(ctx, l[0], full)
		if err != nil {
			t.Error(err)
			cancel()
			return
		}
	}()

	newHeads, err := full.ChainNotify(ctx)
	require.NoError(t, err)
	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.ChainHead(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	h2 := <-newHeads
	require.Equal(t, 1, len(h2))
	require.Greater(t, int64(h2[0].Val.Height()), int64(h1.Height()))
	require.Equal(t, h2[0].Val.Blocks()[0].Miner, k.Address)

	h3 := <-newHeads
	require.Equal(t, 1, len(h3))
	require.Greater(t, int64(h3[0].Val.Height()), int64(h2[0].Val.Height()))
	require.Equal(t, h3[0].Val.Blocks()[0].Miner, k.Address)

	h4 := <-newHeads
	require.Equal(t, 1, len(h4))
	require.Greater(t, int64(h4[0].Val.Height()), int64(h3[0].Val.Height()))
	require.Equal(t, h4[0].Val.Blocks()[0].Miner, k.Address)

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

	err := kit.SubnetPerformHeightCheckForBlocks(ctx, 5, address.RootSubnet, full)
	require.NoError(t, err)
}
