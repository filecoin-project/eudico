package main

import (
	"github.com/filecoin-project/lotus/chain/consensus/common"
	gen "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "tendermint",
		common.RegistrationMessageRequest{},
		common.RegistrationMessageResponse{},
	); err != nil {
		panic(err)
	}
}
