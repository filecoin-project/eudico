package delegcns

import (
	"context"
	"sync/atomic"

	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opencensus.io/stats"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"

	exported6 "github.com/filecoin-project/specs-actors/v6/actors/builtin/exported"

	reward "github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/rand"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/metrics"
)

func DefaultUpgradeSchedule() stmgr.UpgradeSchedule {
	var us stmgr.UpgradeSchedule

	updates := []stmgr.Upgrade{{
		Height:    -1,
		Network:   network.Version13,
		Migration: nil,
		Expensive: true,
	},
	}

	for _, u := range updates {
		if u.Height < 0 {
			// upgrade disabled
			continue
		}
		us = append(us, u)
	}
	return us
}

func NewActorRegistry() *vm.ActorRegistry {
	inv := vm.NewActorRegistry()

	// TODO: drop unneeded
	inv.Register(vm.ActorsVersionPredicate(actors.Version5), exported6.BuiltinActors()...)
	inv.Register(nil, InitActor{}) // use our custom init actor

	inv.Register(nil, SplitActor{})

	return inv
}

type tipSetExecutor struct{}

func (t *tipSetExecutor) NewActorRegistry() *vm.ActorRegistry {
	return NewActorRegistry()
}

func TipSetExecutor() stmgr.Executor {
	return &tipSetExecutor{}
}

func (t *tipSetExecutor) ApplyBlocks(ctx context.Context, sm *stmgr.StateManager, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []store.BlockMessages, epoch abi.ChainEpoch, r vm.Rand, em stmgr.ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet) (cid.Cid, cid.Cid, error) {
	done := metrics.Timer(ctx, metrics.VMApplyBlocksTotal)
	defer done()

	partDone := metrics.Timer(ctx, metrics.VMApplyEarly)
	defer func() {
		partDone()
	}()

	makeVmWithBaseState := func(base cid.Cid) (*vm.VM, error) {
		vmopt := &vm.VMOpts{
			StateBase:      base,
			Epoch:          epoch,
			Rand:           r,
			Bstore:         sm.ChainStore().StateBlockstore(),
			Actors:         NewActorRegistry(),
			Syscalls:       sm.Syscalls,
			CircSupplyCalc: sm.GetVMCirculatingSupply,
			NtwkVersion:    sm.GetNtwkVersion,
			BaseFee:        baseFee,
			LookbackState:  stmgr.LookbackStateGetterForTipset(sm, ts),
		}

		return sm.VMConstructor()(ctx, vmopt)
	}

	vmi, err := makeVmWithBaseState(pstate)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
	}

	for i := parentEpoch; i < epoch; i++ {
		// handle state forks
		// XXX: The state tree
		newState, err := sm.HandleStateForks(ctx, pstate, i, em, ts)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("error handling state forks: %w", err)
		}

		if pstate != newState {
			vmi, err = makeVmWithBaseState(newState)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
			}
		}

		vmi.SetBlockHeight(i + 1)
		pstate = newState
	}

	partDone()
	partDone = metrics.Timer(ctx, metrics.VMApplyMessages)

	var receipts []cbg.CBORMarshaler
	processedMsgs := make(map[cid.Cid]struct{})
	for _, b := range bms {
		penalty := types.NewInt(0)
		gasReward := big.Zero()

		for _, cm := range append(b.BlsMessages, b.SecpkMessages...) {
			m := cm.VMMessage()
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			r, err := vmi.ApplyMessage(ctx, cm)
			if err != nil {
				return cid.Undef, cid.Undef, err
			}

			receipts = append(receipts, &r.MessageReceipt)
			gasReward = big.Add(gasReward, r.GasCosts.MinerTip)
			penalty = big.Add(penalty, r.GasCosts.MinerPenalty)

			if em != nil {
				if err := em.MessageApplied(ctx, ts, cm.Cid(), m, r, false); err != nil {
					return cid.Undef, cid.Undef, err
				}
			}
			processedMsgs[m.Cid()] = struct{}{}
		}

		rwMsg := &types.Message{
			From:       reward.Address,
			To:         b.Miner,
			Nonce:      uint64(epoch),
			Value:      types.FromFil(1), // always reward 1 fil
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   1 << 30,
			Method:     0,
		}
		ret, actErr := vmi.ApplyImplicitMessage(ctx, rwMsg)
		if actErr != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to apply reward message for miner %s: %w", b.Miner, actErr)
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, rwMsg.Cid(), rwMsg, ret, true); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("callback failed on reward message: %w", err)
			}
		}

		if ret.ExitCode != 0 {
			return cid.Undef, cid.Undef, xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
		}
	}

	partDone()
	partDone = metrics.Timer(ctx, metrics.VMApplyCron)

	partDone()
	partDone = metrics.Timer(ctx, metrics.VMApplyFlush)

	rectarr := blockadt.MakeEmptyArray(sm.ChainStore().ActorStore(ctx))
	for i, receipt := range receipts {
		if err := rectarr.Set(uint64(i), receipt); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
		}
	}
	rectroot, err := rectarr.Root()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
	}

	st, err := vmi.Flush(ctx)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("vm flush failed: %w", err)
	}

	stats.Record(ctx, metrics.VMSends.M(int64(atomic.LoadUint64(&vm.StatSends))),
		metrics.VMApplied.M(int64(atomic.LoadUint64(&vm.StatApplied))))

	return st, rectroot, nil
}

func (t *tipSetExecutor) ExecuteTipSet(ctx context.Context, sm *stmgr.StateManager, ts *types.TipSet, em stmgr.ExecMonitor) (stateroot cid.Cid, rectsroot cid.Cid, err error) {
	ctx, span := trace.StartSpan(ctx, "computeTipSetState")
	defer span.End()

	blks := ts.Blocks()

	for i := 0; i < len(blks); i++ {
		for j := i + 1; j < len(blks); j++ {
			if blks[i].Miner == blks[j].Miner {
				return cid.Undef, cid.Undef,
					xerrors.Errorf("duplicate miner in a tipset (%s %s)",
						blks[i].Miner, blks[j].Miner)
			}
		}
	}

	var parentEpoch abi.ChainEpoch
	pstate := blks[0].ParentStateRoot
	if blks[0].Height > 0 {
		parent, err := sm.ChainStore().GetBlock(blks[0].Parents[0])
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("getting parent block: %w", err)
		}

		parentEpoch = parent.Height
	}

	// TODO: No beacon assigned to StateRand. I don't think is needed for delegated consensus.
	r := rand.NewStateRand(sm.ChainStore(), ts.Cids(), nil)

	blkmsgs, err := sm.ChainStore().BlockMsgsForTipset(ts)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting block messages for tipset: %w", err)
	}

	baseFee := blks[0].ParentBaseFee

	return t.ApplyBlocks(ctx, sm, parentEpoch, pstate, blkmsgs, blks[0].Height, r, em, baseFee, ts)
}

var _ stmgr.Executor = &tipSetExecutor{}
