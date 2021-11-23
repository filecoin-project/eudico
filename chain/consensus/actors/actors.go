package actor

import (
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

var (
	SplitActorCodeID       cid.Cid
	SubnetCoordActorCodeID cid.Cid
	SubnetActorCodeID      cid.Cid
)

var builtinActors map[cid.Cid]*actorInfo

type actorInfo struct {
	name string
}

func init() {
	builder := cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}
	builtinActors = make(map[cid.Cid]*actorInfo)

	for id, info := range map[*cid.Cid]*actorInfo{ //nolint:nomaprange
		&SplitActorCodeID:       {name: "example/0/split"},
		&SubnetCoordActorCodeID: {name: "hierarchical/0/sca"},
		&SubnetActorCodeID:      {name: "hierarchical/0/subnet"},
	} {
		c, err := builder.Sum([]byte(info.name))
		if err != nil {
			panic(err)
		}
		*id = c
		builtinActors[c] = info
	}
}
