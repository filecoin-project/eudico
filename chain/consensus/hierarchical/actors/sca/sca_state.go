package sca

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

const (
	// DefaultCheckpointPeriod defines 10 epochs
	// as the default checkpoint period for a subnet.
	// This may be too short, but at this point it comes pretty handy
	// for testing purpose.
	DefaultCheckpointPeriod = abi.ChainEpoch(10)

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
}

type Subnet struct {
	ID       hierarchical.SubnetID // human-readable name of the subnet ID (path in the hierarchy)
	ParentID hierarchical.SubnetID
	Stake    abi.TokenAmount
	// The SCA doesn't keep track of the stake from miners, just locks the funds.
	// Is up to the subnet actor to handle this and distribute the stake
	// when the subnet is killed.
	// NOTE: We may want to keep track of this in the future.
	// Stake      cid.Cid // BalanceTable with locked stake.
	Funds          cid.Cid // BalanceTable with funds from addresses that entered the subnet.
	Status         Status
	PrevCheckpoint schema.Checkpoint
}

func ConstructSCAState(store adt.Store, params *ConstructorParams) (*SCAState, error) {
	emptySubnetsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	emptyCheckpointsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	nn := hierarchical.SubnetID(params.NetworkName)
	// Don't allow really small checkpoint periods for now.
	period := abi.ChainEpoch(params.CheckpointPeriod)
	if period < MinCheckpointPeriod {
		period = DefaultCheckpointPeriod
	}

	return &SCAState{
		NetworkName:  nn,
		TotalSubnets: 0,
		MinStake:     MinSubnetStake,
		Subnets:      emptySubnetsMapCid,
		CheckPeriod:  period,
		Checkpoints:  emptyCheckpointsMapCid,
	}, nil
}

// GetSubnet gets a subnet from the actor state.
func (st *SCAState) GetSubnet(s adt.Store, id hierarchical.SubnetID) (*Subnet, bool, error) {
	subnets, err := adt.AsMap(s, st.Subnets, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load subnets: %w", err)
	}
	return getSubnet(subnets, id)
}

func getSubnet(subnets *adt.Map, id hierarchical.SubnetID) (*Subnet, bool, error) {
	var out Subnet
	found, err := subnets.Get(id, &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get subnet with id %v: %w", id, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

// addStake adds new funds to the stake of the subnet.
//
// This function also accepts negative values to substract, and checks
// if the funds are enough for the subnet to be active.
func (sh *Subnet) addStake(rt runtime.Runtime, st *SCAState, value abi.TokenAmount) {
	// Add stake to the subnet
	sh.Stake = big.Add(sh.Stake, value)

	// Check if subnet has still stake to be active
	if sh.Stake.LessThan(st.MinStake) {
		sh.Status = Inactive
	}

	// Flush subnet into subnetMap
	st.flushSubnet(rt, sh)

}

func (st *SCAState) flushSubnet(rt runtime.Runtime, sh *Subnet) {
	// Update subnet in the list of subnets.
	subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for subnets")
	err = subnets.Put(sh.ID, sh)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new subnet in subnet map")
	// Flush subnets
	st.Subnets, err = subnets.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush subnets")
}

// currWindowCheckpoint gets the template of the checkpoint being
// populated in the current window.
//
// If it hasn't been instantiated, a template is created. From there on,
// the template is populated with every new xShard transaction and
// child checkpoint, until the windows passes that the template is frozen
// and is ready for miners to populate the rest and sign it.
func (st *SCAState) currWindowCheckpoint(rt runtime.Runtime) *schema.Checkpoint {
	chEpoch := types.WindowEpoch(rt.CurrEpoch(), st.CheckPeriod)
	ch, found, err := st.GetCheckpoint(adt.AsStore(rt), chEpoch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get checkpoint template for epoch")
	if !found {
		ch = schema.NewRawCheckpoint(st.NetworkName, chEpoch)
	}
	return ch
}

// rawCheckpoint gets the template of the checkpoint in
// the signing window.
//
// It returns the checkpoint that is ready to be signed
// and already includes all the checkpoints and xshard messages
// to include in it. Miners need to populate the prevCheckpoint
// and tipset of this template and sign ot.
func (st *SCAState) rawCheckpoint(rt runtime.Runtime) *schema.Checkpoint {
	ch, err := RawCheckpoint(st, adt.AsStore(rt), rt.CurrEpoch())
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get raw checkpoint template for epoch")
	return ch
}

// RawCheckpoint gets the template of the checkpoint in
// the signing window for an epoch
func RawCheckpoint(st *SCAState, store adt.Store, epoch abi.ChainEpoch) (*schema.Checkpoint, error) {
	if epoch < 0 {
		return nil, xerrors.Errorf("epoch can't be negative")
	}
	chEpoch := types.CheckpointEpoch(epoch, st.CheckPeriod)
	ch, found, err := st.GetCheckpoint(store, chEpoch)
	if err != nil {
		return nil, err
	}
	// If nothing has been populated yet return an empty checkpoint.
	if !found {
		ch = schema.NewRawCheckpoint(st.NetworkName, chEpoch)
	}
	return ch, nil
}

// GetCheckpoint gets a checkpoint from its index
func (st *SCAState) GetCheckpoint(s adt.Store, epoch abi.ChainEpoch) (*schema.Checkpoint, bool, error) {
	checkpoints, err := adt.AsMap(s, st.Checkpoints, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load checkpoint: %w", err)
	}
	return getCheckpoint(checkpoints, epoch)
}

func getCheckpoint(checkpoints *adt.Map, epoch abi.ChainEpoch) (*schema.Checkpoint, bool, error) {
	var out schema.Checkpoint
	found, err := checkpoints.Get(abi.UIntKey(uint64(epoch)), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get checkpoint for epoch %v: %w", epoch, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

func (st *SCAState) flushCheckpoint(rt runtime.Runtime, ch *schema.Checkpoint) {
	// Update subnet in the list of checkpoints.
	checks, err := adt.AsMap(adt.AsStore(rt), st.Checkpoints, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for checkpoints")
	err = checks.Put(abi.UIntKey(uint64(ch.Data.Epoch)), ch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put checkpoint in map")
	// Flush checkpoints
	st.Checkpoints, err = checks.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush checkpoints")
}

// Get subnet from its subnet actor address.
func (st *SCAState) getSubnetFromActorAddr(s adt.Store, addr address.Address) (*Subnet, bool, error) {
	shid := hierarchical.NewSubnetID(st.NetworkName, addr)
	return st.GetSubnet(s, shid)
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
