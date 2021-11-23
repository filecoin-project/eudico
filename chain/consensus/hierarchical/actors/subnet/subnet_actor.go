package subnet

//go:generate go run ./gen/gen.go

import (
	"bytes"

	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	builtin0 "github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
)

var _ runtime.VMActor = SubnetActor{}

var log = logging.Logger("subnet-actor")

type SubnetActor struct{}

var Methods = struct {
	Constructor abi.MethodNum
	Join        abi.MethodNum
	Leave       abi.MethodNum
	Kill        abi.MethodNum
}{builtin0.MethodConstructor, 2, 3, 4}

func (a SubnetActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Join,
		3:                         a.Leave,
		4:                         a.Kill,
		// Checkpoint - Add a new checkpoint to the subnet.
	}
}

func (a SubnetActor) Code() cid.Cid {
	return actor.SubnetActorCodeID
}

func (a SubnetActor) IsSingleton() bool {
	return false
}

func (a SubnetActor) State() cbor.Er {
	return new(SubnetState)
}

// ConstructParams specifies the configuration parameters for the
// subnet actor constructor.
type ConstructParams struct {
	NetworkName   string          // Name of the current network.
	Name          string          // Name for the subnet
	Consensus     ConsensusType   // Consensus for subnet.
	MinMinerStake abi.TokenAmount // MinStake to give miner rights
	DelegMiner    address.Address // Miner in delegated consensus
}

func (a SubnetActor) Constructor(rt runtime.Runtime, params *ConstructParams) *abi.EmptyValue {
	// Subnet actors need to be deployed through the init actor.
	rt.ValidateImmediateCallerType(builtin.InitActorCodeID)
	st, err := ConstructSubnetState(adt.AsStore(rt), params)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	st.initGenesis(rt, params)

	rt.StateCreate(st)
	return nil
}

func (a SubnetActor) Checkpoint(rt runtime.Runtime, params abi.EmptyValue) abi.EmptyValue {
	panic("checkpoint not implemented yet")
}

func (st *SubnetState) initGenesis(rt runtime.Runtime, params *ConstructParams) {
	// Build genesis for the subnet assigning delegMiner
	buf := new(bytes.Buffer)

	// TODO: Hardcoding the verifyregRoot address here for now.
	// We'll accept it as param in SubnetActor.Add in the next
	// iteration (when we need it).
	vreg, err := address.NewFromString("t3w4spg6jjgfp4adauycfa3fg5mayljflf6ak2qzitflcqka5sst7b7u2bagle3ttnddk6dn44rhhncijboc4q")
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed parsin vreg addr")

	// TODO: Same here, hardcoding an address
	// until we need to set it in AddParams.
	rem, err := address.NewFromString("t3tf274q6shnudgrwrwkcw5lzw3u247234wnep37fqx4sobyh2susfvs7qzdwxj64uaizztosuggvyump4xf7a")
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed parsin rem addr")

	// Getting actor ID from recceiver.
	netName := hierarchical.NewSubnetID(hierarchical.SubnetID(params.NetworkName), rt.Receiver())
	err = WriteGenesis(netName, st.Consensus, params.DelegMiner, vreg, rem, rt.ValueReceived().Uint64(), buf)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed genesis")
	st.Genesis = buf.Bytes()
}

// Join adds stake to the subnet and/or joins if the source is still not part of it.
// TODO: Join may not be the best name for this function, consider changing it.
func (a SubnetActor) Join(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	sourceAddr := rt.Caller()
	value := rt.ValueReceived()

	var st SubnetState
	rt.StateTransaction(&st, func() {
		st.addStake(rt, sourceAddr, value)
	})

	// If the subnet is not registered, i.e. in an instantiated state
	if st.Status == Instantiated {
		// If we have enough stake we can register the subnet in the SCA
		if st.TotalStake.GreaterThanEqual(sca.MinSubnetStake) {
			if rt.CurrentBalance().GreaterThanEqual(st.TotalStake) {
				// Send a transaction with the total stake to the subnet actor.
				// We are discarding the result (which is the CID assigned for the subnet)
				// because we can compute it deterministically, but we can consider keeping it.
				code := rt.Send(sca.SubnetCoordActorAddr, sca.Methods.Register, nil, st.TotalStake, &builtin.Discard{})
				if !code.IsSuccess() {
					rt.Abortf(exitcode.ErrIllegalState, "failed registering subnet in SCA")
				}
			}
		}
	} else {
		// We need to send an addStake transaction to SCA
		if rt.CurrentBalance().GreaterThanEqual(value) {
			// Top-up stake in SCA
			code := rt.Send(sca.SubnetCoordActorAddr, sca.Methods.AddStake, nil, value, &builtin.Discard{})
			if !code.IsSuccess() {
				rt.Abortf(exitcode.ErrIllegalState, "failed sending addStake to SCA")
			}
		}
	}

	rt.StateTransaction(&st, func() {
		// Mutate state
		st.mutateState(rt)
	})

	return nil
}

