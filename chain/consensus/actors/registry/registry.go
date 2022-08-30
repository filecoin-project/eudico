package registry

import (
	"github.com/filecoin-project/lotus/chain/vm"
)

func NewActorRegistry() *vm.ActorRegistry {
	inv := vm.NewActorRegistry()

	return inv
}
