package shard

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

var (
	// MinShardStake required to create a new shard
	MinShardStake = abi.NewTokenAmount(1e18)

	// MinMinerStake is the minimum take required for a
	// miner to be granted mining rights in the shard and join it.
	MinMinerStake = abi.NewTokenAmount(1e18)

	// LeavingFee Penalization
	// Coefficient divided to miner stake when leaving a shard.
	// NOTE: This is currently set to 1, i.e., the miner recovers
	// its full stake. This may change once cryptoecon is figured out.
	// We'll need to decide what to do with the leftover stake, if to
	// burn it or keep it until the shard is full killed.
	LeavingFeeCoeff = big.NewInt(1)
)

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
	Instantiated Status = iota // Waiting to onboard minimum stake to register in SCA
	Active                     // Active and operating
	Inactive                   // Inactive for lack of stake
	Terminating                // Waiting for everyone to take their funds back and close the shard
	Killed                     // Not active anymore.

)

type ShardState struct {
	Name      string
	ParentCid cid.Cid
	ParentID  string
	Consensus ConsensusType
	// Minimum stake required by new joiners.
	MinMinerStake abi.TokenAmount
	// NOTE: Consider adding miners list as AMT
	Miners     []address.Address
	TotalStake abi.TokenAmount
	Stake      cid.Cid // BalanceTable with the distribution of stake by miners
	// State of the shard
	Status Status
	// Genesis bootstrap for the shard. This is created
	// when the shard is generated.
	Genesis []byte
}

func ConstructShardState(store adt.Store, params *ConstructParams) (*ShardState, error) {
	emptyStakeCid, err := adt.StoreEmptyMap(store, adt.BalanceTableBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create stakes balance table: %w", err)
	}

	/* Initialize AMT of miners.
	emptyArr, err := adt.MakeEmptyArray(adt.AsStore(rt), LaneStatesAmtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty array")
	emptyArrCid, err := emptyArr.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to persist empty array")
	*/

	parentCid, err := naming.ShardCid(params.NetworkName)
	if err != nil {
		panic(err)
	}
	return &ShardState{
		ParentCid:     parentCid,
		ParentID:      params.NetworkName,
		Consensus:     params.Consensus,
		MinMinerStake: params.MinMinerStake,
		Miners:        make([]address.Address, 0),
		Stake:         emptyStakeCid,
		Status:        Instantiated,
	}, nil
}
