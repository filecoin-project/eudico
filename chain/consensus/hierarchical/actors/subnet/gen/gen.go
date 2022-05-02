package main

import (
	gen "github.com/whyrusleeping/cbor-gen"

	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "subnet",
		actor.SubnetState{},
		actor.ConstructParams{},
		actor.CheckVotes{},
		actor.Validator{},
	); err != nil {
		panic(err)
	}
}
