package naming

import (
	"path"

	address "github.com/filecoin-project/go-address"
	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

// Root is the ID of the root network
const Root = SubnetID("/root")

// Undef is the undef ID
const Undef = SubnetID("")

// SubNetID represents the ID of a subnet
type SubnetID string

// Builder to generate shard IDs from their name
var builder = cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}

// NewShardID generates the ID for a shard from the networkName
// of its parent.
//
// It takes the parent name and adds the source address of the shard
// actor that represents the shard.
func NewSubnetID(parentName SubnetID, shardActorAddr address.Address) SubnetID {
	return SubnetID(path.Join(parentName.String(), shardActorAddr.String()))
}

// Cid for the shardID
func (id SubnetID) Cid() (cid.Cid, error) {
	return builder.Sum([]byte(id))
}

// Parent returns the ID of the parent network.
func (id SubnetID) Parent() SubnetID {
	if id == Root {
		return Undef
	}
	return SubnetID(path.Dir(string(id)))
}

// Actor returns the shard actor for a shard
//
// Returns the address of the actor that handles the logic for a shard
// in its parent Subnet.
func (id SubnetID) Actor() (address.Address, error) {
	if id == Root {
		return address.Undef, nil
	}
	_, saddr := path.Split(string(id))
	return address.NewFromString(saddr)
}

// String returns the id in string form
func (id SubnetID) String() string {
	return string(id)
}
