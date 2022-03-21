//stm: #integration
package itests

import (
	"context"
	"github.com/filecoin-project/go-state-types/big"
	mbig "math/big"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"testing"
	"time"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/require"
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

	var st sca.SCAState
	bs := blockstore.NewAPIBlockstore(full)
	cst := cbor.NewCborStore(bs)
	s := adt.WrapStore(ctx, cst)
	act, err := full.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	require.NoError(t, err)

	/*
		subnets, err := sca.ListSubnets(s, st)
		require.NoError(t, err)
		require.Equal(t, len(subnets), 1)

	*/

	err = cst.Get(ctx, act.Head, &st)
	require.NoError(t, err)

	addr, err := full.WalletDefaultAddress(ctx)
	require.NoError(t, err)

	parent := address.RootSubnet
	name := "testSubnet"
	consensus := hierarchical.Tendermint
	minerStake := abi.NewStoragePower(1e8) // TODO: Make this value configurable in a flag/argument
	checkperiod := abi.ChainEpoch(10)
	delegminer := hierarchical.SubnetCoordActorAddr

	wait := true
	for wait {
		balance, err := full.WalletBalance(ctx, addr)
		require.NoError(t, err)
		t.Log("Balance:", balance)
		time.Sleep(time.Second * 3)
		a := mbig.NewInt(2)
		if balance.Cmp(a) == 1 {
			wait = false
		}

	}

	actorAddr, err := full.AddSubnet(ctx, addr, parent, name, uint64(consensus), minerStake, checkperiod, delegminer)
	require.NoError(t, err)

	_ = actorAddr

	subnets, err := sca.ListSubnets(s, st)
	require.NoError(t, err)
	require.Equal(t, 1, len(subnets))

	subnet := subnets[0]

	val, err := types.ParseFIL("10")
	require.NoError(t, err)

	walletID, err := full.StateLookupID(ctx, addr, types.EmptyTSK)
	require.NoError(t, err)

	c, err := full.JoinSubnet(ctx, addr, big.Int(val), subnet.ID)
	require.NoError(t, err)
	_ = c

	err = full.MineSubnet(ctx, walletID, subnet.ID, false)
	require.NoError(t, err)

	newHeads, err := full.ChainNotify(ctx)
	require.NoError(t, err)

}
