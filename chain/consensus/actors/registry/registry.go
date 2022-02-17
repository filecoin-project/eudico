package registry

import (
	"github.com/filecoin-project/lotus/chain/actors"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/actors/split"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/vm"
	exported7 "github.com/filecoin-project/specs-actors/v7/actors/builtin/exported"
)

func NewActorRegistry() *vm.ActorRegistry {
	inv := vm.NewActorRegistry()

	// TODO: drop unneeded
	inv.Register(vm.ActorsVersionPredicate(actors.Version7), exported7.BuiltinActors()...)
	inv.Register(nil, initactor.InitActor{}) // use our custom init actor

	inv.Register(nil, reward.Actor{})
	inv.Register(nil, split.SplitActor{})
	inv.Register(nil, subnet.SubnetActor{})
	inv.Register(nil, sca.SubnetCoordActor{})

	return inv
}
