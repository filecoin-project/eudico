package main

import (
	actor "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "replace",
		actor.ReplaceState{},
		actor.Owners{},
		actor.OwnParams{},
	); err != nil {
		panic(err)
	}
}
