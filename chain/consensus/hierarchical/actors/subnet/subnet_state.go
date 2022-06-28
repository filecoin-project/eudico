package subnet

import (
	mbig "math/big"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
)

var (
	// MinMinerStake is the minimum take required for a
	// miner to be granted mining rights in the subnet and join it.
	MinMinerStake = abi.NewTokenAmount(1e18)

	// LeavingFeeCoeff Penalization
	// Coefficient divided to miner stake when leaving a subnet.
	// NOTE: This is currently set to 1, i.e., the miner recovers
	// its full stake. This may change once cryptoecon is figured out.
	// We'll need to decide what to do with the leftover stake, if to
	// burn it or keep it until the subnet is full killed.
	LeavingFeeCoeff = big.NewInt(1)

	// SignatureThreshold that determines the number of votes from
	// total number of miners expected to propagate a checkpoint to
	// SCA
	SignatureThreshold = mbig.NewFloat(0.66)
)

// Status describes in what state in its lifecycle a subnet is.
type Status uint64

const (
	Instantiated Status = iota // Waiting to onboard minimum stake to register in SCA
	Active                     // Active and operating
	Inactive                   // Inactive for lack of stake
	Terminating                // Waiting for everyone to take their funds back and close the subnet
	Killed                     // Not active anymore.
)

type SubnetState struct {
	// Human-readable name of the subnet.
	Name string
	// ID of the parent subnet
	ParentID address.SubnetID
	// Type of Consensus algorithm.
	Consensus hierarchical.ConsensusType
	// Minimum stake required for an address to join the subnet
	// as a miner
	MinMinerStake abi.TokenAmount
	// List of miners in the subnet.
	// NOTE: Consider using AMT.
	Miners []address.Address
	// Total collateral currently deposited in the
	TotalStake abi.TokenAmount
	// BalanceTable with the distribution of stake by address
	Stake cid.Cid // HAMT[tokenAmount]address
	// State of the subnet (Active, Inactive, Terminating)
	Status Status
	// Genesis bootstrap for the subnet. This is created
	// when the subnet is generated.
	Genesis []byte
	// Checkpointing period.
	CheckPeriod abi.ChainEpoch
	// Finality threshold.
	FinalityThreshold abi.ChainEpoch
	// Checkpoints submit to SubnetActor per epoch
	Checkpoints cid.Cid // HAMT[epoch]Checkpoint
	// WindowChecks
	WindowChecks cid.Cid // HAMT[cid]CheckVotes
	// ValidatorSet is a set of validators
	ValidatorSet []hierarchical.Validator
	// MinValidators is the minimal number of validators required to join before starting the subnet
	MinValidators uint64
}

type CheckVotes struct {
	// NOTE: I don't think we need to store the checkpoint for anything.
	// By keeping the Cid of the checkpoint as the key is enough and we
	// save space
	// Checkpoint schema.Checkpoint
	Miners []address.Address
}

func (st SubnetState) majorityVote(rt runtime.Runtime, wch *CheckVotes) (bool, error) {
	sum := big.Zero()
	for _, m := range wch.Miners {
		stake, err := st.GetStake(adt.AsStore(rt), m)
		if err != nil {
			return false, err
		}
		sum = big.Sum(sum, stake)
	}
	fsum := new(mbig.Float).SetInt(sum.Int)
	fTotal := new(mbig.Float).SetInt(st.TotalStake.Int)
	div := new(mbig.Float).Quo(fsum, fTotal)
	return div.Cmp(SignatureThreshold) >= 0, nil
}

