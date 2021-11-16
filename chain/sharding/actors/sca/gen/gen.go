package main

import (
	actor "github.com/filecoin-project/lotus/chain/sharding/actors/sca"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "sca",
		actor.SCAState{},
		actor.Shard{},
		actor.FundParams{},
		actor.AddShardReturn{},
	); err != nil {
		panic(err)
	}
}
