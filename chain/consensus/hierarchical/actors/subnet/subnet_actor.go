package subnet

//go:generate go run ./gen/gen.go

import (
	"bytes"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	checkpoint "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	builtin0 "github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
)

var _ runtime.VMActor = SubnetActor{}

var _ SubnetIface = SubnetActor{}

var log = logging.Logger("subnet-actor")

type SubnetActor struct{}

var Methods = struct {
	Constructor      abi.MethodNum
	Join             abi.MethodNum
	Leave            abi.MethodNum
	Kill             abi.MethodNum
	SubmitCheckpoint abi.MethodNum
}{builtin0.MethodConstructor, 2, 3, 4, 5}

func (a SubnetActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Join,
		3:                         a.Leave,
		4:                         a.Kill,
		5:                         a.SubmitCheckpoint,
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
	NetworkName     string                        // Name of the current network.
	Name            string                        // Name for the subnet.
	Consensus       hierarchical.ConsensusType    // Consensus for subnet.
	ConsensusParams *hierarchical.ConsensusParams // Used parameters for the consensus protocol.
	MinMinerStake   abi.TokenAmount               // MinStake to give miner rights.
	CheckPeriod     abi.ChainEpoch                // Checkpointing period.
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

func (st *SubnetState) initGenesis(rt runtime.Runtime, params *ConstructParams) {
	// Build genesis for the subnet assigning delegMiner
	buf := new(bytes.Buffer)

	// TODO: Hard coding the verifyregRoot address here for now.
	// We'll accept it as param in SubnetActor.Add in the next
	// iteration (when we need it).
	vreg, err := address.NewFromString("t3w4spg6jjgfp4adauycfa3fg5mayljflf6ak2qzitflcqka5sst7b7u2bagle3ttnddk6dn44rhhncijboc4q")
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed parsin vreg addr")

	// TODO: Same here, hard coding an address
	// until we need to set it in AddParams.
	rem, err := address.NewFromString("t3tf274q6shnudgrwrwkcw5lzw3u247234wnep37fqx4sobyh2susfvs7qzdwxj64uaizztosuggvyump4xf7a")
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed parsin rem addr")

	// Getting actor ID from receiver.
	netName := address.NewSubnetID(address.SubnetID(params.NetworkName), rt.Receiver())
	err = WriteGenesis(netName, st.Consensus, params.ConsensusParams.DelegMiner, vreg, rem,
		params.CheckPeriod, rt.ValueReceived().Uint64(), buf)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed genesis")
	st.Genesis = buf.Bytes()
}

// Join adds stake to the subnet and/or joins if the source is still not part of it.
// TODO: Join may not be the best name for this function, consider changing it.
func (a SubnetActor) Join(rt runtime.Runtime, v *hierarchical.Validator) *abi.EmptyValue {
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
				code := rt.Send(hierarchical.SubnetCoordActorAddr, sca.Methods.Register, nil, st.TotalStake, &builtin.Discard{})
				if !code.IsSuccess() {
					rt.Abortf(exitcode.ErrIllegalState, "failed registering subnet in SCA")
				}
			}
		}
	} else {
		// We need to send an addStake transaction to SCA
		if rt.CurrentBalance().GreaterThanEqual(value) {
			// Top-up stake in SCA
			code := rt.Send(hierarchical.SubnetCoordActorAddr, sca.Methods.AddStake, nil, value, &builtin.Discard{})
			if !code.IsSuccess() {
				rt.Abortf(exitcode.ErrIllegalState, "failed sending addStake to SCA")
			}
		}
	}

	rt.StateTransaction(&st, func() {
		// Mutate state
		if st.MinValidators > 0 {
			// TODO: we don't check that validators with equal addresses or network addresses already exist.
			st.ValidatorSet = append(st.ValidatorSet, *v)

			log.Debugf("Added validator: %s", v.ID())
			log.Debugf("%d validators have been registered", len(st.ValidatorSet))
		}
	})

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
		code := rt.Send(hierarchical.SubnetCoordActorAddr, sca.Methods.ReleaseStake, &sca.FundParams{Value: minerStake}, big.Zero(), &builtin.Discard{})
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

