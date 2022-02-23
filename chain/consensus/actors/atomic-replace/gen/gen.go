package main

import (
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "replace",
		replace.ReplaceState{},
		replace.Owners{},
		replace.ReplaceParams{},
		replace.OwnParams{},
	); err != nil {
		panic(err)
	}
}
