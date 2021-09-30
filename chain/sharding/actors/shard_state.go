package actor

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/v3/actors/builtin"
	"github.com/filecoin-project/specs-actors/v3/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"
)

// Builder to generate shard IDs from their name
var builder = cid.V1Builder{Codec: cid.Raw, MhType: mh.IDENTITY}

// MinShardStake required to create a new shard
// TODO: Kept in 1FIL for testing, change to the right
// value once we decide it.
// We could make this value configurable in construction.
var MinShardStake = abi.NewTokenAmount(1e18)

// ConsensusType for shard
type ConsensusType uint64

// List of supported/implemented consensus for shards.
const (
	Delegated ConsensusType = iota
)

// ShardStatus describes in what state in its lifecycle a shard is.
type Status uint64

const (
	Instantiated Status = iota // Waiting to onboard minimum stake and power
	Active                     // Active and operating
	Terminating                // Waiting for everyone to take their funds back and close the shard
	Killed                     // Not active anymore.

)

type ShardState struct {
	// TODO: In this iteration we will hardcore the Network,
	// and thus the parentID of every shard to always be root.
	// In the next iteration we'll change this and set the network
	// name in genesis when the network/shard is created. But deferring
	// this until we manage to make sharding work with a minimal version
	// of this actor.
	// ID of the current network
	Network cid.Cid
	// Total shards below this one.
	TotalShards uint64
	// Minimum stake to create a new shard
	MinStake abi.TokenAmount
	// List of shards
	Shards cid.Cid // HAMT[cid.Cid]Shard
	// TODO:
	// TotalPower // Total power in shards
}

type Shard struct {
	ID         cid.Cid // Digest of chosen name
	Name       []byte
	Parent     cid.Cid
	Consensus  ConsensusType
	Miners     []address.Address
	TotalStake abi.TokenAmount
	Stake      cid.Cid //HAMT[Address]MinerState
	Status     Status
	// TODO:
	// PowerTable // PowerTable of miners in shard.
}

type MinerState struct {
	InitialStake abi.TokenAmount
	// NOTE: We may add additional info for miner here.
	// Reputation, slashing, etc.
}

func ConstructShardState(store adt.Store) (*ShardState, error) {
	emptyShardsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	// TODO: Hardcoding the Network to "root", this should change and
	// we should accept the network name as a constructor parameter.
	networkCid, err := ShardID([]byte("root"))
	if err != nil {
		panic(err)
	}
	return &ShardState{
		Network:     networkCid,
		TotalShards: 0,
		MinStake:    MinShardStake,
		Shards:      emptyShardsMapCid,
	}, nil
}

// GetShard gets a shard from the actor state.
func (st *ShardState) GetShard(s adt.Store, id cid.Cid) (*Shard, bool, error) {
	claims, err := adt.AsMap(s, st.Shards, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load claims: %w", err)
	}
	return getShard(claims, id)
}

func getShard(shards *adt.Map, id cid.Cid) (*Shard, bool, error) {
	var out Shard
	found, err := shards.Get(abi.CidKey(id), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get shard with id %v: %w", id, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

func ShardID(name []byte) (cid.Cid, error) {
	return builder.Sum(name)
}
