package main

import (
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "subnet",
		actor.SubnetState{},
		actor.ConstructParams{},
		actor.CheckVotes{},
	); err != nil {
		panic(err)
	}
}