// verifyCheck verifies the submitted checkpoint and returns the checkpoint signer if valid.
func (st *SubnetState) verifyCheck(rt runtime.Runtime, ch *schema.Checkpoint) address.Address {
	// Check that the subnet is active.
	if st.Status != Active {
		rt.Abortf(exitcode.ErrIllegalState, "submitting checkpoints is not allowed while subnet is not active")
	}

	// Check that the checkpoint for this epoch hasn't been committed yet.
	if _, found, _ := st.GetCheckpoint(adt.AsStore(rt), ch.Epoch()); found {
		rt.Abortf(exitcode.ErrIllegalArgument, "cannot submit checkpoint for epoch that has been committed already")
	}

	// Check that the source is correct.
	shid := address.NewSubnetID(st.ParentID, rt.Receiver())
	if ch.Source() != shid {
		rt.Abortf(exitcode.ErrIllegalArgument, "submitting a checkpoint with the wrong source")
	}

	// Check that the epoch is correct.
	if ch.Epoch()%st.CheckPeriod != 0 {
		rt.Abortf(exitcode.ErrIllegalArgument, "epoch in checkpoint doesn't correspond with signing window")
	}

	// Check that the previous checkpoint is correct.
	prevCom, err := st.PrevCheckCid(adt.AsStore(rt), ch.Epoch())
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching Cid for previous check")
	if prev, _ := ch.PreviousCheck(); prevCom != prev {
		rt.Abortf(exitcode.ErrIllegalArgument, "previous checkpoint not consistent with previous check committed")
	}

	// Check the signature and get address.
	// We are using a simple signature verifier, we could optionally use other verifiers.
	ver := checkpoint.NewSingleSigner()
	sigAddr, err := ver.Verify(ch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "failed to verify signature for submitted checkpoint")

	/*
		// Check that the ID address included in signature belongs to the pkey specified.
		resolved, ok := rt.ResolveAddress(sigAddr.Addr)
		if !ok {
			rt.Abortf(exitcode.ErrIllegalArgument, "unable to resolve address %v", sigAddr.Addr)
		}
	*/

	addr := sigAddr.Addr
	if sigAddr.IDAddr != address.Undef {
		resolved, ok := rt.ResolveAddress(sigAddr.Addr)
		if !ok {
			rt.Abortf(exitcode.ErrIllegalArgument, "unable to resolve address %v", sigAddr.Addr)
		}
		if resolved != sigAddr.IDAddr {
			rt.Abortf(exitcode.ErrIllegalArgument, "inconsistent pkey addr and ID addr in signature")
		}
		addr = sigAddr.IDAddr
	}

	// Only miners (i.e. peers with collateral in subnet) are allowed to submit checkpoints.
	if !st.IsMiner(addr) {
		rt.Abortf(exitcode.ErrIllegalArgument, "checkpoint not signed by a miner")
	}

	return addr

}

