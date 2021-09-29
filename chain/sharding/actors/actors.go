package actor

import (
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/vm"
	exported5 "github.com/filecoin-project/specs-actors/v5/actors/builtin/exported"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

//go:generate go run ./gen

var (
	SplitActorCodeID cid.Cid
	ShardActorCodeID cid.Cid
)

var builtinActors map[cid.Cid]*actorInfo

type actorInfo struct {
	name string
}

func init() {
	builder := cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}
	builtinActors = make(map[cid.Cid]*actorInfo)

	for id, info := range map[*cid.Cid]*actorInfo{ //nolint:nomaprange
		&SplitActorCodeID: {name: "deleg/0/split"},
		&ShardActorCodeID: {name: "deleg/0/shards"},
	} {
		c, err := builder.Sum([]byte(info.name))
		if err != nil {
			panic(err)
		}
		*id = c
		builtinActors[c] = info
	}
}

func NewActorRegistry() *vm.ActorRegistry {
	inv := vm.NewActorRegistry()

	// TODO: drop unneeded
	inv.Register(vm.ActorsVersionPredicate(actors.Version5), exported5.BuiltinActors()...)
	inv.Register(nil, InitActor{}) // use our custom init actor

	inv.Register(nil, SplitActor{})
	inv.Register(nil, ShardActor{})

	return inv
}
