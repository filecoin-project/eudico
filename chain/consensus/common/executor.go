package common

import (
	"context"
	"sync/atomic"

	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/chain/consensus/actors/registry"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/rand"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opencensus.io/stats"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"

	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/metrics"
)

func DefaultUpgradeSchedule() stmgr.UpgradeSchedule {
	var us stmgr.UpgradeSchedule

	updates := []stmgr.Upgrade{{
		Height:    -1,
		Network:   network.Version14,
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

type tipSetExecutor struct {
	submgr subnet.SubnetMgr
	// To avoid having to get it from the state manager
	// for every message, we store this info here from the
	// beginning (this potentially never changes).
	netName dtypes.NetworkName
}

func (t *tipSetExecutor) NewActorRegistry() *vm.ActorRegistry {
	return registry.NewActorRegistry()
}

func TipSetExecutor(submgr subnet.SubnetMgr, netName dtypes.NetworkName) stmgr.Executor {
	return &tipSetExecutor{submgr, netName}
}

func RootTipSetExecutor() stmgr.Executor {
	return &tipSetExecutor{nil, dtypes.NetworkName(hierarchical.RootSubnet)}
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
			Actors:         registry.NewActorRegistry(),
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

		// TODO: This is the reward for a miner, we should maybe remove it
		// in subnets
		rwMsg := &types.Message{
			From:       reward.RewardActorAddr,
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

		processedMsgs = make(map[cid.Cid]struct{})
		for _, crossm := range b.CrossMessages {
			m := crossm.VMMessage()
			// additional sanity-check to avoid processing a message
			// included in a block twice (although this is already checked
			// by SCA, and there are a few more additional checks, so this
			// may not be needed).
			// TODO: We may need to sort nonces to avoid applying them in the
			// wrong order (in case they haven't been included in order in the
			// block)
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			log.Infof("Executing cross message: %v", crossm)
			if err := ApplyCrossMsg(ctx, vmi, t.submgr, em, m, ts, t.netName); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("cross messsage application failed: %w", err)
			}
			processedMsgs[m.Cid()] = struct{}{}
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

	r := rand.NewStateRand(sm.ChainStore(), ts.Cids(), nil)

	blkmsgs, err := sm.ChainStore().BlockMsgsForTipset(ts)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting block messages for tipset: %w", err)
	}

	baseFee := blks[0].ParentBaseFee

	return t.ApplyBlocks(ctx, sm, parentEpoch, pstate, blkmsgs, blks[0].Height, r, em, baseFee, ts)
}

/* Execute Fund Message
func ApplyFundMessage(to address.Address) {
	// Fund message
	testAddr, err := address.NewFromString("t1fqcjcfxhe634p25ts53ddvlmncbpvy5gae7pmsi")
	if err != nil {
		panic(err)
	}

		// fundMsg := &types.Message{
		//         From:       reward.Address,
		//         To:         testAddr,
		//         Nonce:      uint64(epoch),
		//         Value:      types.FromFil(10), // always reward 1 fil
		//         GasFeeCap:  types.NewInt(0),
		//         GasPremium: types.NewInt(0),
		//         GasLimit:   1 << 30,
		//         Method:     0,
		// }

	params, err := actors.SerializeParams(&reward.FundingParams{
		Addr:  testAddr,
		Value: types.FromFil(10000),
	})
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("failed to serialize award params: %w", err)
	}

	fundMsg := &types.Message{
		From:       builtin.SystemActorAddr,
		To:         reward.RewardActorAddr,
		Nonce:      uint64(epoch),
		Value:      types.NewInt(0),
		GasFeeCap:  types.NewInt(0),
		GasPremium: types.NewInt(0),
		GasLimit:   1 << 30,
		Method:     reward.Methods.ExternalFunding,
		Params:     params,
	}

	st := vmi.StateTree()
	toActor, err := st.GetActor(testAddr)
	fmt.Println(">>>> to actor", toActor, err)
	if err != nil {
		fmt.Println(">>>>> CREATE ACCOUNT ACTOR", toActor)
		fmt.Println(vmi.CreateAccountActor(ctx, fundMsg, testAddr))
	}
	ret, actErr := vmi.ApplyImplicitMessage(ctx, fundMsg)
	if actErr != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("failed to apply reward message for miner %s: %w", b.Miner, actErr)
	}
	if em != nil {
		if err := em.MessageApplied(ctx, ts, fundMsg.Cid(), fundMsg, ret, true); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("callback failed on reward message: %w", err)
		}
	}

	if ret.ExitCode != 0 {
		return cid.Undef, cid.Undef, xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
	}
	fmt.Println(">>>>>>>>>>> IMPLICIT FUND MESSAGE APPLIED", fundMsg.Cid(), ret)
}
*/

var _ stmgr.Executor = &tipSetExecutor{}