func ConstructSubnetState(store adt.Store, params *ConstructParams) (*SubnetState, error) {
	emptyStakeCid, err := adt.StoreEmptyMap(store, adt.BalanceTableBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create stakes balance table: %w", err)
	}
	emptyCheckpointsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	emptyWindowChecks, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	// TODO: @alfonso do we need this?
	/* Initialize AMT of miners.
	emptyArr, err := adt.MakeEmptyArray(adt.AsStore(rt), LaneStatesAmtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty array")
	emptyArrCid, err := emptyArr.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to persist empty array")
	*/

	minCheckpointPeriod := hierarchical.DefaultCheckpointPeriod(params.Consensus)
	checkpointPeriod := params.CheckpointPeriod
	if checkpointPeriod < minCheckpointPeriod {
		checkpointPeriod = minCheckpointPeriod
	}

	minFinality := hierarchical.DefaultFinality(params.Consensus)
	finality := params.FinalityThreshold
	if finality < minFinality {
		finality = minFinality
	}

	// Finality should always be less than the checkpoint period.
	if finality >= checkpointPeriod {
		return nil, xerrors.Errorf("finality threshold (%v) must be less than checkpoint period (%v)",
			finality, checkpointPeriod)
	}

	parentID := address.SubnetID(params.NetworkName)

	return &SubnetState{
		ParentID:          parentID,
		Consensus:         params.Consensus,
		MinMinerStake:     params.MinMinerStake,
		Miners:            make([]address.Address, 0),
		Stake:             emptyStakeCid,
		Status:            Instantiated,
		CheckPeriod:       checkpointPeriod,
		Checkpoints:       emptyCheckpointsMapCid,
		FinalityThreshold: finality,
		WindowChecks:      emptyWindowChecks,
		ValidatorSet:      make([]hierarchical.Validator, 0),
		MinValidators:     params.ConsensusParams.MinValidators,
	}, nil

}

// PrevCheckCid returns the Cid of the previously committed checkpoint
func (st *SubnetState) PrevCheckCid(store adt.Store, epoch abi.ChainEpoch) (cid.Cid, error) {
	ep := epoch - st.CheckPeriod
	// From epoch back if we found a previous checkpoint
	// committed we return its CID
	for ep >= 0 {
		ch, found, err := st.GetCheckpoint(store, ep)
		if err != nil {
			return cid.Undef, err
		}
		if found {
			return ch.Cid()
		}
		ep = ep - st.CheckPeriod
	}
	// If nothing is found return NoPreviousCheckCommit
	return schema.NoPreviousCheck, nil
}

// GetCheckpoint gets a checkpoint from its index
func (st *SubnetState) GetCheckpoint(s adt.Store, epoch abi.ChainEpoch) (*schema.Checkpoint, bool, error) {
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

func (st *SubnetState) flushCheckpoint(rt runtime.Runtime, ch *schema.Checkpoint) {
	// Update subnet in the list of checkpoints.
	checks, err := adt.AsMap(adt.AsStore(rt), st.Checkpoints, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for checkpoints")
	err = checks.Put(abi.UIntKey(uint64(ch.Data.Epoch)), ch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put checkpoint in map")
	// Flush checkpoints
	st.Checkpoints, err = checks.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush checkpoints")
}

// GetWindowChecks with the list of uncommitted checkpoints.
func (st *SubnetState) GetWindowChecks(s adt.Store, checkCid cid.Cid) (*CheckVotes, bool, error) {
	checks, err := adt.AsMap(s, st.WindowChecks, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load windowCheck: %w", err)
	}

	var out CheckVotes
	found, err := checks.Get(abi.CidKey(checkCid), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get windowCheck for Cid %v: %w", checkCid, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

func (st *SubnetState) rmChecks(s adt.Store, checkCid cid.Cid) error {
	checks, err := adt.AsMap(s, st.WindowChecks, builtin.DefaultHamtBitwidth)
	if err != nil {
		return xerrors.Errorf("failed to load windowCheck: %w", err)
	}

	if err := checks.Delete(abi.CidKey(checkCid)); err != nil {
		return err
	}
	st.WindowChecks, err = checks.Root()
	return err
}

func (st *SubnetState) flushWindowChecks(rt runtime.Runtime, checkCid cid.Cid, w *CheckVotes) {
	checks, err := adt.AsMap(adt.AsStore(rt), st.WindowChecks, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for windowChecks")
	err = checks.Put(abi.CidKey(checkCid), w)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put windowCheck in map")
	// Flush windowCheck
	st.WindowChecks, err = checks.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush windowChecks")
}

func (st *SubnetState) IsMiner(addr address.Address) bool {
	return HasMiner(addr, st.Miners)
}

func HasMiner(addr address.Address, miners []address.Address) bool {
	for _, a := range miners {
		if a == addr {
			return true
		}
	}
	return false
}
