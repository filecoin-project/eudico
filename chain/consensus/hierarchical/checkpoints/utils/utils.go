package utils

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
)

func GenRandChecks(num int) []*schema.Checkpoint {
	l := make([]*schema.Checkpoint, 0)
	for i := 0; i < num; i++ {
		a, _ := address.NewIDAddress(uint64(i))
		s := address.NewSubnetID(address.RootSubnet, a)
		c, _ := schema.Linkproto.Sum([]byte(s.String()))
		ch := schema.NewRawCheckpoint(s, abi.ChainEpoch(i))
		ch.SetPrevious(c)
		l = append(l, ch)

	}
	return l
}
