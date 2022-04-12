package common

import (
	"context"
	"sync/atomic"

	"github.com/filecoin-project/lotus/chain/consensus/actors/registry"
	"github.com/filecoin-project/lotus/chain/consensus/common/crossmsg"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/rand"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opencensus.io/stats"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/cron"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/metrics"
)

type FilCnsTipSetExecutor struct{}

func NewFilCnsTipSetExecutor() *FilCnsTipSetExecutor {
	return &FilCnsTipSetExecutor{}
}

func (t *FilCnsTipSetExecutor) NewActorRegistry() *vm.ActorRegistry {
	return registry.NewActorRegistry()
}

type FilecoinBlockMessages struct {
	store.BlockMessages

	WinCount int64
}

func (t *FilCnsTipSetExecutor) ApplyBlocks(ctx context.Context, sm *stmgr.StateManager, cr *resolver.Resolver, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []FilecoinBlockMessages, epoch abi.ChainEpoch, r vm.Rand, em stmgr.ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet) (cid.Cid, cid.Cid, error) {
	done := metrics.Timer(ctx, metrics.VMApplyBlocksTotal)
	defer done()

	partDone := metrics.Timer(ctx, metrics.VMApplyEarly)
	defer func() {
		partDone()
	}()

	makeVmWithBaseStateAndEpoch := func(base cid.Cid, e abi.ChainEpoch) (*vm.VM, error) {
		vmopt := &vm.VMOpts{
			StateBase:      base,
			Epoch:          e,
			Rand:           r,
			Bstore:         sm.ChainStore().StateBlockstore(),
			Actors:         registry.NewActorRegistry(),
			Syscalls:       sm.Syscalls,
			CircSupplyCalc: sm.GetVMCirculatingSupply,
			NetworkVersion: sm.GetNetworkVersion(ctx, e),
			BaseFee:        baseFee,
			LookbackState:  stmgr.LookbackStateGetterForTipset(sm, ts),
		}

		return sm.VMConstructor()(ctx, vmopt)
	}

	runCron := func(vmCron *vm.VM, epoch abi.ChainEpoch) error {
		cronMsg := &types.Message{
			To:         cron.Address,
			From:       builtin.SystemActorAddr,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   build.BlockGasLimit * 10000, // Make super sure this is never too little
			Method:     cron.Methods.EpochTick,
			Params:     nil,
		}
		ret, err := vmCron.ApplyImplicitMessage(ctx, cronMsg)
		if err != nil {
			return xerrors.Errorf("running cron: %w", err)
		}

		if em != nil {
			if err := em.MessageApplied(ctx, ts, cronMsg.Cid(), cronMsg, ret, true); err != nil {
				return xerrors.Errorf("callback failed on cron message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return xerrors.Errorf("cron exit was non-zero: %d", ret.ExitCode)
		}

		return nil
	}

	for i := parentEpoch; i < epoch; i++ {
		var err error
		if i > parentEpoch {
			vmCron, err := makeVmWithBaseStateAndEpoch(pstate, i)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("making cron vm: %w", err)
			}

			// run cron for null rounds if any
			if err = runCron(vmCron, i); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("running cron: %w", err)
			}

			pstate, err = vmCron.Flush(ctx)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("flushing cron vm: %w", err)
			}
		}

		// handle state forks
		// XXX: The state tree
		pstate, err = sm.HandleStateForks(ctx, pstate, i, em, ts)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("error handling state forks: %w", err)
		}
	}

	partDone()
	partDone = metrics.Timer(ctx, metrics.VMApplyMessages)

	vmi, err := makeVmWithBaseStateAndEpoch(pstate, epoch)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
	}

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

		params, err := actors.SerializeParams(&reward.AwardBlockRewardParams{
			Miner:     b.Miner,
			Penalty:   penalty,
			GasReward: gasReward,
			WinCount:  b.WinCount,
		})
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to serialize award params: %w", err)
		}

		rwMsg := &types.Message{
			From:       builtin.SystemActorAddr,
			To:         reward.Address,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   1 << 30,
			Method:     reward.Methods.AwardBlockReward,
			Params:     params,
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

		// Sort cross-messages deterministically before applying them
		crossm, aerr := crossmsg.SortCrossMsgs(ctx, sm, cr, b.CrossMessages, ts)
		if aerr != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("error sorting cross-msgs: %w", aerr)
		}

		processedMsgs = make(map[cid.Cid]struct{})
		for _, m := range crossm {
			// m := crossm.VMMessage()
			// additional sanity-check to avoid processing a message
			// included in a block twice (although this is already checked
			// by SCA, and there are a few more additional checks, so this
			// may not be needed).
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			log.Infof("Executing cross message: %v", crossm)

			if err := crossmsg.ApplyCrossMsg(ctx, vmi, nil, em, m, ts); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("cross messsage application failed: %w", err)
			}
			processedMsgs[m.Cid()] = struct{}{}
		}
	}

	partDone()
	partDone = metrics.Timer(ctx, metrics.VMApplyCron)

	if err := runCron(vmi, epoch); err != nil {
		return cid.Cid{}, cid.Cid{}, err
	}

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

func (t *FilCnsTipSetExecutor) ExecuteTipSet(ctx context.Context, sm *stmgr.StateManager, cr *resolver.Resolver, ts *types.TipSet, em stmgr.ExecMonitor) (stateroot cid.Cid, rectsroot cid.Cid, err error) {
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
		parent, err := sm.ChainStore().GetBlock(ctx, blks[0].Parents[0])
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("getting parent block: %w", err)
		}

		parentEpoch = parent.Height
	}

	r := rand.NewStateRand(sm.ChainStore(), ts.Cids(), sm.Beacon(), sm.GetNetworkVersion)

	blkmsgs, err := sm.ChainStore().BlockMsgsForTipset(ctx, ts)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting block messages for tipset: %w", err)
	}
	fbmsgs := make([]FilecoinBlockMessages, len(blkmsgs))
	for i := range fbmsgs {
		fbmsgs[i].BlockMessages = blkmsgs[i]
		fbmsgs[i].WinCount = ts.Blocks()[i].ElectionProof.WinCount
	}
	baseFee := blks[0].ParentBaseFee

	return t.ApplyBlocks(ctx, sm, cr, parentEpoch, pstate, fbmsgs, blks[0].Height, r, em, baseFee, ts)
}

var _ stmgr.Executor = &FilCnsTipSetExecutor{}
