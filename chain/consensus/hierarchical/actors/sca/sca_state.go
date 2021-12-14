package sca

import (
	"math"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
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
	// for testing purposes.
	DefaultCheckpointPeriod = abi.ChainEpoch(10)
	// MinCheckpointPeriod allowed for subnets
	MinCheckpointPeriod = abi.ChainEpoch(10)

	// CrossMsgsAMTBitwidth determines the bitwidth to use for cross-msg AMT.
	CrossMsgsAMTBitwidth = 3
	// MaxNonce supported in cross messages
	MaxNonce = math.MaxInt64
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
	Funds     cid.Cid // BalanceTable with funds from addresses that entered the subnet.
	CrossMsgs cid.Cid // AMT[*ltypes.Messages] of cross messages to subnet.
	// NOTE: We can avoid explitly storing the Nonce here and use CrossMsgs length
	// to determine the nonce. Deferring that for future iterations.
	Nonce      uint64          // Latest nonce of cross message submitted to subnet.
	CircSupply abi.TokenAmount // Circulating supply of FIL in subnet.
	Status     Status
	// NOTE: We could probably save some gas here without affecting the
	// overall behavior of check committment by just keeping the information
	// required for verification (prevCheck cid and epoch).
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

// freezeFunds freezes some funds from an address to inject them into a subnet.
func (sh *Subnet) freezeFunds(rt runtime.Runtime, source address.Address, value abi.TokenAmount) {
	funds, err := adt.AsBalanceTable(adt.AsStore(rt), sh.Funds)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state balance map for frozen funds")
	// Add the amount being frozen to address.
	err = funds.Add(source, value)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error adding frozen funds to user balance table")
	// Flush funds adding miner stake.
	sh.Funds, err = funds.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush funds")

	// Increase circulating supply in subnet.
	sh.CircSupply = big.Add(sh.CircSupply, value)
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

// RawCheckpoint gets the template of the checkpoint in
// the signing window for an epoch
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

func (st *SCAState) registerSubnet(rt runtime.Runtime, shid hierarchical.SubnetID, stake big.Int) {
	emptyFundBalances, err := adt.StoreEmptyMap(adt.AsStore(rt), adt.BalanceTableBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty funds balance table")
	emptyCrossMsgsAMT, err := adt.StoreEmptyArray(adt.AsStore(rt), CrossMsgsAMTBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty cross msgs array")

	// We always initialize in instantiated state
	status := Active

	sh := &Subnet{
		ID:             shid,
		ParentID:       st.NetworkName,
		Stake:          stake,
		Funds:          emptyFundBalances,
		CrossMsgs:      emptyCrossMsgsAMT,
		CircSupply:     big.Zero(),
		Status:         status,
		PrevCheckpoint: *schema.EmptyCheckpoint,
	}

	// Increase the number of child subnets for the current network.
	st.TotalSubnets++

	// Flush subnet into subnetMap
	st.flushSubnet(rt, sh)
}

func (sh *Subnet) addFundMsg(rt runtime.Runtime, value big.Int) {
	// Source
	// NOTE: We are including here the ID address from the source, but the user
	// may have a completely different ID address in the subnet. Nodes will have
	// to translate this ID address to the right SECP/BLS address that owns the
	// account
	source := rt.Caller()

	// Build message.
	//
	// Fund messages include the same to and from.
	msg := &ltypes.Message{
		To:         source,
		From:       source,
		Value:      value,
		Nonce:      sh.Nonce,
		GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
		GasFeeCap:  ltypes.NewInt(0),
		GasPremium: ltypes.NewInt(0),
		Params:     nil,
	}

	// Store in the list of cross messages.
	sh.storeCrossMsg(rt, msg)

	// Increase nonce.
	sh.incrementNonce(rt)
}

func (sh *Subnet) storeCrossMsg(rt runtime.Runtime, msg *ltypes.Message) {
	crossMsgs, err := adt.AsArray(adt.AsStore(rt), sh.CrossMsgs, CrossMsgsAMTBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load cross-messages")
	// Set message in AMT
	err = crossMsgs.Set(msg.Nonce, msg)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to store cross-messages")
	// Flush AMT
	sh.CrossMsgs, err = crossMsgs.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush cross-messages")

}

func (sh *Subnet) GetCrossMsg(s adt.Store, nonce uint64) (*ltypes.Message, bool, error) {
	crossMsgs, err := adt.AsArray(s, sh.CrossMsgs, CrossMsgsAMTBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load cross-msgs: %w", err)
	}
	return getCrossMsg(crossMsgs, nonce)
}

func getCrossMsg(crossMsgs *adt.Array, nonce uint64) (*ltypes.Message, bool, error) {
	if nonce > MaxNonce {
		return nil, false, xerrors.Errorf("maximum cross-message nonce is 2^63-1")
	}
	var out ltypes.Message
	found, err := crossMsgs.Get(nonce, &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get cross-msg with nonce %v: %w", nonce, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

func (sh *Subnet) incrementNonce(rt runtime.Runtime) {
	// Increment nonce.
	sh.Nonce++

	// If overflow we restart from zero.
	if sh.Nonce > MaxNonce {
		// FIXME: This won't be a problem in the short-term, but we should handle this.
		// We could maybe use a snapshot or paging approach so new peers can sync
		// from scratch while restarting the nonce for cross-message for subnets to zero.
		// sh.Nonce = 0
		rt.Abortf(exitcode.ErrIllegalState, "nonce overflow not supported yet")
	}
}

func (sh *Subnet) CrossMsgFromNonce(s adt.Store, nonce uint64) ([]*ltypes.Message, error) {
	crossMsgs, err := adt.AsArray(s, sh.CrossMsgs, CrossMsgsAMTBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to load cross-msgs: %w", err)
	}
	// FIXME: Consider setting the length of the slice in advance
	// to improve performance.
	out := make([]*ltypes.Message, 0)
	for i := nonce; i < sh.Nonce; i++ {
		msg, found, err := getCrossMsg(crossMsgs, i)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, msg)
		}
	}
	return out, nil
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
