package replace

// Sample actor that replaces the cid from one address to the other.
// This actor is used as an example of how to use the actor execution
// protocol.

import (
	cid "github.com/ipfs/go-cid"
	xerrors "golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	actor "github.com/filecoin-project/lotus/chain/consensus/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/specs-actors/v3/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
)

//go:generate go run ./gen/gen.go

// example "Replace" actor that atomically replaces the cid from one owner
// to the other.

// LockableActors needs to register their lockableState so other protocols
// can use it in a generally way. We could probably remove this if we had
// generics.
func init() {
	atomic.RegisterState(ReplaceActor{}.Code(), ReplaceActor{}.StateInstance())
}

var CidUndef, _ = abi.CidBuilder.Sum([]byte("test"))

const (
	MethodReplace = 6
	MethodOwn     = 7
)

var _ atomic.LockableActor = ReplaceActor{}
var _ atomic.LockableActorState = &ReplaceState{}
var _ atomic.LockableState = &Owners{}

// ReplaceState determines the actor state.
// FIXME: We are using a non-efficient locking strategy for now
// where the whole map is locked for an atomic execution.
// We could use a more fine-grained approach, but this actor
// has illustrative purposes for now. Consider improving it in
// future iterations.
type ReplaceState struct {
	Owners    *atomic.LockedState
	LockedMap cid.Cid //HAMT[cid]LockedState
}

func (st *ReplaceState) LockedMapCid() cid.Cid {
	return st.LockedMap
}

func (st *ReplaceState) Output(params *atomic.LockParams) *atomic.LockedState {
	return st.Owners
}

type Owners struct {
	M map[string]cid.Cid
}

type ReplaceActor struct{}

func (a ReplaceActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		atomic.MethodLock:         a.Lock,
		atomic.MethodMerge:        a.Merge,
		atomic.MethodAbort:        a.Abort,
		atomic.MethodUnlock:       a.Unlock,
		MethodReplace:             a.Replace,
		MethodOwn:                 a.Own,
	}
}

func (a ReplaceActor) Code() cid.Cid {
	return actor.ReplaceActorCodeID
}

func (a ReplaceActor) IsSingleton() bool {
	return false
}

func (a ReplaceActor) StateInstance() atomic.LockableActorState {
	return &ReplaceState{}
}

func (a ReplaceActor) State() cbor.Er {
	return new(ReplaceState)
}

// Merge implements the merge strategy to follow for our lockable state
// in the actor, when we merge other locked state and/or the output.
func (o *Owners) Merge(other atomic.LockableState, output bool) error {
	tt, ok := other.(*Owners)
	if !ok {
		return xerrors.Errorf("type of LockableState not Owners")
	}

	for k, v := range tt.M {
		_, ok := o.M[k]
		// only consider conflicts when merging from other lockable state.
		if ok && !output {
			return xerrors.Errorf("merge conflict. key for owner already set")
		}
		o.M[k] = v
	}
	return nil
}

func ConstructState(store adt.Store) (*ReplaceState, error) {
	owners, err := atomic.WrapLockableState(&Owners{M: map[string]cid.Cid{}})
	if err != nil {
		return nil, err
	}
	emptyLockedMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}
	return &ReplaceState{Owners: owners, LockedMap: emptyLockedMapCid}, nil
}

func (a ReplaceActor) Constructor(rt runtime.Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerType(builtin.InitActorCodeID)
	st, err := ConstructState(adt.AsStore(rt))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error computing initial state")
	rt.StateCreate(st)
	return nil
}

type OwnParams struct {
	Seed string
}

func (a ReplaceActor) Own(rt runtime.Runtime, params *OwnParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()

	var st ReplaceState
	rt.StateTransaction(&st, func() {
		ValidateLockedState(rt, &st)
		own, err := st.UnwrapOwners()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error unwrapping lockable state")
		_, ok := own.M[rt.Caller().String()]
		if ok {
			rt.Abortf(exitcode.ErrIllegalState, "address already owning something")
		}
		own.M[rt.Caller().String()], err = abi.CidBuilder.Sum([]byte(params.Seed))
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error computing cid")
		st.storeOwners(rt, own)
	})

	return nil
}

type ReplaceParams struct {
	Addr address.Address
}

func (a ReplaceActor) Replace(rt runtime.Runtime, params *ReplaceParams) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()

	var st ReplaceState
	rt.StateTransaction(&st, func() {
		ValidateLockedState(rt, &st)
		own, err := st.UnwrapOwners()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error unwrapping lockable state")
		_, ok1 := own.M[rt.Caller().String()]
		if !ok1 {
			own.M[rt.Caller().String()] = CidUndef
		}
		_, ok2 := own.M[params.Addr.String()]
		if !ok2 {
			own.M[params.Addr.String()] = CidUndef
		}
		if !ok1 && !ok2 {
			rt.Abortf(exitcode.ErrIllegalState, "none of the parties involved have assets")
		}
		// Replace
		own.M[rt.Caller().String()], own.M[params.Addr.String()] =
			own.M[params.Addr.String()], own.M[rt.Caller().String()]
		st.storeOwners(rt, own)
	})

	return nil
}

