package tspow

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
)

func TestWork(t *testing.T) {
	c, _ := cid.Parse("bafy2bzaced2sxo4udmu74agbisxncjichjhvbf4xnauzxuyosqkssmhe54cbk")

	bh := &types.BlockHeader{
		Miner:                 builtin.SystemActorAddr,
		Ticket:                nil,
		ElectionProof:         nil,
		BeaconEntries:         nil,
		WinPoStProof:          nil,
		Parents:               nil,
		ParentWeight:          big.Zero(),
		Height:                0,
		ParentStateRoot:       c,
		ParentMessageReceipts: c,
		Messages:              c,
		BLSAggregate:          nil,
		Timestamp:             0,
		BlockSig:              nil,
		ForkSignaling:         0,
		ParentBaseFee:         big.Zero(),
	}

	bestH := *bh
	fmt.Println(bh.Cid(), work(bh), 0)

	it := func(iter int) {
		for i := 0; i < iter; i++ {
			bh.ElectionProof = &types.ElectionProof{
				VRFProof: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			}
			rand.Read(bh.ElectionProof.VRFProof)
			if work(&bestH).LessThan(work(bh)) {
				bestH = *bh
			}
		}

		fmt.Println(bestH.Cid(), work(&bestH), iter)
	}

	it(60)
	it(600)
	it(60000)
	it(600000)
}
