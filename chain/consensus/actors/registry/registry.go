package registry

import (
	exported8 "github.com/filecoin-project/specs-actors/v7/actors/builtin/exported"

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
	inv.Register(actors.Version8, vm.ActorsVersionPredicate(actors.Version8), exported8.BuiltinActors()...)
	inv.Register(actors.Version8, nil, initactor.InitActor{}) // use our custom init actor

	// Hierarchical consensus
	inv.Register(actors.Version8, nil, reward.Actor{})
	inv.Register(actors.Version8, nil, subnet.SubnetActor{})
	inv.Register(actors.Version8, nil, sca.SubnetCoordActor{})

	// Custom actors
	inv.Register(actors.Version8, nil, split.SplitActor{})
	inv.Register(actors.Version8, nil, replace.ReplaceActor{})

	return inv
}
