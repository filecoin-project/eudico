package main

import (
	mpower "github.com/filecoin-project/lotus/chain/consensus/actors/mpower"
	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "mpower",
		mpower.State{},
		mpower.AddMinerParams{},
		mpower.NewTaprootAddressParam{},
	); err != nil {
		panic(err)
	}
}