///////
// Atomic function definitions.
//////
func (a ReplaceActor) Lock(rt runtime.Runtime, params *atomic.LockParams) *atomic.LockedOutput {
	// Anyone can lock the state
	rt.ValidateImmediateCallerAcceptAny()

	var (
		st  ReplaceState
		c   cid.Cid
		err error
	)
	rt.StateTransaction(&st, func() {
		switch params.Method {
		case MethodReplace:
			// first we lock
			builtin.RequireNoErr(rt, st.Owners.LockState(), exitcode.ErrIllegalArgument, "error locking state")
			c, err = st.Owners.Cid()
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error computing Cid for locked state")
			// then we put in map. We always put the locked state in the map
			err = st.putLocked(adt.AsStore(rt), c, st.Owners)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error putting locked state in map")
		default:
			rt.Abortf(exitcode.ErrIllegalArgument, "provided method doesn't support atomic execution. No need to lock")
		}
	})

	return &atomic.LockedOutput{Cid: c}
}

func (st *ReplaceState) unlock(rt runtime.Runtime) {
	// first we rm
	c, err := st.Owners.Cid()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalArgument, "error computing cid for locked state")
	err = st.rmLocked(adt.AsStore(rt), c)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error removing locked state from map")
	// then we unlock
	builtin.RequireNoErr(rt, st.Owners.UnlockState(), exitcode.ErrIllegalArgument, "error unlocking state")
}

func (a ReplaceActor) Merge(rt runtime.Runtime, params *atomic.MergeParams) *abi.EmptyValue {
	// Only system actor can trigger this function.
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)
	var st ReplaceState
	rt.StateTransaction(&st, func() {
		merge := &Owners{}
		err := atomic.UnwrapMergeParams(params, merge)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error unwrapping output from mergeParams")
		st.merge(rt, merge, false)
		// Merge is only called to perform the off-chain execution, it is safe to unlock
		// the state to be able to execute the message as it is done in a temporary blockstore
		if st.Owners.IsLocked() {
			builtin.RequireNoErr(rt, st.Owners.UnlockState(), exitcode.ErrIllegalArgument, "error unlocking state")
		}
	})

	return nil
}

func (a ReplaceActor) Unlock(rt runtime.Runtime, params *atomic.UnlockParams) *abi.EmptyValue {
	// called through a top-down transaction, so only the SCA can call it
	// through ApplyMsg
	rt.ValidateImmediateCallerIs(hierarchical.SubnetCoordActorAddr)

	var st ReplaceState
	rt.StateTransaction(&st, func() {
		output := &Owners{}
		err := atomic.UnwrapUnlockParams(params, output)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error unwrapping output from unlockParams")
		switch params.Params.Method {
		case MethodReplace:
			st.unlock(rt)
			st.merge(rt, output, true)
		default:
			rt.Abortf(exitcode.ErrIllegalArgument, "this method has nothing to merge")
		}
	})

	return nil
}

func (st *ReplaceState) merge(rt runtime.Runtime, state atomic.LockableState, output bool) {
	var owners Owners
	err := atomic.UnwrapLockableState(st.Owners, &owners)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error unwrapping owners")
	builtin.RequireNoErr(rt, owners.Merge(state, output), exitcode.ErrIllegalState, "error merging output")
	st.storeOwners(rt, &owners)
}

func (a ReplaceActor) Abort(rt runtime.Runtime, params *atomic.LockParams) *abi.EmptyValue {
	// abort is triggered both, by a top-down transaction, or can be called by a user
	// that locked some state by mistake.
	rt.ValidateImmediateCallerAcceptAny()
	var st ReplaceState
	rt.StateTransaction(&st, func() {
		switch params.Method {
		case MethodReplace:
			st.unlock(rt)
		default:
			rt.Abortf(exitcode.ErrIllegalArgument, "this method has nothing to unlock")
		}
	})

	return nil
}

func ValidateLockedState(rt runtime.Runtime, st *ReplaceState) {
	builtin.RequireNoErr(rt,
		atomic.ValidateIfLocked([]*atomic.LockedState{st.Owners}...),
		exitcode.ErrIllegalState, "state locked")
}

// UnwrapOwners is a convenient function to handle the locked state from the actor.
func (st *ReplaceState) UnwrapOwners() (*Owners, error) {
	var own Owners
	if err := atomic.UnwrapLockableState(st.Owners, &own); err != nil {
		return nil, err
	}
	return &own, nil
}

func (st *ReplaceState) storeOwners(rt runtime.Runtime, owners *Owners) {
	err := st.Owners.SetState(owners)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error wrapping lockable state")
}

func (st *ReplaceState) putLocked(s adt.Store, c cid.Cid, l *atomic.LockedState) error {
	lmap, err := adt.AsMap(s, st.LockedMap, builtin.DefaultHamtBitwidth)
	if err != nil {
		return xerrors.Errorf("failed to load : %w", err)
	}
	if err := lmap.Put(abi.CidKey(c), l); err != nil {
		return err
	}
	st.LockedMap, err = lmap.Root()
	return err
}

func (st *ReplaceState) rmLocked(s adt.Store, c cid.Cid) error {
	lmap, err := adt.AsMap(s, st.LockedMap, builtin.DefaultHamtBitwidth)
	if err != nil {
		return xerrors.Errorf("failed to load : %w", err)
	}
	if err := lmap.Delete(abi.CidKey(c)); err != nil {
		return err
	}
	st.LockedMap, err = lmap.Root()
	return err
}
