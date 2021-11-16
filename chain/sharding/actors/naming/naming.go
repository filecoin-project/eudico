package naming

import (
	"path"

	address "github.com/filecoin-project/go-address"
	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

// Builder to generate shard IDs from their name
var builder = cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}

// ShardCid returns the cid from the human-readable shard name.
func ShardCid(name string) (cid.Cid, error) {
	return builder.Sum([]byte(name))
}

// GenShardID generates the ID for a shard from the networkName
// of its parent.
//
// It takes the parent name and adds the source address of the shard
// actor that represents the shard.
func GenShardID(parentName string, shardActorAddr address.Address) string {
	return path.Join(parentName, shardActorAddr.String())
}
