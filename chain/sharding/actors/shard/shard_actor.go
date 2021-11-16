package shard

import (
	"bytes"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
)

var _ runtime.VMActor = ShardActor{}

// var _ ActorIface = ShardActor{}

var log = logging.Logger("shard-actor")

type ShardActor struct{}

func (a ShardActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Add,
		3:                         a.Join,
		4:                         a.Leave,
		// Checkpoint - Add a new checkpoint to the shard.
	}
}

func (a ShardActor) Code() cid.Cid {
	return actor.ShardActorCodeID
}

func (a ShardActor) IsSingleton() bool {
	return false
}

func (a ShardActor) State() cbor.Er {
	return new(ShardState)
}

// CreateParams specifies the configuration parameters for the
// shard actor constructor.
type ConstructParams struct {
	NetworkName   string
	Name          []byte
	Consensus     ConsensusType
	MinMinerStake abi.TokenAmount
	DelegMiner    address.Address
	Metadata      []byte
}

// SelectParams params used to select a specific shard.
// It is used by several functions in the actor.
type SelectParams struct {
	ID []byte
}

type AddShardReturn struct {
	ID cid.Cid
}

func (a ShardActor) Constructor(rt runtime.Runtime, params *ConstructParams) *abi.EmptyValue {
	// Shard actors need to be deployed through the init actor.
	rt.ValidateImmediateCallerType(builtin.InitActorCodeID)
	st, err := ConstructShardState(adt.AsStore(rt), params)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")

	rt.StateCreate(st)
	return nil
}

func (a ShardActor) Checkpoint(rt runtime.Runtime, params abi.EmptyValue) abi.EmptyValue {
	panic("checkpoint not implemented yet")
}

func (sh *ShardState) initGenesis(rt runtime.Runtime, params *ConstructParams) {
	rt.NewActorAddress()
	// Build genesis for the shard assigning delegMiner
	buf := new(bytes.Buffer)

	// TODO: Hardcoding the verifyregRoot address here for now.
	// We'll accept it as param in shardactor.Add in the next
	// iteration (when we need it).
	vreg, err := address.NewFromString("t3w4spg6jjgfp4adauycfa3fg5mayljflf6ak2qzitflcqka5sst7b7u2bagle3ttnddk6dn44rhhncijboc4q")
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed parsin vreg addr")

	// TODO: Same here, hardcoding an address
	// until we need to set it in AddParams.
	rem, err := address.NewFromString("t3tf274q6shnudgrwrwkcw5lzw3u247234wnep37fqx4sobyh2susfvs7qzdwxj64uaizztosuggvyump4xf7a")
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed parsin rem addr")

	// TODO: Get the actor address in order to generate the genesis of the subnet.
	// We may need to find an alternative way.
	err = WriteGenesis(shid.String(), sh.Consensus, params.DelegMiner, vreg, rem, st.TotalShards, buf)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed genesis")
	sh.Genesis = buf.Bytes()
}

/*
// initShard initialzes a shard.
func (a ShardActor) initShard(rt runtime.Runtime, params *ConstructParams) *AddShardReturn {
	rt.ValidateImmediateCallerAcceptAny()
	if string(params.Name) == "" {
		rt.Abortf(exitcode.ErrIllegalArgument, "can't start a shard with an empty name")
	}
	// TODO: Add checks for minStakes?
	if
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "shard with the same name already exists")
	// Get the miner and the amount that it is sending.
	sourceAddr := rt.Caller()
	value := rt.ValueReceived()

	var st ShardState
	rt.StateTransaction(&st, func() {

		// TODO: Everything is specific for the delegated consensus now
		// (the only consensus supported). We should choose the right option
		// when we suport new consensus.
		sh.addStake(rt, &st, sourceAddr, value)

	})

	return &AddShardReturn{ID: shid}
}
*/

// Join adds stake to the shard and/or joins if the source is still not part of it.
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
		// Add stake for the miner
		sh.addStake(rt, &st, sourceAddr, value)
	})

	return nil
}

