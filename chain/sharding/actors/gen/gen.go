package main

import (
	"github.com/filecoin-project/lotus/chain/sharding/actor"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "actors",
		actor.SplitState{},
		actor.ShardState{},
	); err != nil {
		panic(err)
	}
}
