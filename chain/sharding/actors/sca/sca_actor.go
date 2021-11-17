package sca

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	initactor "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	builtin0 "github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/v3/actors/builtin"
	"github.com/filecoin-project/specs-actors/v3/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	cid "github.com/ipfs/go-cid"
)

var _ runtime.VMActor = ShardCoordActor{}

// ShardCoordActorAddr is initialized in genesis with the
// address t064
var ShardCoordActorAddr = func() address.Address {
	a, err := address.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()

var Methods = struct {
	Constructor  abi.MethodNum
	Register     abi.MethodNum
	AddStake     abi.MethodNum
	ReleaseStake abi.MethodNum
	Kill         abi.MethodNum
}{builtin0.MethodConstructor, 2, 3, 4, 5}

type FundParams struct {
	Value abi.TokenAmount
}

type AddShardReturn struct {
	Cid cid.Cid
}
type ShardCoordActor struct{}

func (a ShardCoordActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Register,
		3:                         a.AddStake,
		4:                         a.ReleaseStake,
		5:                         a.Kill,
		// -1:                         a.Fund,
		// -1:                         a.Release,
		// -1:                         a.Checkpoint,
		// -1:                         a.RawCheckpoint,
		// -1:                         a.XShardTx,
	}
}

func (a ShardCoordActor) Code() cid.Cid {
	return actor.ShardCoordActorCodeID
}

func (a ShardCoordActor) IsSingleton() bool {
	return true
}

func (a ShardCoordActor) State() cbor.Er {
	return new(SCAState)
}

func (a ShardCoordActor) Constructor(rt runtime.Runtime, params *initactor.ConstructorParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)
	st, err := ConstructSCAState(adt.AsStore(rt), params.NetworkName)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

// Register
//
// It registers a new shard actor to the hierarchical consensus.
// In order for the registering of a shard to be successful, the transaction
// needs to stake at least the minimum stake, if not it'll fail.
func (a ShardCoordActor) Register(rt runtime.Runtime, _ *abi.EmptyValue) *AddShardReturn {
	// Register can only be called by an actor implementing the shard actor interface.
	rt.ValidateImmediateCallerType(actor.ShardActorCodeID)
	shardActorAddr := rt.Caller()

	var st SCAState
	var shcid cid.Cid
	rt.StateTransaction(&st, func() {
		var err error
		shid := naming.GenShardID(st.NetworkName, shardActorAddr)
		shcid, err = naming.ShardCid(shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed computing CID from shardID")
		// Check if the shard with that ID already exists
		if _, has, _ := st.GetShard(adt.AsStore(rt), shcid); has {
			rt.Abortf(exitcode.ErrIllegalArgument, "can't register a shard that has been already registered")
		}
		// Check if the transaction has enough funds to register the shard.
		value := rt.ValueReceived()
		if value.LessThanEqual(st.MinStake) {
			rt.Abortf(exitcode.ErrIllegalArgument, "call to register doesn't include enough funds to stake")
		}

		// We always initialize in instantiated state
		status := Active

		// Instatiate the shard state
		emptyFundBalances, err := adt.StoreEmptyMap(adt.AsStore(rt), adt.BalanceTableBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty funds balance table")

		sh := &Shard{
			Cid:      shcid,
			ID:       shid,
			Parent:   st.Network,
			ParentID: st.NetworkName,
			Stake:    value,
			Funds:    emptyFundBalances,
			Status:   status,
		}

		// Increase the number of child shards for the current network.
		st.TotalShards++

		// Flush shard into shardMap
		sh.flushShard(rt, &st)
	})

	return &AddShardReturn{Cid: shcid}
}

// AddStake
//
// Locks more stake from an actor. This needs to be triggered
// by the shard actor with the shard logic.
func (a ShardCoordActor) AddStake(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Can only be called by an actor implementing the shard actor interface.
	rt.ValidateImmediateCallerType(actor.ShardActorCodeID)
	shardActorAddr := rt.Caller()

	var st SCAState
	rt.StateTransaction(&st, func() {
		// Check if the shard for the actor exists
		sh, has, err := st.getShardFromActorAddr(adt.AsStore(rt), shardActorAddr)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error fetching shard state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "shard for for actor hasn't been registered yet")
		}

		// Check if the transaction includes funds
		value := rt.ValueReceived()
		if value.LessThanEqual(big.NewInt(0)) {
			rt.Abortf(exitcode.ErrIllegalArgument, "no funds included in transaction")
		}

		// Increment stake locked for shard.
		sh.addStake(rt, &st, value)
	})

	return nil
}

