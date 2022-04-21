package actor

import (
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

var (
	SubnetCoordActorCodeID cid.Cid
	SubnetActorCodeID      cid.Cid

	MpowerActorCodeID      cid.Cid

	RewardActorCodeID      cid.Cid

	SplitActorCodeID   cid.Cid
	ReplaceActorCodeID cid.Cid

)

var builtinActors map[cid.Cid]*actorInfo

type actorInfo struct {
	name string
}

func init() {
	builder := cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}
	builtinActors = make(map[cid.Cid]*actorInfo)

	for id, info := range map[*cid.Cid]*actorInfo{ //nolint:nomaprange
		&SubnetCoordActorCodeID: {name: "hierarchical/0/sca"},
		&SubnetActorCodeID:      {name: "hierarchical/0/subnet"},

		&MpowerActorCodeID:      {name: "deleg/0/mpower"},

		&RewardActorCodeID:      {name: "hierarchical/0/reward"},

		&SplitActorCodeID:   {name: "example/0/split"},
		&ReplaceActorCodeID: {name: "example/0/replace"},

	} {
		c, err := builder.Sum([]byte(info.name))
		if err != nil {
			panic(err)
		}
		*id = c
		builtinActors[c] = info
	}
}
