package registry

import (
	exported7 "github.com/filecoin-project/specs-actors/v7/actors/builtin/exported"

	"github.com/filecoin-project/lotus/chain/actors"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/actors/split"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/vm"
)

func NewActorRegistry() *vm.ActorRegistry {
	inv := vm.NewActorRegistry()

	// TODO: drop unneeded
	inv.Register(vm.ActorsVersionPredicate(actors.Version7), exported7.BuiltinActors()...)
	inv.Register(nil, initactor.InitActor{}) // use our custom init actor

	// Hierarchical consensus
	inv.Register(nil, reward.Actor{})
	inv.Register(nil, subnet.SubnetActor{})
	inv.Register(nil, sca.SubnetCoordActor{})

	// Custom actors
	inv.Register(nil, split.SplitActor{})
	inv.Register(nil, replace.ReplaceActor{})

	return inv
}