// ReleaseStake
//
// Request from the shard actor to release part of the stake locked for shard.
// Is up to the shard actor to do the corresponding verifications and
// distribute the funds to its owners.
func (a ShardCoordActor) ReleaseStake(rt runtime.Runtime, params *FundParams) *abi.EmptyValue {
	// Can only be called by an actor implementing the shard actor interface.
	rt.ValidateImmediateCallerType(actor.ShardActorCodeID)
	shardActorAddr := rt.Caller()

	if params.Value.LessThanEqual(abi.NewTokenAmount(0)) {
		rt.Abortf(exitcode.ErrIllegalArgument, "no funds included in params")
	}
	var st SCAState
	rt.StateTransaction(&st, func() {
		// Check if the shard for the actor exists
		sh, has, err := st.getShardFromActorAddr(adt.AsStore(rt), shardActorAddr)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching shard state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "shard for for actor hasn't been registered yet")
		}

		// Check if the shard actor is allowed to release the amount of stake specified.
		if sh.Stake.LessThan(params.Value) {
			rt.Abortf(exitcode.ErrIllegalState, "shard actor not allowed to release that many funds")
		}

		// This is a sanity check to ensure that there is enough balance in actor.
		if rt.CurrentBalance().LessThan(params.Value) {
			rt.Abortf(exitcode.ErrIllegalState, "yikes! actor doesn't have enough balance to release these funds")
		}

		// Decrement locked stake
		sh.addStake(rt, &st, params.Value.Neg())
	})

	// Send a transaction with the funds to the shard actor.
	code := rt.Send(shardActorAddr, builtin.MethodSend, nil, params.Value, &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed sending released stake to shard actor")
	}

	return nil
}

// Kill
//
// Unregisters a subnet from the hierarchical consensus
func (a ShardCoordActor) Kill(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	// Can only be called by an actor implementing the shard actor interface.
	rt.ValidateImmediateCallerType(actor.ShardActorCodeID)
	shardActorAddr := rt.Caller()

	var st SCAState
	var sh *Shard
	rt.StateTransaction(&st, func() {
		var has bool
		shid := naming.GenShardID(st.NetworkName, shardActorAddr)
		shcid, err := naming.ShardCid(shid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed computing CID from shardID")
		// Check if the shard for the actor exists
		sh, has, err = st.GetShard(adt.AsStore(rt), shcid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching shard state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "shard for for actor hasn't been registered yet")
		}

		// This is a sanity check to ensure that there is enough balance in actor to return stakes
		if rt.CurrentBalance().LessThan(sh.Stake) {
			rt.Abortf(exitcode.ErrIllegalState, "yikes! actor doesn't have enough balance to release these funds")
		}

		// Remove shard from shard registry.
		shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for shards")
		err = shards.Delete(abi.CidKey(shcid))
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to remove miner stake in stake map")
		// Flush stakes adding miner stake.
		st.Shards, err = shards.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards after removal")
	})

	// Send a transaction with the total stake to the shard actor.
	code := rt.Send(shardActorAddr, builtin.MethodSend, nil, sh.Stake, &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed sending released stake to shard actor")
	}

	return nil
}

// addStake adds new funds to the stake of the shard.
//
// This function also accepts negative values to substract, and checks
// if the funds are enough for the shard to be active.
func (sh *Shard) addStake(rt runtime.Runtime, st *SCAState, value abi.TokenAmount) {
	// Add stake to the shard
	sh.Stake = big.Add(sh.Stake, value)

	// Check if shard has still stake to be active
	if sh.Stake.LessThan(st.MinStake) {
		sh.Status = Inactive
	}

	// Flush shard into shardMap
	sh.flushShard(rt, st)

}

func (sh *Shard) flushShard(rt runtime.Runtime, st *SCAState) {
	// Update shard in the list of shards.
	shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for shards")
	err = shards.Put(abi.CidKey(sh.Cid), sh)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new shard in shard map")
	// Flush shards
	st.Shards, err = shards.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")
}
