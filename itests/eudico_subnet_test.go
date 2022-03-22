//stm: #integration
package itests

import (
	"context"
	"testing"
	"time"

	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestEudicoSubnetConsensus(t *testing.T) {
	t.Run("subnet", func(t *testing.T) {
		runSubnetConsensusTests(t, kit.ThroughRPC(), kit.TSPoW())
	})
}

type eudicoSubnetConsensusSuite struct {
	opts []interface{}
}

func runSubnetConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoSubnetConsensusSuite{opts: opts}

	t.Run("testBasicInitialization", ts.testBasicInitialization)
}

func (ts *eudicoSubnetConsensusSuite) testBasicInitialization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, _, _ := kit.EudicoEnsembleMinimal(t, ts.opts...)

	l, err := full.WalletList(ctx)
	require.NoError(t, err)
	if len(l) != 1 {
		t.Fatal("wallet key list is empty")
	}

	go tspow.Mine(ctx, l[0], full)

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)

	parent := address.RootSubnet
	subnetName := "testSubnet"
	consensus := hierarchical.PoW
	minerStake := abi.NewStoragePower(1e8) // TODO: Make this value configurable in a flag/argument
	checkperiod := abi.ChainEpoch(10)
	delegminer := hierarchical.SubnetCoordActorAddr

	wait := true
	for wait {
		balance, err := full.WalletBalance(ctx, addr)
		require.NoError(t, err)
		t.Log("\t>>>>> Balance:", balance)
		time.Sleep(time.Second * 3)
		m := types.FromFil(3)
		if big.Cmp(balance, m) == 1 {
			wait = false
		}

	}

	actorAddr, err := full.AddSubnet(ctx, addr, parent, subnetName, uint64(consensus), minerStake, checkperiod, delegminer)
	require.NoError(t, err)

	subnetAddr := address.NewSubnetID(parent, actorAddr)

	t.Log("\t>>>>> Subnet addr:", subnetAddr)

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	_, err = full.StateLookupID(ctx, addr, types.EmptyTSK)
	require.NoError(t, err)

	_, err = full.JoinSubnet(ctx, addr, big.Int(val), subnetAddr)
	require.NoError(t, err)

	// AddSubnet only deploys the subnet actor. The subnet will only be listed after joining the subnet.
	var st sca.SCAState
	act, err := full.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	require.NoError(t, err)
	bs := blockstore.NewAPIBlockstore(full)
	cst := cbor.NewCborStore(bs)
	s := adt.WrapStore(ctx, cst)
	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)
	sn, err := sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Status)

	go full.MineSubnet(ctx, addr, subnetAddr, false)

	newHeads, err := full.SubnetChainNotify(ctx, subnetAddr)
	require.NoError(t, err)
	initHead := (<-newHeads)[0]
	baseHeight := initHead.Val.Height()

	h1, err := full.SubnetChainHead(ctx, subnetAddr)
	require.NoError(t, err)
	require.Equal(t, int64(h1.Height()), int64(baseHeight))

	<-newHeads

	h2, err := full.SubnetChainHead(ctx, subnetAddr)
	require.NoError(t, err)
	require.Greater(t, int64(h2.Height()), int64(h1.Height()))

	<-newHeads

	h3, err := full.SubnetChainHead(ctx, subnetAddr)
	require.NoError(t, err)
	require.Greater(t, int64(h3.Height()), int64(h2.Height()))

	_, err = full.LeaveSubnet(ctx, addr, subnetAddr)
	require.NoError(t, err)

	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)
	sn, err = sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(sn))
	require.NotEqual(t, 0, sn[0].Status)
}
