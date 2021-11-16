package sca

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

var (
	// MinShardStake required to register a new shard
	// TODO: Kept in 1FIL for testing, change to the right
	// value once we decide it.
	// We could make this value configurable in construction.
	MinShardStake = abi.NewTokenAmount(1e18)
)

// Status describes in what state in its lifecycle a shard is.
type Status uint64

const (
	Active   Status = iota // Active and operating. Has permission to interact with other chains in the hierarchy
	Inactive               // Waiting for the stake to be top-up over the MinStake threshold
	Killed                 // Not active anymore.

)

// SCAState represents the state of the Shard Coordinator Actor
type SCAState struct {
	// CID of the current network
	Network cid.Cid
	// ID of the current network
	NetworkName string
	// Total shards below this one.
	TotalShards uint64
	// Minimum stake to create a new shard
	MinStake abi.TokenAmount
	// List of shards
	Shards cid.Cid // HAMT[cid.Cid]Shard
}

type Shard struct {
	Cid      cid.Cid // Cid of the shard ID
	ID       string  // human-readable name of the shard ID (path in the hierarchy)
	Parent   cid.Cid
	ParentID string
	Stake    abi.TokenAmount
	// The SCA doesn't keep track of the stake from miners, just locks the funds.
	// Is up to the shard actor to handle this and distribute the stake
	// when the shard is killed.
	// NOTE: We may want to keep track of this in the future.
	// Stake      cid.Cid // BalanceTable with locked stake.
	Funds  cid.Cid // BalanceTable with funds from addresses that entered the shard.
	Status Status
}

func ConstructSCAState(store adt.Store, networkName string) (*SCAState, error) {
	emptyShardsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	networkCid, err := naming.ShardCid(networkName)
	if err != nil {
		panic(err)
	}
	return &SCAState{
		Network:     networkCid,
		NetworkName: networkName,
		TotalShards: 0,
		MinStake:    MinShardStake,
		Shards:      emptyShardsMapCid,
	}, nil
}

// GetShard gets a shard from the actor state.
func (st *SCAState) GetShard(s adt.Store, id cid.Cid) (*Shard, bool, error) {
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

// Get shard from its shard actor address.
func (st *SCAState) getShardFromActorAddr(s adt.Store, addr address.Address) (*Shard, bool, error) {
	shid := naming.GenShardID(st.NetworkName, addr)
	shcid, err := naming.ShardCid(shid)
	if err != nil {
		return nil, false, err
	}
	return st.GetShard(s, shcid)
}

func ListShards(s adt.Store, st SCAState) ([]Shard, error) {
	shardMap, err := adt.AsMap(s, st.Shards, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, err
	}
	var sh Shard
	out := []Shard{}
	err = shardMap.ForEach(&sh, func(k string) error {
		out = append(out, sh)
		return nil
	})
	return out, err
}
