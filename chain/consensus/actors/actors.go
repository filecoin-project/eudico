package actor

import (
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
		// TODO: Find better names.
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
