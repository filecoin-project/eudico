package utils

import (
	"strconv"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
)

func GenRandChildChecks(num int) []schema.ChildCheck {
	l := make([]schema.ChildCheck, 0)
	for i := 0; i < num; i++ {
		s := strconv.FormatInt(int64(i), 10)
		c, _ := schema.Linkproto.Sum([]byte(s))
		l = append(l,
			schema.ChildCheck{Source: s, Check: c})
	}
	return l
}
