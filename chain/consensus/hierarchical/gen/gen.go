package main

import (
	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "hierarchical",
		hierarchical.ConsensusParams{},
		hierarchical.Validator{},
		hierarchical.ValidatorSet{},
	); err != nil {
		panic(err)
	}
}
