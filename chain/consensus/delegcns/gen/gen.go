package main

import (
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "delegcns",
		delegcns.SplitState{},
	); err != nil {
		panic(err)
	}
}
