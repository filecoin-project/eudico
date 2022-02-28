package exec

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	bstore "github.com/filecoin-project/lotus/blockstore"
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
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	xerrors "golang.org/x/xerrors"
)

var log = logging.Logger("atomic")

func ComputeAtomicOutput(ctx context.Context, sm *stmgr.StateManager, to address.Address, actorState interface{}, locked []atomic.LockableState, msgs []*types.Message) (*api.InvocResult, error) {

	tmpbs := bstore.NewMemory()
	fmt.Println("Call compute state")
	ts := sm.ChainStore().GetHeaviestTipSet()
	// Search back till we find a height with no fork, or we reach the beginning.
	for ts.Height() > 0 {
		pts, err := sm.ChainStore().GetTipSetFromKey(ctx, ts.Parents())
		if err != nil {
			return nil, xerrors.Errorf("failed to find a non-forking epoch: %w", err)
		}
		ts = pts
	}

	pheight := ts.Height()
	bstate := ts.ParentState()
	tst, err := sm.StateTree(bstate)
	if err != nil {
		return nil, err
	}
	toActor, err := tst.GetActor(to)
	fmt.Println(">>>>>><< head", toActor.Head)
	if err != nil {
		return nil, xerrors.Errorf("call raw get actor: %s", err)
	}
	if err := vm.Copy(ctx, sm.ChainStore().StateBlockstore(), tmpbs, bstate); err != nil {
		return nil, err
	}
	if err := vm.Copy(ctx, sm.ChainStore().StateBlockstore(), tmpbs, toActor.Head); err != nil {
		return nil, err
	}

	vmopt := &vm.VMOpts{
		StateBase: bstate,
		Epoch:     pheight + 1,
		Rand:      rand.NewStateRand(sm.ChainStore(), ts.Cids(), sm.Beacon(), sm.GetNetworkVersion),
		Bstore:    sm.ChainStore().StateBlockstore(),
		// Bstore:         tmpbs,
		Actors:         registry.NewActorRegistry(),
		Syscalls:       sm.Syscalls,
		CircSupplyCalc: sm.GetCirculatingSupply,
		NetworkVersion: sm.GetNetworkVersion(ctx, pheight+1),
		BaseFee:        types.NewInt(0),
		LookbackState:  stmgr.LookbackStateGetterForTipset(sm, ts),
	}
	vmi, err := sm.VMConstructor()(ctx, vmopt)
	if err != nil {
		return nil, xerrors.Errorf("failed to set up vm: %w", err)
	}

	// Merge locked state to actor state.
	for _, l := range locked {
		mparams, err := atomic.WrapMergeParams(l)
		lmsg, err := mergeMsg(to, mparams)
		if err != nil {
			return nil, err
		}
		err = computeMsg(ctx, vmi, lmsg)
		if err != nil {
			return nil, xerrors.Errorf("error merging locked states", err)
		}
		fmt.Println(">>>>> Merged locked state")
	}

	fmt.Println("Messages: ", msgs)
	for _, m := range msgs {
		fmt.Println(">>>>> Computing for msg", m)
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

		fromActor, err := vmi.StateTree().GetActor(m.From)
		if err != nil {
			return nil, xerrors.Errorf("call raw get actor: %s", err)
		}

		m.Nonce = fromActor.Nonce
		err = computeMsg(ctx, vmi, m)
		if err != nil {
			return nil, xerrors.Errorf("error executing atomic msg", err)
		}
	}

	// _, err = vmi.Flush(ctx)
	// if err != nil {
	//         return nil, err
	// }
	toActor, err = vmi.StateTree().GetActor(to)
	if err != nil {
		return nil, xerrors.Errorf("call raw get actor: %s", err)
	}
	fmt.Println(">>>>>><< head", toActor.Head)
	bs := sm.ChainStore().ChainBlockstore()
	cst := cbor.NewCborStore(bs)
	if err := cst.Get(ctx, toActor.Head, actorState); err != nil {
		return nil, err
	}
	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>", actorState)

	// FIXME: Pending results
	return nil, nil
}

func computeMsg(ctx context.Context, vmi *vm.VM, m *types.Message) error {
	// TODO: maybe just use the invoker directly?
	ret, err := vmi.ApplyImplicitMessage(ctx, m)
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

func (sg *StateSurgeon) transplantActors(src *state.StateTree, pluck []address.Address) (*state.StateTree, error) {
	log.Printf("transplanting actor states: %v", pluck)

	dst, err := state.NewStateTree(sg.stores.CBORStore, src.Version())
	if err != nil {
		return nil, err
	}

	for _, a := range pluck {
		actor, err := src.GetActor(a)
		if err != nil {
			return nil, fmt.Errorf("get actor %s failed: %w", a, err)
		}

		err = dst.SetActor(a, actor)
		if err != nil {
			return nil, err
		}

		// recursive copy of the actor state.
		err = vm.Copy(context.TODO(), sg.stores.Blockstore, sg.stores.Blockstore, actor.Head)
		if err != nil {
			return nil, err
		}

		actorState, err := sg.api.ChainReadObj(sg.ctx, actor.Head)
		if err != nil {
			return nil, err
		}

		cid, err := sg.stores.CBORStore.Put(sg.ctx, &cbg.Deferred{Raw: actorState})
		if err != nil {
			return nil, err
		}

		if cid != actor.Head {
			panic("mismatched cids")
		}
	}

	return dst, nil
}
