package main

import (
	actor "github.com/filecoin-project/lotus/chain/consensus/actors/split"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "split",
		actor.SplitState{},
	); err != nil {
		panic(err)
	}
}
