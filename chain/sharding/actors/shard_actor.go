package actor

import (
	addr "github.com/filecoin-project/go-address"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/specs-actors/v5/actors/builtin"
	"github.com/filecoin-project/specs-actors/v5/actors/runtime"
	"github.com/filecoin-project/specs-actors/v5/actors/util/adt"
	cid "github.com/ipfs/go-cid"
)

var _ runtime.VMActor = ShardActor{}

// ShardActorAddr is initialized in genesis with the
// address t064
var ShardActorAddr = func() addr.Address {
	a, err := addr.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()

type ShardActor struct{}

func (a ShardActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Add,
		3:                         a.Join,
		// Checkpoint - Add a new checkpoint to the shard.
		// Leave - The miner wants to leave the chain. The shard is killed if the
		// too many miners leave the shard and the amount staked is below minStake.
	}
}

func (a ShardActor) Code() cid.Cid {
	return ShardActorCodeID
}

func (a ShardActor) IsSingleton() bool {
	return false
}

func (a ShardActor) State() cbor.Er {
	return new(ShardState)
}

var _ runtime.VMActor = SplitActor{}

//TODO: Rename to AddShardParams if we keep having more than
// one actor in the same directory. Although this must be rethought.
type AddParams struct {
	Name      []byte
	Consensus ConsensusType
	// NOTE: We could additional parameters here
	// to configure the type of shard to spawn.
	// When FVM is a thing we'll be able to write
	// policies in the form of custom logic for shards.
}

// SelectParams params used to select a specific shard.
// It is used by several functions in the actor.
type SelectParams struct {
	ID []byte
}

type AddShardReturn struct {
	ID cid.Cid
}

func (a ShardActor) Constructor(rt runtime.Runtime, params *ConstructorParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)
	st, err := ConstructShardState(adt.AsStore(rt), params.NetworkName)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

// Add creates a new shard
func (a ShardActor) Add(rt runtime.Runtime, params *AddParams) *AddShardReturn {
	rt.ValidateImmediateCallerAcceptAny()
	shid, err := ShardID(params.Name)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "shard with the same name already exists")
	// Get the miner and the amount that it is sending.
	sourceAddr := rt.Caller()
	value := rt.ValueReceived()

	var st ShardState
	rt.StateTransaction(&st, func() {
		// Check if the shard with that ID already exists, if this is the error
		if _, has, _ := st.GetShard(adt.AsStore(rt), shid); has {
			rt.Abortf(exitcode.ErrIllegalArgument, "can't initialize a shard with existing shardID")
		}
		// We always initialize in instantiated state
		status := Instantiated

		// Instatiate the shard state
		emptyStakeMapAddr, err := adt.StoreEmptyMap(adt.AsStore(rt), builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty stake map addr")
		sh := &Shard{
			ID:         shid,
			Name:       params.Name,
			Parent:     st.Network,
			Consensus:  params.Consensus,
			Miners:     make([]address.Address, 0),
			TotalStake: abi.NewTokenAmount(0),
			Stake:      emptyStakeMapAddr,
			Status:     status,
		}

		sh.addStake(rt, &st, sourceAddr, value)

		// Increase the number of child shards for the current network.
		st.TotalShards++

	})

	return &AddShardReturn{ID: shid}
}

// Join requests joining the shard
func (a ShardActor) Join(rt runtime.Runtime, params *SelectParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	sourceAddr := rt.Caller()
	value := rt.ValueReceived()
	// Decode cid for shard
	c, err := cid.Cast(params.ID)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed to cast cid for shard")

	var st ShardState
	rt.StateTransaction(&st, func() {
		sh, has, err := st.GetShard(adt.AsStore(rt), c)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching shard state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "shard for ID hasn't been instantiated yet")
		}

		// Add stake for the
		sh.addStake(rt, &st, sourceAddr, value)
	})

	return nil
}

func (sh *Shard) addStake(rt runtime.Runtime, st *ShardState, sourceAddr address.Address, value abi.TokenAmount) {
	// Add the amount staked by miner to stake map.
	// NOTE: There's currently no minimum stake required. Any stake is accepted even
	// if a peer is not granted mining rights. According to the final design we may
	// choose to accept only stakes over a minimum amount.
	stakes, err := adt.AsMap(adt.AsStore(rt), sh.Stake, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for stakes in shard")
	minerStake, err := getStake(stakes, sourceAddr)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get stake for miner")
	minerStake = big.Add(minerStake, value)
	err = stakes.Put(abi.AddrKey(sourceAddr), &MinerState{InitialStake: minerStake})
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put miner stake in stake map")
	// Flush stakes adding miner stake.
	sh.Stake, err = stakes.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")

	// Add to totalStake in the shard.
	sh.TotalStake = big.Add(sh.TotalStake, value)

	// Check if the miner has staked enough to be granted mining rights.
	if minerStake.GreaterThanEqual(st.MinMinerStake) {
		sh.Miners = append(sh.Miners, sourceAddr)
	}

	// Check if shard is still instantiated and there is enough stake to become active
	if sh.TotalStake.GreaterThanEqual(st.MinStake) {
		sh.Status = Active
	}

	// Update shard in the list of shards.
	shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for shards")
	err = shards.Put(abi.CidKey(sh.ID), sh)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new shard in shard map")
	// Flush shards
	st.Shards, err = shards.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")
}
