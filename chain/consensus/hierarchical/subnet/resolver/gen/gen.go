package main

import (
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "resolver",
		resolver.ResolveMsg{},
	); err != nil {
		panic(err)
	}
}
