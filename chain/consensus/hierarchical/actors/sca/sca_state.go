package sca

import (
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
)

const (
	// CrossMsgsAMTBitwidth determines the bitwidth to use for cross-msg AMT.
	// TODO: We probably need some empirical experiments to determine the best values
	// for these constants.
	CrossMsgsAMTBitwidth = 3

	// MaxNonce supported in cross messages
	// Bear in mind that we cast to Int64 when marshalling in
	// some places
	MaxNonce = ^uint64(0)
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
	// ID of the current network.
	NetworkName address.SubnetID
	// Total subnets below this one.
	TotalSubnets uint64
	// Minimum stake to create a new subnet.
	MinStake abi.TokenAmount
	// List of subnets.
	Subnets cid.Cid // HAMT[cid.Cid]Subnet

	// CheckPeriod is a period in number of epochs.
	CheckPeriod abi.ChainEpoch
	// Checkpoints committed in SCA.
	Checkpoints cid.Cid // HAMT[epoch]Checkpoint

	// CheckMsgMetaRegistry stores information about the list of messages and child msgMetas being
	// propagated in checkpoints to the top of the hierarchy.
	CheckMsgsRegistry cid.Cid // HAMT[cid]CrossMsgs.
	Nonce             uint64  // Latest nonce of cross message sent from subnet.
	BottomUpNonce     uint64  // BottomUpNonce of bottomup messages for msgMeta received from checkpoints (probably redundant).
	BottomUpMsgsMeta  cid.Cid // AMT[schema.CrossMsgs] from child subnets to apply.

	// AppliedBottomUpNonce keeps track of the next nonce of the bottom-up message to be applied.
	AppliedBottomUpNonce uint64
	// AppliedTopDownNonce keeps track of the next nonce of the top-down message to be applied.
	AppliedTopDownNonce uint64

	// Atomic execution state
	AtomicExecRegistry cid.Cid // HAMT[cid]AtomicExec
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
	emptyMsgsMetaMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	emptyAtomicMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	emptyBottomUpMsgsAMT, err := adt.StoreEmptyArray(store, CrossMsgsAMTBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty AMT: %w", err)
	}

	minCheckpointPeriod := hierarchical.DefaultCheckpointPeriod(params.Consensus)
	checkpointPeriod := abi.ChainEpoch(params.CheckpointPeriod)
	if checkpointPeriod < minCheckpointPeriod {
		checkpointPeriod = minCheckpointPeriod
	}

	return &SCAState{
		NetworkName:          address.SubnetID(params.NetworkName),
		TotalSubnets:         0,
		MinStake:             MinSubnetStake,
		Subnets:              emptySubnetsMapCid,
		CheckPeriod:          checkpointPeriod,
		Checkpoints:          emptyCheckpointsMapCid,
		CheckMsgsRegistry:    emptyMsgsMetaMapCid,
		BottomUpMsgsMeta:     emptyBottomUpMsgsAMT,
		AppliedBottomUpNonce: MaxNonce, // We need initial nonce+1 to be 0 due to how messages are applied.
		AtomicExecRegistry:   emptyAtomicMapCid,
	}, nil
}

// GetSubnet gets a subnet from the actor state.
func (st *SCAState) GetSubnet(s adt.Store, id address.SubnetID) (*Subnet, bool, error) {
	subnets, err := adt.AsMap(s, st.Subnets, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load subnets: %w", err)
	}
	return getSubnet(subnets, id)
}

func getSubnet(subnets *adt.Map, id address.SubnetID) (*Subnet, bool, error) {
	var out Subnet
	found, err := subnets.Get(hierarchical.SubnetKey(id), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get subnet with id %v: %w", id, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

func (st *SCAState) flushSubnet(rt runtime.Runtime, sh *Subnet) {
	// Update subnet in the list of subnets.
	subnets, err := adt.AsMap(adt.AsStore(rt), st.Subnets, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for subnets")
	err = subnets.Put(hierarchical.SubnetKey(sh.ID), sh)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new subnet in subnet map")
	// Flush subnets
	st.Subnets, err = subnets.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush subnets")
}

// CurrWindowCheckpoint gets the template of the checkpoint being populated in the current window.
//
// If it hasn't been instantiated, a template is created. From there on,
// the template is populated with every new x-net transaction and
// child checkpoint, until the windows passes that the template is frozen
// and is ready for miners to populate the rest and sign it.
func (st *SCAState) CurrWindowCheckpoint(store adt.Store, epoch abi.ChainEpoch) (*schema.Checkpoint, error) {
	chEpoch := types.WindowEpoch(epoch, st.CheckPeriod)
	ch, found, err := st.GetCheckpoint(store, chEpoch)
	if err != nil {
		return nil, err
	}
	if !found {
		ch = schema.NewRawCheckpoint(st.NetworkName, chEpoch)
	}
	return ch, nil
}

func (st *SCAState) currWindowCheckpoint(rt runtime.Runtime) *schema.Checkpoint {
	ch, err := st.CurrWindowCheckpoint(adt.AsStore(rt), rt.CurrEpoch())
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get checkpoint template for epoch")
	return ch
}

// RawCheckpoint gets the template of the checkpoint in the signing window for an epoch.
//
// It returns the checkpoint that is ready to be signed
// and already includes all the checkpoints and x-net messages
// to include in it. Miners need to populate the prevCheckpoint
// and tipset of this template and sign ot.
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

// GetCheckpoint gets a checkpoint from its index.
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
	// Flush checkpoints.
	st.Checkpoints, err = checks.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush checkpoints")
}

// GetSubnetFromActorAddr gets subnet from its subnet actor address.
func (st *SCAState) getSubnetFromActorAddr(s adt.Store, addr address.Address) (*Subnet, bool, error) {
	shid := address.NewSubnetID(st.NetworkName, addr)
	return st.GetSubnet(s, shid)
}

func (st *SCAState) registerSubnet(rt runtime.Runtime, shid address.SubnetID, stake big.Int) {
	emptyTopDownMsgsAMT, err := adt.StoreEmptyArray(adt.AsStore(rt), CrossMsgsAMTBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty top-down msgs array")

	// We always initialize in instantiated state.
	status := Active

	sh := &Subnet{
		ID:             shid,
		ParentID:       st.NetworkName,
		Stake:          stake,
		TopDownMsgs:    emptyTopDownMsgsAMT,
		CircSupply:     big.Zero(),
		Status:         status,
		PrevCheckpoint: *schema.EmptyCheckpoint,
	}

	// Increase the number of child subnets for the current network.
	st.TotalSubnets++

	// Flush subnet into subnetMap.
	st.flushSubnet(rt, sh)
}

// ListSubnets lists subnets.
func ListSubnets(s adt.Store, st *SCAState) ([]Subnet, error) {
	subnetMap, err := adt.AsMap(s, st.Subnets, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, err
	}

	var sh Subnet
	var out []Subnet
	err = subnetMap.ForEach(&sh, func(k string) error {
		out = append(out, sh)
		return nil
	})
	return out, err
}
