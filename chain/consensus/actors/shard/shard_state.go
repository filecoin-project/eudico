package shard

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
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

// MinMinerStake is the minimum take required for a
// miner to be granted mining rights in the shard and join it.
var MinMinerStake = abi.NewTokenAmount(1e18)

// ConsensusType for shard
type ConsensusType uint64

// List of supported/implemented consensus for shards.
const (
	Delegated ConsensusType = iota
	PoW
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
	// ID of the current network
	Network     cid.Cid
	NetworkName string
	// Total shards below this one.
	TotalShards uint64
	// Minimum stake to create a new shard
	MinStake abi.TokenAmount
	// Minimum stake required by new joiners.
	MinMinerStake abi.TokenAmount
	// List of shards
	Shards cid.Cid // HAMT[cid.Cid]Shard
	// TODO:
	// TotalPower // Total power in shards
}

type Shard struct {
	ID        cid.Cid // Digest of chosen name
	Name      []byte
	Parent    cid.Cid
	Consensus ConsensusType
	// NOTE: Consider adding miners to MinerState in
	// Stake HAMT.
	Miners     []address.Address
	TotalStake abi.TokenAmount
	// TODO: I just realized that BalanceTable is alreaedy
	// a thing and is a handy data structure that we can
	// use here: adt.AsBalanceTable.
	// We are currently going to keep a HAMT here in
	// case MinerState starts growing with additonal params.
	// If this is not the case, let's switch to BalanceTable
	Stake cid.Cid //HAMT[Address]MinerState
	// State of the shard
	Status Status
	// Genesis bootstrap for the shard. This is created
	// when the shard is generated.
	Genesis []byte
	// TODO:
	// PowerTable // PowerTable of miners in shard.
}

type MinerState struct {
	InitialStake abi.TokenAmount
	// NOTE: We may add additional info for miner here.
	// Reputation, slashing, etc.
}

func ConstructShardState(store adt.Store, networkName string) (*ShardState, error) {
	emptyShardsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	networkCid, err := ShardID([]byte(networkName))
	if err != nil {
		panic(err)
	}
	return &ShardState{
		Network:       networkCid,
		NetworkName:   networkName,
		TotalShards:   0,
		MinStake:      MinShardStake,
		MinMinerStake: MinMinerStake,
		Shards:        emptyShardsMapCid,
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

func GetMinerState(stakeMap *adt.Map, miner address.Address) (*MinerState, bool, error) {
	var out MinerState
	found, err := stakeMap.Get(abi.AddrKey(miner), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get stake from miner %v: %w", miner, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

func getStake(stakeMap *adt.Map, miner address.Address) (abi.TokenAmount, error) {
	state, has, err := GetMinerState(stakeMap, miner)
	if err != nil {
		return abi.NewTokenAmount(0), err
	}
	// If the miner has no stake.
	if !has {
		return abi.NewTokenAmount(0), nil
	}
	return state.InitialStake, nil
}

func ShardID(name []byte) (cid.Cid, error) {
	return builder.Sum(name)
}
