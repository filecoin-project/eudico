package sca

import (
	"path"

	address "github.com/filecoin-project/go-address"
	cid "github.com/ipfs/go-cid"
)

// ShardCid returns the cid from the human-readable shard name.
func ShardCid(name string) (cid.Cid, error) {
	return builder.Sum([]byte(name))
}

// genShardID generates the ID for a shard from the networkName
// of its parent.
//
// It takes the parent name and adds the source address of the shard
// actor that represents the shard.
func genShardID(parentName string, shardActorAddr address.Address) string {
	return path.Join(parentName, shardActorAddr.String())
}
