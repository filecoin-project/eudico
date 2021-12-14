package mpower

import (
	"bytes"

	addr "github.com/filecoin-project/go-address"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	rtt "github.com/filecoin-project/go-state-types/rt"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	xerrors "golang.org/x/xerrors"

	power0 "github.com/filecoin-project/specs-actors/actors/builtin/power"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	initact "github.com/filecoin-project/specs-actors/v6/actors/builtin/init"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin/reward"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime/proof"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/actors/util/smoothing"
)

type Runtime = runtime.Runtime

type SectorTermination int64

const (
	ErrTooManyProveCommits = exitcode.FirstActorSpecificExitCode + iota
)

type Actor struct{}

var PowerActorAddr = func() address.Address {
	a, err := address.NewIDAddress(65)
	if err != nil {
		panic(err)
	}
	return a
}()

func (a Actor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.CreateMiner,
		3:                         a.UpdateClaimedPower,
		4:                         a.EnrollCronEvent,
		5:                         a.OnEpochTickEnd,
		6:                         a.UpdatePledgeTotal,
		7:                         nil, // deprecated
		8:                         a.SubmitPoRepForBulkVerify,
		9:                         a.CurrentTotalPower,
	}
}

func (a Actor) Code() cid.Cid {
	return actor.MpowerActorCodeID
}

func (a Actor) IsSingleton() bool {
	return true
}

func (a Actor) State() cbor.Er {
	return new(State)
}

var _ runtime.VMActor = Actor{}

// Storage miner actor constructor params are defined here so the power actor can send them to the init actor
// to instantiate miners.
// Changed since v2:
// - Seal proof type replaced with PoSt proof type
type MinerConstructorParams struct {
	OwnerAddr           addr.Address
	WorkerAddr          addr.Address
	ControlAddrs        []addr.Address
	WindowPoStProofType abi.RegisteredPoStProof
	PeerId              abi.PeerID
	Multiaddrs          []abi.Multiaddrs
}

////////////////////////////////////////////////////////////////////////////////
// Actor methods
////////////////////////////////////////////////////////////////////////////////

func (a Actor) Constructor(rt Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)

	st, err := ConstructState(adt.AsStore(rt))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

// Changed since v2:
// - Seal proof type replaced with PoSt proof types
type CreateMinerParams struct {
	Owner               addr.Address
	Worker              addr.Address
	WindowPoStProofType abi.RegisteredPoStProof
	Peer                abi.PeerID
	Multiaddrs          []abi.Multiaddrs
}

//type CreateMinerReturn struct {
//	IDAddress     addr.Address // The canonical ID-based address for the actor.
//	RobustAddress addr.Address // A more expensive but re-org-safe address for the newly created actor.
//}
type CreateMinerReturn = power0.CreateMinerReturn

