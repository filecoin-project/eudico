package param

import (
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/build"
)

var GenesisWorkTarget = func() big.Int {
	w, _ := big.FromString(build.GenesisPoWTarget)
	return w
}()
