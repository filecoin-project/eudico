package main

import (
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "atomic",
		atomic.MergeParams{},
		atomic.UnlockParams{},
		atomic.LockParams{},
		atomic.LockedOutput{},
		atomic.LockedState{},
	); err != nil {
		panic(err)
	}
}