// Leave can be used for users to leave the shard and recover their state.
// NOTE: At this stage we will only support to fully leave the shard and
// not to recover part of the stake. We are going to set a leaving fee
// but this will need to be revisited when we design sharding cryptoecon model.
func (a ShardActor) Leave(rt runtime.Runtime, params *SelectParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	sourceAddr := rt.Caller()
	// Decode cid for shard
	c, err := cid.Cast(params.ID)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed to cast cid for shard")

	priorBalance := rt.CurrentBalance()
	var st ShardState
	var retFunds abi.TokenAmount
	rt.StateTransaction(&st, func() {
		sh, has, err := st.GetShard(adt.AsStore(rt), c)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching shard state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalArgument, "shard for ID hasn't been instantiated yet")
		}
		// Remove stake. Kill the shard if needed
		retFunds = sh.rmStake(rt, &st, sourceAddr)
	})

	// Never send back if we don't have enough balance
	builtin.RequireState(rt, retFunds.LessThanEqual(priorBalance), "reward %v exceeds balance %v", retFunds, priorBalance)

	// Send funds back to owner
	code := rt.Send(sourceAddr, builtin.MethodSend, nil, retFunds, &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed to send stake back to address, code: %v", code)
	}

	return nil
}

func (sh *Shard) addStake(rt runtime.Runtime, st *ShardState, sourceAddr address.Address, value abi.TokenAmount) {
	// NOTE: There's currently no minimum stake required. Any stake is accepted even
	// if a peer is not granted mining rights. According to the final design we may
	// choose to accept only stakes over a minimum amount.
	// Add the amount staked by miner to stake map.
	stakes, err := adt.AsMap(adt.AsStore(rt), sh.Stake, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for stakes in stakes")
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
		// Except for delegated consensus if there is already a miner.
		// There can only be a single miner in delegated consensus.
		if sh.Consensus != Delegated || len(sh.Miners) < 1 {
			sh.Miners = append(sh.Miners, sourceAddr)
		}
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

func (sh *Shard) rmStake(rt runtime.Runtime, st *ShardState, sourceAddr address.Address) abi.TokenAmount {

	stakes, err := adt.AsMap(adt.AsStore(rt), sh.Stake, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for stakes in shard")
	minerStake, err := getStake(stakes, sourceAddr)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get stake for miner")
	if minerStake.Equals(abi.NewTokenAmount(0)) {
		rt.Abortf(exitcode.ErrForbidden, "caller hasn't stake in this shard")
	}
	retFunds := big.Div(minerStake, LeavingFeeCoeff)

	// Remove from stakes
	err = stakes.Delete(abi.AddrKey(sourceAddr))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to remove miner stake in stake map")
	// Flush stakes adding miner stake.
	sh.Stake, err = stakes.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush stakes")

	// Remove miner from list of miners if it is there.
	// NOTE: If we decide to support part-recovery of stake from shards
	// we need to check if the miner keeps its mining rights.
	sh.Miners = rmMiner(sourceAddr, sh.Miners)

	// We are removing what we return to the miner, the rest stays
	// in the shard, we'll need to figure out what to do with the balance
	sh.TotalStake = big.Sub(sh.TotalStake, retFunds)

	shards, err := adt.AsMap(adt.AsStore(rt), st.Shards, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for shards")
	// Check if shard is still instantiated and there is enough stake to become active
	if sh.TotalStake.LessThan(st.MinStake) {
		lstakes, err := ListStakes(adt.AsStore(rt), sh)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get list of stakes")
		if len(lstakes) == 0 {
			// No stakes left, we can kill the shard but maybe not
			// remove it if there is still stake.
			// FIXME: Decide what to do with the pending state, if any.
			sh.Status = Killed
			if sh.TotalStake.LessThanEqual(big.NewInt(0)) {
				// Remove shard completely.
				err = shards.Delete(abi.CidKey(sh.ID))
				builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new shard in shard map")
				// Flush shards
				st.Shards, err = shards.Root()
				builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")
				st.TotalShards--
			}
			return retFunds
		}
		// Terminating because there is not minimum stake
		sh.Status = Terminating
	}

	// There are still miners with stake in the shard, so don't kill it
	// The shard is either active or terminating.
	err = shards.Put(abi.CidKey(sh.ID), sh)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put new shard in shard map")
	// Flush shards
	st.Shards, err = shards.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush shards")
	return retFunds
}

func rmMiner(miner address.Address, ls []address.Address) []address.Address {
	for i, v := range ls {
		if v == miner {
			return append(ls[:i], ls[i+1:]...)
		}
	}
	return ls
}
