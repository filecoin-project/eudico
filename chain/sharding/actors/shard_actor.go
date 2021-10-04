package actor

import (
	addr "github.com/filecoin-project/go-address"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
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
		// Join - A miner wants to join the shard
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
}

type AddShardReturn struct {
	ID cid.Cid
}

// TODO: Change constructor to accept ConstructorParams (see actor_init) as
// input and set the NetwrorkName when initializing the actor. This will require
// changing how this actor is initialized.
func (a ShardActor) Constructor(rt runtime.Runtime, params *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)
	st, err := ConstructShardState(adt.AsStore(rt))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

// Add creates a new shard
func (a ShardActor) Add(rt runtime.Runtime, params *AddParams) *AddShardReturn {
	rt.ValidateImmediateCallerAcceptAny()
	// Check if the shard with that ID already exists, if this is the error
	shid, err := ShardID(params.Name)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "shard with the same name already exists")
	// Get the miner and the amount that it is sending.
	sourceAddr := rt.Caller()
	value := rt.ValueReceived()

	var st ShardState
	rt.StateTransaction(&st, func() {
		// Check if there is enough stake to activate the shard.
		status := Instantiated
		if value.GreaterThanEqual(st.MinStake) {
			status = Active
		}

		// Instatiate the shard state
		emptyStakeMapAddr, err := adt.StoreEmptyMap(adt.AsStore(rt), builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty stake map addr")
		sh := &Shard{
			ID:         shid,
			Name:       params.Name,
			Parent:     st.Network,
			Consensus:  params.Consensus,
			Miners:     []address.Address{sourceAddr},
			TotalStake: value,
			Stake:      emptyStakeMapAddr,
			Status:     status,
		}
		// Add the amount staked by miner to stake map.
		stakes, err := adt.AsMap(adt.AsStore(rt), sh.Stake, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for stakes in shard")
		err = stakes.Put(abi.AddrKey(sourceAddr), &MinerState{InitialStake: value})
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put miner stake in stake map")
		// Flush stakes adding miner stake.
		sh.Stake, err = stakes.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")

		// Add shard to the list of shards.
		shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for shards")
		err = shards.Put(abi.CidKey(shid), sh)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new shard in shard map")
		// Flush shards
		st.Shards, err = shards.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")
		// Increase the number of child shards for the current network.
		st.TotalShards++

	})

	return &AddShardReturn{ID: shid}
}
