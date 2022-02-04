package main

import (
	actor "github.com/filecoin-project/lotus/chain/consensus/actors/reward"

	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "reward",
		// actor.ThisEpochRewardReturn{},
		actor.FundingParams{},
		actor.State{},
	); err != nil {
		panic(err)
	}
}
