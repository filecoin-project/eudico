package sca

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

const (
	// DefualtCheckpointPeriod defines 1000 epochs
	// as the default checkpoint period for a subnet.
	DefaultCheckpointPeriod = abi.ChainEpoch(1000)

	// MinCheckpointPeriod allowed for subnets
	MinCheckpointPeriod = abi.ChainEpoch(10)
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

	// Checkpoint period in number of epochs
	CheckPeriod abi.ChainEpoch
	// Checkpoints committed in SCA
	Checkpoints cid.Cid // HAMT[epoch]Checkpoint
	// PrevCheckMap map with previous checkpoints for childs
	PrevCheckMap cid.Cid // HAMT[subnetID]cid
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

func ConstructSCAState(store adt.Store, params ConstructorParams) (*SCAState, error) {
	emptySubnetsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	emptyCheckpointsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	emptyPrevMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	nn := hierarchical.SubnetID(params.NetworkName)
	networkCid, err := nn.Cid()
	if err != nil {
		panic(err)
	}
	// Don't allow really small checkpoint periods for now.
	period := abi.ChainEpoch(params.CheckpointPeriod)
	if period < MinCheckpointPeriod {
		period = DefaultCheckpointPeriod
	}

	return &SCAState{
		Network:      networkCid,
		NetworkName:  nn,
		TotalSubnets: 0,
		MinStake:     MinSubnetStake,
		Subnets:      emptySubnetsMapCid,
		CheckPeriod:  period,
		Checkpoints:  emptyCheckpointsMapCid,
		PrevCheckMap: emptyPrevMapCid,
	}, nil
}

// GetSubnet gets a subnet from the actor state.
func (st *SCAState) GetSubnet(s adt.Store, id cid.Cid) (*Subnet, bool, error) {
	subnets, err := adt.AsMap(s, st.Subnets, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load subnets: %w", err)
	}
	return getSubnet(subnets, id)
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

// GetCheckpoint gets a checkpoint from its index
func (st *SCAState) GetCheckpoint(s adt.Store, epoch abi.ChainEpoch) (*schema.Checkpoint, bool, error) {
	checkpoints, err := adt.AsMap(s, st.Checkpoints, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load subnets: %w", err)
	}
	return getCheckpoint(checkpoints, epoch)
}

func getCheckpoint(subnets *adt.Map, epoch abi.ChainEpoch) (*schema.Checkpoint, bool, error) {
	var out schema.Checkpoint
	found, err := subnets.Get(abi.UIntKey(uint64(epoch)), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get checkpoint for epoch %v: %w", epoch, err)
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
