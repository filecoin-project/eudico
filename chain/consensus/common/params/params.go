package param

import "github.com/filecoin-project/go-state-types/big"

var GenesisWorkTarget = func() big.Int {
	w, _ := big.FromString("4519783675352289407433363")
	return w
}()
