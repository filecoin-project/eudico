package main

import (
	actor "github.com/filecoin-project/lotus/chain/sharding/actors"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "actor",
		actor.SplitState{},
		actor.ShardState{},
		actor.Shard{},
		actor.MinerState{},
		actor.AddParams{},
		actor.SelectParams{},
		actor.AddShardReturn{},
	); err != nil {
		panic(err)
	}
}