// SubmitCheckpoint accepts signed checkpoint votes for miners.
//
// This functions verifies that the checkpoint is valid before
// propagating it for commitment to the SCA. It expects at least
// votes from 2/3 of miners with collateral.
func (a SubnetActor) SubmitCheckpoint(rt runtime.Runtime, params *sca.CheckpointParams) *abi.EmptyValue {
	// Only account actors can submit signed checkpoints for commitment.
	rt.ValidateImmediateCallerType(builtin.AccountActorCodeID)
	submit := &schema.Checkpoint{}
	err := submit.UnmarshalBinary(params.Checkpoint)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error unmarshalling checkpoint in params")

	var st SubnetState
	var majority bool
	rt.StateTransaction(&st, func() {
		// Verify checkpoint and get signer
		signAddr := st.verifyCheck(rt, submit)
		c, err := submit.Cid()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error computing Cid for checkpoint")
		// Get windowChecks for submitted checkpoint
		wch, found, err := st.GetWindowChecks(adt.AsStore(rt), c)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "get list of uncommitted checks")
		if !found {
			wch = &CheckVotes{make([]address.Address, 0)}
		}

		// Check if miner already submitted this checkpoint.
		if HasMiner(signAddr, wch.Miners) {
			rt.Abortf(exitcode.ErrIllegalArgument, "miner already submitted a vote for this checkpoint")
		}

		// Add miners vote
		wch.Miners = append(wch.Miners, signAddr)
		majority, err = st.majorityVote(rt, wch)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching miner stakes")
		if majority {

			// Update checkpoint in SubnetState
			// NOTE: We are including the last signature. It won't be used for verification
			// so this is OK for now. We could also optionally remove the signature to
			// save gas.
			st.flushCheckpoint(rt, submit)

			// Remove windowChecks, the checkpoint has been committed
			// (do this only if they were found before, if not we don't have
			// windowChecks yet)
			if found {
				err := st.rmChecks(adt.AsStore(rt), c)
				builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error removing windowChecks")
				// FIXME: XXX Consider periodically emptying the full votes map to avoid
				// keeping state for wrong Cids committed in the past. This could be
				// a DoS vector/source of inefficiency. Use a rotating map scheme to empty every
				// epoch?
			}
			return
		}

		// If not flush checkWindow and we're good to go!
		st.flushWindowChecks(rt, c, wch)

	})

	// If we reached majority propagate the commitment to SCA
	if majority {
		// If the checkpoint is correct we can reuse params and avoid having to marshal it again.
		code := rt.Send(hierarchical.SubnetCoordActorAddr, sca.Methods.CommitChildCheckpoint, params, big.Zero(), &builtin.Discard{})
		if !code.IsSuccess() {
			rt.Abortf(exitcode.ErrIllegalState, "failed committing checkpoint in SCA")
		}
	}

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
	code := rt.Send(hierarchical.SubnetCoordActorAddr, sca.Methods.Kill, nil, big.Zero(), &builtin.Discard{})
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

func (st *SubnetState) GetStake(store adt.Store, miner address.Address) (big.Int, error) {
	stakes, err := adt.AsBalanceTable(store, st.Stake)
	if err != nil {
		return big.Zero(), err
	}
	return stakes.Get(miner)
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
	// Flush stakes adding miner stake.
	st.Stake, err = stakes.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush subnet")

	// Add to totalStake in the subnet.
	st.TotalStake = big.Add(st.TotalStake, value)

	// Check if the miner has staked enough to be granted mining rights.
	minerStake, err := stakes.Get(sourceAddr)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get stake for miner")
	if minerStake.GreaterThanEqual(st.MinMinerStake) {
		// Except for delegated consensus if there is already a miner.
		// There can only be a single miner in delegated consensus.
		if st.Consensus != hierarchical.Delegated || len(st.Miners) < 1 {
			st.Miners = append(st.Miners, sourceAddr)
		}
	}

}

func (st *SubnetState) rmStake(rt runtime.Runtime, sourceAddr address.Address, stakes *adt.BalanceTable, minerStake abi.TokenAmount) abi.TokenAmount {
	retFunds := big.Div(minerStake, LeavingFeeCoeff)

	// Remove from stakes
	err := stakes.MustSubtract(sourceAddr, minerStake)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed subtracting ablanace for miner")
	// Flush stakes adding miner stake.
	st.Stake, err = stakes.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flust stakes")

	// Remove miner from list of miners if it is there.
	// NOTE: If we decide to support part-recovery of stake from stards
	// we need to check if the miner keeps its mining rights.
	st.Miners = rmMiner(sourceAddr, st.Miners)

	// We are removing what we return to the miner, the rest stays
	// in the subnet, right now the leavingCoeff==1 so there won't be
	// balance left, we'll need to figure out how to distribute this
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
