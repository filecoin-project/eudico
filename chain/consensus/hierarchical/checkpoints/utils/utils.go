package utils

import (
	"strconv"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
)

func GenRandChecks(num int) []*schema.Checkpoint {
	l := make([]*schema.Checkpoint, 0)
	for i := 0; i < num; i++ {
		s := strconv.FormatInt(int64(i), 10)
		c, _ := schema.Linkproto.Sum([]byte(s))
		ch := schema.NewRawCheckpoint(address.SubnetID(s), abi.ChainEpoch(i))
		ch.SetPrevious(c)
		l = append(l, ch)

	}
	return l
}
