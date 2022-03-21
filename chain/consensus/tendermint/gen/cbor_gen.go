package main

import (
	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "tendermint",
		tendermint.RegistrationMessageRequest{},
		tendermint.RegistrationMessageResponse{},
	); err != nil {
		panic(err)
	}
}