func (a Actor) CreateMiner(rt Runtime, params *CreateMinerParams) *CreateMinerReturn {
	rt.ValidateImmediateCallerAcceptAny()

	ctorParams := MinerConstructorParams{
		OwnerAddr:           params.Owner,
		WorkerAddr:          params.Worker,
		WindowPoStProofType: params.WindowPoStProofType,
		PeerId:              params.Peer,
		Multiaddrs:          params.Multiaddrs,
	}
	ctorParamBuf := new(bytes.Buffer)
	err := ctorParams.MarshalCBOR(ctorParamBuf)
	builtin.RequireNoErr(rt, err, exitcode.ErrSerialization, "failed to serialize miner constructor params %v", ctorParams)

	var addresses initact.ExecReturn
	code := rt.Send(
		builtin.InitActorAddr,
		builtin.MethodsInit.Exec,
		&initact.ExecParams{
			CodeCID:           builtin.StorageMinerActorCodeID,
			ConstructorParams: ctorParamBuf.Bytes(),
		},
		rt.ValueReceived(), // Pass on any value to the new actor.
		&addresses,
	)
	builtin.RequireSuccess(rt, code, "failed to init new actor")

	var st State
	rt.StateTransaction(&st, func() {
		claims, err := adt.AsMap(adt.AsStore(rt), st.Claims, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load claims")

		err = setClaim(claims, addresses.IDAddress, &Claim{params.WindowPoStProofType, abi.NewStoragePower(0), abi.NewStoragePower(0)})
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put power in claimed table while creating miner")

		st.MinerCount += 1

		// Ensure new claim updates all power stats
		err = st.updateStatsForNewMiner(params.WindowPoStProofType)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed update power stats for new miner %v", addresses.IDAddress)

		st.Claims, err = claims.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush claims")
	})
	return &CreateMinerReturn{
		IDAddress:     addresses.IDAddress,
		RobustAddress: addresses.RobustAddress,
	}
}

//type UpdateClaimedPowerParams struct {
//	RawByteDelta         abi.StoragePower
//	QualityAdjustedDelta abi.StoragePower
//}
type UpdateClaimedPowerParams = power0.UpdateClaimedPowerParams

// Adds or removes claimed power for the calling actor.
// May only be invoked by a miner actor.
func (a Actor) UpdateClaimedPower(rt Runtime, params *UpdateClaimedPowerParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	minerAddr := rt.Caller()
	var st State
	rt.StateTransaction(&st, func() {
		claims, err := adt.AsMap(adt.AsStore(rt), st.Claims, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load claims")

		err = st.addToClaim(claims, minerAddr, params.RawByteDelta, params.QualityAdjustedDelta)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to update power raw %s, qa %s", params.RawByteDelta, params.QualityAdjustedDelta)

		st.Claims, err = claims.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush claims")
	})
	return nil
}

//type EnrollCronEventParams struct {
//	EventEpoch abi.ChainEpoch
//	Payload    []byte
//}
type EnrollCronEventParams = power0.EnrollCronEventParams

func (a Actor) EnrollCronEvent(rt Runtime, params *EnrollCronEventParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerType(builtin.StorageMinerActorCodeID)
	minerAddr := rt.Caller()
	minerEvent := CronEvent{
		MinerAddr:       minerAddr,
		CallbackPayload: params.Payload,
	}

	// Ensure it is not possible to enter a large negative number which would cause problems in cron processing.
	if params.EventEpoch < 0 {
		rt.Abortf(exitcode.ErrIllegalArgument, "cron event epoch %d cannot be less than zero", params.EventEpoch)
	}

	var st State
	rt.StateTransaction(&st, func() {
		events, err := adt.AsMultimap(adt.AsStore(rt), st.CronEventQueue, CronQueueHamtBitwidth, CronQueueAmtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load cron events")

		err = st.appendCronEvent(events, params.EventEpoch, &minerEvent)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to enroll cron event")

		st.CronEventQueue, err = events.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush cron events")
	})
	return nil
}

// Called by Cron.
func (a Actor) OnEpochTickEnd(rt Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.CronActorAddr)

	var rewret reward.ThisEpochRewardReturn
	rewretcode := rt.Send(builtin.RewardActorAddr, builtin.MethodsReward.ThisEpochReward, nil, big.Zero(), &rewret)
	builtin.RequireSuccess(rt, rewretcode, "failed to check epoch baseline power")

	if err := a.processBatchProofVerifies(rt, rewret); err != nil {
		rt.Log(rtt.ERROR, "unexpected error processing batch proof verifies: %s. Skipping all verification for epoch %d", err, rt.CurrEpoch())
	}
	a.processDeferredCronEvents(rt, rewret)

	var st State
	rt.StateTransaction(&st, func() {
		// update next epoch's power and pledge values
		// this must come before the next epoch's rewards are calculated
		// so that next epoch reward reflects power added this epoch
		rawBytePower, qaPower := CurrentTotalPower(&st)
		st.ThisEpochPledgeCollateral = st.TotalPledgeCollateral
		st.ThisEpochQualityAdjPower = qaPower
		st.ThisEpochRawBytePower = rawBytePower
		// we can now assume delta is one since cron is invoked on every epoch.
		st.updateSmoothedEstimate(abi.ChainEpoch(1))
	})

	// update network KPI in RewardActor
	code := rt.Send(
		builtin.RewardActorAddr,
		builtin.MethodsReward.UpdateNetworkKPI,
		&st.ThisEpochRawBytePower,
		abi.NewTokenAmount(0),
		&builtin.Discard{},
	)
	builtin.RequireSuccess(rt, code, "failed to update network KPI with Reward Actor")

	return nil
}

func (a Actor) UpdatePledgeTotal(rt Runtime, pledgeDelta *abi.TokenAmount) *abi.EmptyValue {
	rt.ValidateImmediateCallerType(builtin.StorageMinerActorCodeID)
	var st State
	rt.StateTransaction(&st, func() {
		validateMinerHasClaim(rt, st, rt.Caller())
		st.addPledgeTotal(*pledgeDelta)
		builtin.RequireState(rt, st.TotalPledgeCollateral.GreaterThanEqual(big.Zero()), "negative total pledge collateral %v", st.TotalPledgeCollateral)
	})
	return nil
}

// GasOnSubmitVerifySeal is amount of gas charged for SubmitPoRepForBulkVerify
// This number is empirically determined
const GasOnSubmitVerifySeal = 34721049

func (a Actor) SubmitPoRepForBulkVerify(rt Runtime, sealInfo *proof.SealVerifyInfo) *abi.EmptyValue {
	rt.ValidateImmediateCallerType(builtin.StorageMinerActorCodeID)

	minerAddr := rt.Caller()

	var st State
	rt.StateTransaction(&st, func() {
		validateMinerHasClaim(rt, st, minerAddr)

		store := adt.AsStore(rt)
		var mmap *adt.Multimap
		var err error
		if st.ProofValidationBatch == nil {
			mmap, err = adt.MakeEmptyMultimap(store, builtin.DefaultHamtBitwidth, ProofValidationBatchAmtBitwidth)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create empty proof validation set")
			rt.Log(rtt.DEBUG, "ProofValidationBatch created")
		} else {
			mmap, err = adt.AsMultimap(adt.AsStore(rt), *st.ProofValidationBatch, builtin.DefaultHamtBitwidth, ProofValidationBatchAmtBitwidth)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load proof batch set")
		}

		arr, found, err := mmap.Get(abi.AddrKey(minerAddr))
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get get seal verify infos at addr %s", minerAddr)
		if found && arr.Length() >= MaxMinerProveCommitsPerEpoch {
			rt.Abortf(ErrTooManyProveCommits, "miner %s attempting to prove commit over %d sectors in epoch", minerAddr, MaxMinerProveCommitsPerEpoch)
		}

		err = mmap.Add(abi.AddrKey(minerAddr), sealInfo)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to insert proof into batch")

		mmrc, err := mmap.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush proof batch")

		rt.ChargeGas("OnSubmitVerifySeal", GasOnSubmitVerifySeal, 0)
		st.ProofValidationBatch = &mmrc
	})

	return nil
}

// Changed since v0:
// - QualityAdjPowerSmoothed is not a pointer
type CurrentTotalPowerReturn struct {
	RawBytePower            abi.StoragePower
	QualityAdjPower         abi.StoragePower
	PledgeCollateral        abi.TokenAmount
	QualityAdjPowerSmoothed smoothing.FilterEstimate
}

// Returns the total power and pledge recorded by the power actor.
// The returned values are frozen during the cron tick before this epoch
// so that this method returns consistent values while processing all messages
// of an epoch.
func (a Actor) CurrentTotalPower(rt Runtime, _ *abi.EmptyValue) *CurrentTotalPowerReturn {
	rt.ValidateImmediateCallerAcceptAny()
	var st State
	rt.StateReadonly(&st)

	return &CurrentTotalPowerReturn{
		RawBytePower:            st.ThisEpochRawBytePower,
		QualityAdjPower:         st.ThisEpochQualityAdjPower,
		PledgeCollateral:        st.ThisEpochPledgeCollateral,
		QualityAdjPowerSmoothed: st.ThisEpochQAPowerSmoothed,
	}
}

////////////////////////////////////////////////////////////////////////////////
// Method utility functions
////////////////////////////////////////////////////////////////////////////////

func validateMinerHasClaim(rt Runtime, st State, minerAddr addr.Address) {
	claims, err := adt.AsMap(adt.AsStore(rt), st.Claims, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load claims")

	found, err := claims.Has(abi.AddrKey(minerAddr))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to look up claim")
	if !found {
		rt.Abortf(exitcode.ErrForbidden, "unknown miner %s forbidden to interact with power actor", minerAddr)
	}
}

func (a Actor) processBatchProofVerifies(rt Runtime, rewret reward.ThisEpochRewardReturn) error {
	var st State

	var miners []addr.Address
	verifies := make(map[addr.Address][]proof.SealVerifyInfo)

	var stErr error
	rt.StateTransaction(&st, func() {
		store := adt.AsStore(rt)
		if st.ProofValidationBatch == nil {
			rt.Log(rtt.DEBUG, "ProofValidationBatch was nil, quitting verification")
			return
		}
		mmap, err := adt.AsMultimap(store, *st.ProofValidationBatch, builtin.DefaultHamtBitwidth, ProofValidationBatchAmtBitwidth)
		if err != nil {
			stErr = xerrors.Errorf("failed to load proofs validation batch: %w", err)
			return
		}

		claims, err := adt.AsMap(adt.AsStore(rt), st.Claims, builtin.DefaultHamtBitwidth)
		if err != nil {
			stErr = xerrors.Errorf("failed to load claims: %w", err)
			return
		}

		err = mmap.ForAll(func(k string, arr *adt.Array) error {
			a, err := addr.NewFromBytes([]byte(k))
			if err != nil {
				return xerrors.Errorf("failed to parse address key: %w", err)
			}

			// refuse to process proofs for miner with no claim
			found, err := claims.Has(abi.AddrKey(a))
			if err != nil {
				return xerrors.Errorf("failed to look up claim: %w", err)
			}
			if !found {
				rt.Log(rtt.WARN, "skipping batch verifies for unknown miner %s", a)
				return nil
			}

			miners = append(miners, a)

			var infos []proof.SealVerifyInfo
			var svi proof.SealVerifyInfo
			err = arr.ForEach(&svi, func(i int64) error {
				infos = append(infos, svi)
				return nil
			})
			if err != nil {
				return xerrors.Errorf("failed to iterate over proof verify array for miner %s: %w", a, err)
			}

			verifies[a] = infos
			return nil
		})
		// Do not return immediately, all runs that get this far should wipe the ProofValidationBatchQueue.
		// If we leave the validation batch then in the case of a repeating state error the queue
		// will quickly fill up and repeated traversals will start ballooning cron execution time.
		if err != nil {
			stErr = xerrors.Errorf("failed to iterate proof batch: %w", err)
		}
		st.ProofValidationBatch = nil
	})
	if stErr != nil {
		return stErr
	}

	res, err := rt.BatchVerifySeals(verifies)
	if err != nil {
		return xerrors.Errorf("failed to batch verify: %w", err)
	}

	for _, m := range miners {
		vres, ok := res[m]
		if !ok {
			return xerrors.Errorf("batch verify seals syscall implemented incorrectly, result not found for miner: %s", m)
		}

		verifs := verifies[m]

		seen := map[abi.SectorNumber]struct{}{}
		var successful []abi.SectorNumber
		for i, r := range vres {
			if r {
				snum := verifs[i].SectorID.Number

				if _, exists := seen[snum]; exists {
					// filter-out duplicates
					rt.Log(rtt.INFO, "skipped over a duplicate proof")
					continue
				}

				seen[snum] = struct{}{}
				successful = append(successful, snum)
			} else {
				rt.Log(rtt.INFO, "a proof failed from miner %s", m)
			}
		}

		if len(successful) > 0 {
			code := rt.Send(
				m,
				builtin.MethodsMiner.ConfirmSectorProofsValid,
				&builtin.ConfirmSectorProofsParams{
					Sectors:                 successful,
					RewardSmoothed:          rewret.ThisEpochRewardSmoothed,
					RewardBaselinePower:     rewret.ThisEpochBaselinePower,
					QualityAdjPowerSmoothed: st.ThisEpochQAPowerSmoothed},
				abi.NewTokenAmount(0),
				&builtin.Discard{},
			)
			if code.IsError() {
				rt.Log(rtt.ERROR,
					"failed to confirm sector proof validity to %s, error code %d",
					m, code)
			}
		}
	}
	return nil
}

func (a Actor) processDeferredCronEvents(rt Runtime, rewret reward.ThisEpochRewardReturn) {
	rtEpoch := rt.CurrEpoch()

	var cronEvents []CronEvent
	var st State
	rt.StateTransaction(&st, func() {
		events, err := adt.AsMultimap(adt.AsStore(rt), st.CronEventQueue, CronQueueHamtBitwidth, CronQueueAmtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load cron events")

		claims, err := adt.AsMap(adt.AsStore(rt), st.Claims, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load claims")

		for epoch := st.FirstCronEpoch; epoch <= rtEpoch; epoch++ {
			epochEvents, err := loadCronEvents(events, epoch)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load cron events at %v", epoch)

			for _, evt := range epochEvents {
				// refuse to process proofs for miner with no claim
				found, err := claims.Has(abi.AddrKey(evt.MinerAddr))
				builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to look up claim")
				if !found {
					rt.Log(rtt.WARN, "skipping cron event for unknown miner %v", evt.MinerAddr)
					continue
				}
				cronEvents = append(cronEvents, evt)
			}

			if len(epochEvents) > 0 {
				err = events.RemoveAll(epochKey(epoch))
				builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to clear cron events at %v", epoch)
			} else {
				rt.Log(rtt.DEBUG, "no epoch events were loaded")
			}
		}

		st.FirstCronEpoch = rtEpoch + 1

		st.CronEventQueue, err = events.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush events")
	})
	failedMinerCrons := make([]addr.Address, 0)

	for _, event := range cronEvents {

		params := builtin.DeferredCronEventParams{
			EventPayload:            event.CallbackPayload,
			RewardSmoothed:          rewret.ThisEpochRewardSmoothed,
			QualityAdjPowerSmoothed: st.ThisEpochQAPowerSmoothed,
		}

		code := rt.Send(
			event.MinerAddr,
			builtin.MethodsMiner.OnDeferredCronEvent,
			&params,
			abi.NewTokenAmount(0),
			&builtin.Discard{},
		)
		// If a callback fails, this actor continues to invoke other callbacks
		// and persists state removing the failed event from the event queue. It won't be tried again.
		// Failures are unexpected here but will result in removal of miner power as a defensive measure.
		if code != exitcode.Ok {
			rt.Log(rtt.ERROR, "OnDeferredCronEvent failed for miner %s: exitcode %d", event.MinerAddr, code)
			failedMinerCrons = append(failedMinerCrons, event.MinerAddr)
		}
	}

	if len(failedMinerCrons) > 0 {
		rt.StateTransaction(&st, func() {
			claims, err := adt.AsMap(adt.AsStore(rt), st.Claims, builtin.DefaultHamtBitwidth)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load claims")

			// Remove miner claim and leave miner frozen
			for _, minerAddr := range failedMinerCrons {
				found, err := st.deleteClaim(claims, minerAddr)
				if err != nil {
					rt.Log(rtt.ERROR, "failed to delete claim for miner %s after failing OnDeferredCronEvent: %s", minerAddr, err)
					continue
				} else if !found {
					rt.Log(rtt.ERROR, "can't find claim for miner %s after failing OnDeferredCronEvent: %s", minerAddr, err)
					continue
				}

				// Decrement miner count to keep stats consistent.
				st.MinerCount--
			}

			st.Claims, err = claims.Root()
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush claims")
		})
	}
}
