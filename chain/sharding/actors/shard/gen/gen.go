package main

import (
	actor "github.com/filecoin-project/lotus/chain/sharding/actors/shard"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "shard",
		actor.ShardState{},
		actor.ConstructParams{},
	); err != nil {
		panic(err)
	}
}
