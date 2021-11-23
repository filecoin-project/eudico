package sca

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

var (
	// MinSubnetStake required to register a new subnet
	// TODO: Kept in 1FIL for testing, change to the right
	// value once we decide it.
	// We could make this value configurable in construction.
	MinSubnetStake = abi.NewTokenAmount(1e18)
)

// Status describes in what state in its lifecycle a subnet is.
type Status uint64

const (
	Active   Status = iota // Active and operating. Has permission to interact with other chains in the hierarchy
	Inactive               // Waiting for the stake to be top-up over the MinStake threshold
	Killed                 // Not active anymore.

)

// SCAState represents the state of the Subnet Coordinator Actor
type SCAState struct {
	// CID of the current network
	Network cid.Cid
	// ID of the current network
	NetworkName hierarchical.SubnetID
	// Total subnets below this one.
	TotalSubnets uint64
	// Minimum stake to create a new subnet
	MinStake abi.TokenAmount
	// List of subnets
	Subnets cid.Cid // HAMT[cid.Cid]Subnet
}

type Subnet struct {
	Cid      cid.Cid               // Cid of the subnet ID
	ID       hierarchical.SubnetID // human-readable name of the subnet ID (path in the hierarchy)
	Parent   cid.Cid
	ParentID hierarchical.SubnetID
	Stake    abi.TokenAmount
	// The SCA doesn't keep track of the stake from miners, just locks the funds.
	// Is up to the subnet actor to handle this and distribute the stake
	// when the subnet is killed.
	// NOTE: We may want to keep track of this in the future.
	// Stake      cid.Cid // BalanceTable with locked stake.
	Funds  cid.Cid // BalanceTable with funds from addresses that entered the subnet.
	Status Status
}

func ConstructSCAState(store adt.Store, networkName hierarchical.SubnetID) (*SCAState, error) {
	emptySubnetsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	networkCid, err := networkName.Cid()
	if err != nil {
		panic(err)
	}
	return &SCAState{
		Network:      networkCid,
		NetworkName:  networkName,
		TotalSubnets: 0,
		MinStake:     MinSubnetStake,
		Subnets:      emptySubnetsMapCid,
	}, nil
}

// GetSubnet gets a subnet from the actor state.
func (st *SCAState) GetSubnet(s adt.Store, id cid.Cid) (*Subnet, bool, error) {
	claims, err := adt.AsMap(s, st.Subnets, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load subnets: %w", err)
	}
	return getSubnet(claims, id)
}

func getSubnet(subnets *adt.Map, id cid.Cid) (*Subnet, bool, error) {
	var out Subnet
	found, err := subnets.Get(abi.CidKey(id), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get subnet with id %v: %w", id, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

// Get subnet from its subnet actor address.
func (st *SCAState) getSubnetFromActorAddr(s adt.Store, addr address.Address) (*Subnet, bool, error) {
	shid := hierarchical.NewSubnetID(st.NetworkName, addr)
	shcid, err := shid.Cid()
	if err != nil {
		return nil, false, err
	}
	return st.GetSubnet(s, shcid)
}

func ListSubnets(s adt.Store, st SCAState) ([]Subnet, error) {
	subnetMap, err := adt.AsMap(s, st.Subnets, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, err
	}
	var sh Subnet
	out := []Subnet{}
	err = subnetMap.ForEach(&sh, func(k string) error {
		out = append(out, sh)
		return nil
	})
	return out, err
}
