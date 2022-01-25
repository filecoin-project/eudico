package main

import (
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "schema",
		schema.ChildCheck{},
		schema.CrossMsgMeta{},
		schema.CheckData{},
		schema.Checkpoint{},
	); err != nil {
		panic(err)
	}
}
