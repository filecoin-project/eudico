package main

import (
	actor "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "sca",
		actor.ConstructorParams{},
		actor.CheckpointParams{},
		actor.SCAState{},
		actor.Subnet{},
		actor.FundParams{},
		actor.SubnetIDParam{},
		actor.CrossMsgs{},
		actor.MetaTag{},
		actor.CrossMsgParams{},
		actor.ErrorParam{},
	); err != nil {
		panic(err)
	}
}
