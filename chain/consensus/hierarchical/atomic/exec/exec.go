package exec

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/actors/registry"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/rand"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
)

var log = logging.Logger("atomic-exec")

// ComputeAtomicOutput receives as input a list of locked states from other subnets, and a list of
// messages to execute atomically in an actor, and output the final state for the actor after the execution
// in actorState. This output needs to be committed to the SCA in the parent chain to finalize the execution.
func ComputeAtomicOutput(ctx context.Context, sm *stmgr.StateManager, ts *types.TipSet, to address.Address, locked []atomic.LockableState, msgs []types.Message) (*atomic.LockedState, error) {
	log.Info("triggering off-chain execution for locked state")
	// Search back till we find a height with no fork, or we reach the beginning.
	// for ts.Height() > 0 {
	//         pts, err := sm.ChainStore().GetTipSetFromKey(ctx, ts.Parents())
	//         if err != nil {
	//                 return xerrors.Errorf("failed to find a non-forking epoch: %w", err)
	//         }
	//         ts = pts
	// }

	// Get base state parameters
	pheight := ts.Height()
	bstate := ts.ParentState()
	tst, err := sm.StateTree(bstate)
	if err != nil {
		return nil, err
	}

	log.Debugf("Computing off-chain in height: %v", ts.Height())
	// transplant actor state and state tree to temporary blockstore for off-chain computation
	tmpbs, err := tmpState(ctx, sm.ChainStore().StateBlockstore(), tst, []address.Address{to})
	if err != nil {
		return nil, err
	}
	if err := vm.Copy(ctx, sm.ChainStore().StateBlockstore(), tmpbs, bstate); err != nil {
		return nil, err
	}

	// Since we're simulating a future message, pretend we're applying it in the "next" tipset
	vmHeight := pheight + 1

	filVested, err := sm.GetFilVested(ctx, vmHeight)
	if err != nil {
		return nil, err
	}

	vmopt := &vm.VMOpts{
		StateBase: bstate,
		Epoch:     vmHeight,
		Rand:      rand.NewStateRand(sm.ChainStore(), ts.Cids(), sm.Beacon(), sm.GetNetworkVersion),
		// Bstore:    sm.ChainStore().StateBlockstore(),
		Bstore:         tmpbs,
		Actors:         registry.NewActorRegistry(),
		Syscalls:       sm.Syscalls,
		CircSupplyCalc: sm.GetCirculatingSupply,
		NetworkVersion: sm.GetNetworkVersion(ctx, pheight+1),
		BaseFee:        types.NewInt(0),
		LookbackState:  stmgr.LookbackStateGetterForTipset(sm, ts),
		FilVested:      filVested,
	}
	vmi, err := sm.VMConstructor()(ctx, vmopt)
	if err != nil {
		return nil, xerrors.Errorf("failed to set up vm: %w", err)
	}

	// Merge locked state to actor state.
	for _, l := range locked {
		mparams, err := atomic.WrapMergeParams(l)
		if err != nil {
			return nil, xerrors.Errorf("error wrapping merge params: %w", err)
		}
		log.Debugf("Merging locked state for actor: %s", to)
		lmsg, err := mergeMsg(to, mparams)
		if err != nil {
			return nil, xerrors.Errorf("error creating merge msg: %w", err)
		}
		// Ensure that we are targeting the right actor.
		lmsg.To = to
		log.Debugf("Computing merge message for actor: %s", lmsg.To)
		err = computeMsg(ctx, vmi, *lmsg)
		if err != nil {
			return nil, xerrors.Errorf("error merging locked states: %w", err)
		}
	}

	// execute messages
	for _, m := range msgs {
		if m.GasLimit == 0 {
			m.GasLimit = build.BlockGasLimit
		}
		if m.GasFeeCap == types.EmptyInt {
			m.GasFeeCap = types.NewInt(0)
		}
		if m.GasPremium == types.EmptyInt {
			m.GasPremium = types.NewInt(0)
		}

		if m.Value == types.EmptyInt {
			m.Value = types.NewInt(0)
		}

		// fromActor, err := vmi.StateTree().GetActor(m.From)
		// if err != nil {
		//         return xerrors.Errorf("call raw get actor: %w", err)
		// }

		m.Nonce = 0
		// Ensure that we are targeting the right actor.
		m.To = to
		err = computeMsg(ctx, vmi, m)
		if err != nil {
			return nil, xerrors.Errorf("error executing atomic msg: %w", err)
		}
	}

	// flush state to process it.
	stroot, err := vmi.Flush(ctx)
	if err != nil {
		return nil, err
	}

	cst := cbor.NewCborStore(tmpbs)
	stTree, err := state.LoadStateTree(cst, stroot)
	if err != nil {
		return nil, xerrors.Errorf("failed to load state tree: %w", err)
	}

	toActor, err := stTree.GetActor(to)
	st, ok := atomic.StateRegistry[toActor.Code].(atomic.LockableActorState)
	if !ok {
		return nil, xerrors.Errorf("state from actor not of lockable state type")
	}
	if err := cst.Get(ctx, toActor.Head, st); err != nil {
		return nil, err
	}

	// FIXME: We are using here the method and params from the first message.
	// This works because we are currently using a really simple LockableActor,
	// this will need to be further generalized when we introduce new actors.
	lparams, err := atomic.WrapSerializedParams(msgs[0].Method, msgs[0].Params)
	if err != nil {
		return nil, err
	}
	return st.Output(lparams), nil
}

