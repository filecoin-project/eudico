package registry

import (
	"github.com/filecoin-project/lotus/chain/actors"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/lotus/chain/consensus/actors/split"
	"github.com/filecoin-project/lotus/chain/vm"
	exported6 "github.com/filecoin-project/specs-actors/v6/actors/builtin/exported"
)

func NewActorRegistry() *vm.ActorRegistry {
	inv := vm.NewActorRegistry()

	// TODO: drop unneeded
	inv.Register(vm.ActorsVersionPredicate(actors.Version6), exported6.BuiltinActors()...)
	inv.Register(nil, initactor.InitActor{}) // use our custom init actor

	inv.Register(nil, split.SplitActor{})
	inv.Register(nil, shard.ShardActor{})

	return inv
}
