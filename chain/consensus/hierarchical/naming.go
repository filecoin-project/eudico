package hierarchical

import (
	"path"

	address "github.com/filecoin-project/go-address"
	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

// Root is the ID of the root network
const RootSubnet = SubnetID("/root")

// Undef is the undef ID
const UndefID = SubnetID("")

// SubNetID represents the ID of a subnet
type SubnetID string

// Builder to generate subnet IDs from their name
var builder = cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}

// NewSubnetID generates the ID for a subnet from the networkName
// of its parent.
//
// It takes the parent name and adds the source address of the subnet
// actor that represents the subnet.
func NewSubnetID(parentName SubnetID, SubnetActorAddr address.Address) SubnetID {
	return SubnetID(path.Join(parentName.String(), SubnetActorAddr.String()))
}

// Cid for the subnetID
func (id SubnetID) Cid() (cid.Cid, error) {
	return builder.Sum([]byte(id))
}

// Parent returns the ID of the parent network.
func (id SubnetID) Parent() SubnetID {
	if id == RootSubnet {
		return UndefID
	}
	return SubnetID(path.Dir(string(id)))
}

// Actor returns the subnet actor for a subnet
//
// Returns the address of the actor that handles the logic for a subnet
// in its parent Subnet.
func (id SubnetID) Actor() (address.Address, error) {
	if id == RootSubnet {
		return address.Undef, nil
	}
	_, saddr := path.Split(string(id))
	return address.NewFromString(saddr)
}

// String returns the id in string form
func (id SubnetID) String() string {
	return string(id)
}