func computeMsg(ctx context.Context, vmi vm.Interface, m types.Message) error {
	// apply msg implicitly to execute new state
	ret, err := vmi.ApplyImplicitMessage(ctx, &m)
	if err != nil {
		return xerrors.Errorf("apply message failed: %w", err)
	}

	if err := ret.ActorErr; err != nil {
		return err
	}
	return nil
}

func mergeMsg(to address.Address, mparams *atomic.MergeParams) (*types.Message, error) {
	enc, err := actors.SerializeParams(mparams)
	if err != nil {
		return nil, err
	}
	m := &types.Message{
		From:   builtin.SystemActorAddr,
		To:     to,
		Value:  abi.NewTokenAmount(0),
		Method: atomic.MethodMerge,
		Params: enc,
	}
	m.GasLimit = build.BlockGasLimit
	return m, nil
}

// tmpState creates a temporary blockstore with all the state required to perform
// the off-chain execution.
func tmpState(ctx context.Context, frombs blockstore.Blockstore, src *state.StateTree, pluck []address.Address) (blockstore.Blockstore, error) {

	tmpbs := blockstore.NewMemory()
	cstore := cbor.NewCborStore(tmpbs)
	dst, err := state.NewStateTree(cstore, src.Version())
	if err != nil {
		return nil, err
	}
	for _, a := range pluck {
		actor, err := src.GetActor(a)
		if err != nil {
			return nil, xerrors.Errorf("get actor %s failed: %w", a, err)
		}

		err = dst.SetActor(a, actor)
		if err != nil {
			return nil, err
		}

		// recursive copy of the actor state.
		err = vm.Copy(context.TODO(), frombs, tmpbs, actor.Head)
		if err != nil {
			return nil, err
		}

		actorState, err := chainReadObj(ctx, frombs, actor.Head)
		if err != nil {
			return nil, err
		}

		cid, err := cstore.Put(ctx, &cbg.Deferred{Raw: actorState})
		if err != nil {
			return nil, err
		}

		if cid != actor.Head {
			return nil, xerrors.Errorf("mismatch in head cid after actor transplant")
		}
	}

	return tmpbs, nil
}

func chainReadObj(ctx context.Context, bs blockstore.Blockstore, obj cid.Cid) ([]byte, error) {
	blk, err := bs.Get(ctx, obj)
	if err != nil {
		return nil, xerrors.Errorf("blockstore get: %w", err)
	}

	return blk.RawData(), nil
}