// Leave can be used for users to leave the subnet and recover their state.
//
// NOTE: At this stage we will only support to fully leave the subnet and
// not to recover part of the stake. We are going to set a leaving fee
// but this will need to be revisited when we design subneting cryptoecon model.
func (a SubnetActor) Leave(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	sourceAddr := rt.Caller()

	var (
		st         SubnetState
		minerStake abi.TokenAmount
		stakes     *adt.BalanceTable
		err        error
	)
	// We first get the miner stake to know how much to release from the SCA stake
	rt.StateTransaction(&st, func() {
		stakes, err = adt.AsBalanceTable(adt.AsStore(rt), st.Stake)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state balance map for stakes")
		minerStake, err = stakes.Get(sourceAddr)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get stake for miner")
		if minerStake.Equals(abi.NewTokenAmount(0)) {
			rt.Abortf(exitcode.ErrForbidden, "caller has no stake in subnet")
		}
	})

	// Release stake from SCA if all the stake hasn't been released already because the subnet
	// is in a terminating state
	if st.Status != Terminating {
		code := rt.Send(sca.SubnetCoordActorAddr, sca.Methods.ReleaseStake, &sca.FundParams{Value: minerStake}, big.Zero(), &builtin.Discard{})
		if !code.IsSuccess() {
			rt.Abortf(exitcode.ErrIllegalState, "failed releasing stake in SCA")
		}
	}

	priorBalance := rt.CurrentBalance()
	var retFunds abi.TokenAmount
	rt.StateTransaction(&st, func() {
		// Remove stake from stake balanace table.
		retFunds = st.rmStake(rt, sourceAddr, stakes, minerStake)
	})

	// Never send back if we don't have enough balance
	builtin.RequireState(rt, retFunds.LessThanEqual(priorBalance), "returning stake %v exceeds balance %v", retFunds, priorBalance)

	// Send funds back to owner
	code := rt.Send(sourceAddr, builtin.MethodSend, nil, retFunds, &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed to send stake back to address, code: %v", code)
	}

	rt.StateTransaction(&st, func() {
		// Mutate state
		st.mutateState(rt)
	})
	return nil
}

// Kill is used to signal that the subnet must be terminated.
//
// In the current policy any user can terminate the subnet and recover their stake
// as long as there are no miners in the network.
func (a SubnetActor) Kill(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	var st SubnetState
	rt.StateTransaction(&st, func() {
		if st.Status == Terminating || st.Status == Killed {
			rt.Abortf(exitcode.ErrIllegalState, "the subnet is already in a killed or terminating state")
		}
		if len(st.Miners) != 0 {
			rt.Abortf(exitcode.ErrIllegalState, "this subnet can only be killed when all miners have left")
		}
		// Move to terminating state
		st.Status = Terminating

	})

	// Kill (unregister) subnet from SCA and release full stake
	code := rt.Send(sca.SubnetCoordActorAddr, sca.Methods.Kill, nil, big.Zero(), &builtin.Discard{})
	if !code.IsSuccess() {
		rt.Abortf(exitcode.ErrIllegalState, "failed killing subnet in SCA")
	}

	rt.StateTransaction(&st, func() {
		// Mutate state
		st.mutateState(rt)
	})
	return nil
}

func (st *SubnetState) mutateState(rt runtime.Runtime) {

	switch st.Status {
	case Instantiated:
		if st.TotalStake.GreaterThanEqual(sca.MinSubnetStake) {
			st.Status = Active
		}
	case Active:
		if st.TotalStake.LessThan(sca.MinSubnetStake) {
			st.Status = Inactive
		}
	case Inactive:
		if st.TotalStake.GreaterThanEqual(sca.MinSubnetStake) {
			st.Status = Active
		}
	// In the current implementation after Kill is triggered, the
	// subnet enters a killing state and can't be revived. The subnet
	// stays in a terminating state until all the funds have been recovered.
	case Terminating:
		if st.TotalStake.Equals(abi.NewTokenAmount(0)) &&
			rt.CurrentBalance().Equals(abi.NewTokenAmount(0)) {
			st.Status = Killed
		}
	case Killed:
		break
	}
}
func (st *SubnetState) addStake(rt runtime.Runtime, sourceAddr address.Address, value abi.TokenAmount) {
	// NOTE: There's currently no minimum stake required. Any stake is accepted even
	// if a peer is not granted mining rights. According to the final design we may
	// choose to accept only stakes over a minimum amount.
	stakes, err := adt.AsBalanceTable(adt.AsStore(rt), st.Stake)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state balance map for stakes")
	// Add the amount staked by miner to stake map.
	err = stakes.Add(sourceAddr, value)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error adding stake to user balance table")
	// Flust stakes adding miner stake.
	st.Stake, err = stakes.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flust stards")

	// Add to totalStake in the stard.
	st.TotalStake = big.Add(st.TotalStake, value)

	// Check if the miner has staked enough to be granted mining rights.
	minerStake, err := stakes.Get(sourceAddr)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get stake for miner")
	if minerStake.GreaterThanEqual(st.MinMinerStake) {
		// Except for delegated consensus if there is already a miner.
		// There can only be a single miner in delegated consensus.
		if st.Consensus != Delegated || len(st.Miners) < 1 {
			st.Miners = append(st.Miners, sourceAddr)
		}
	}

}

func (st *SubnetState) rmStake(rt runtime.Runtime, sourceAddr address.Address, stakes *adt.BalanceTable, minerStake abi.TokenAmount) abi.TokenAmount {
	retFunds := big.Div(minerStake, LeavingFeeCoeff)

	// Remove from stakes
	err := stakes.MustSubtract(sourceAddr, minerStake)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed substracting ablanace for miner")
	// Flush stakes adding miner stake.
	st.Stake, err = stakes.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flust stakes")

	// Remove miner from list of miners if it is there.
	// NOTE: If we decide to support part-recovery of stake from stards
	// we need to check if the miner keeps its mining rights.
	st.Miners = rmMiner(sourceAddr, st.Miners)

	// We are removing what we return to the miner, the rest stays
	// in the subnet, right now the leavingCoeff==1 so there won't be
	// balance letf, we'll need to figure out how to distribute this
	// in the future.
	st.TotalStake = big.Sub(st.TotalStake, retFunds)

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
