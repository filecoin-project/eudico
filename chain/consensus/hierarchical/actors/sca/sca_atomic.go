package sca

import (
	"context"

	address "github.com/filecoin-project/go-address"
	abi "github.com/filecoin-project/go-state-types/abi"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	types "github.com/filecoin-project/lotus/chain/types"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

type ExecStatus uint64

const (
	Initialized ExecStatus = iota
	Success
	Aborted
)

type AtomicExec struct {
	Params AtomicExecParams
	Output map[string]cid.Cid
	Status ExecStatus
}

type SubmitExecParams struct {
	ID     cid.Cid
	Abort  bool
	Output cid.Cid
}

type SubmitOutput struct {
	Status ExecStatus
}

type AtomicExecParams struct {
	Msgs   []types.Message
	Inputs []LockedState
}

type LockedState struct {
	From  address.Address
	State atomic.LockedState
}

func (st *SCAState) putExec(execMap *adt.Map, exec *AtomicExec) (cid.Cid, error) {
	execCid, err := exec.Params.Cid()
	if err != nil {
		return cid.Undef, err
	}
	return execCid, st.putExecWithCid(execMap, execCid, exec)
}

func (st *SCAState) putExecWithCid(execMap *adt.Map, c cid.Cid, exec *AtomicExec) error {
	var err error
	if err := execMap.Put(abi.CidKey(c), exec); err != nil {
		return err
	}
	st.AtomicExecRegistry, err = execMap.Root()
	return err
}

func (st *SCAState) GetAtomicExec(s adt.Store, c cid.Cid) (*AtomicExec, bool, error) {
	execMap, err := adt.AsMap(s, st.AtomicExecRegistry, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load atomic exec: %w", err)
	}
	return getAtomicExec(execMap, c)
}

func getAtomicExec(execMap *adt.Map, c cid.Cid) (*AtomicExec, bool, error) {
	var out AtomicExec
	found, err := execMap.Get(abi.CidKey(c), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get execution for cid %v: %w", c, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

/*
func putExec(execMap *adt.Map, exec *AtomicExec) (cid.Cid, error) {
	execCid, err := exec.Params.Cid()
	if err != nil {
		return cid.Undef, err
	}
	return execCid, execMap.Put(abi.CidKey(execCid), exec)
}


func (st *SCAState) flushCheckpoint(rt runtime.Runtime, ch *schema.Checkpoint) {
	// Update subnet in the list of checkpoints.
	checks, err := adt.AsMap(adt.AsStore(rt), st.Checkpoints, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load state for checkpoints")
	err = checks.Put(abi.UIntKey(uint64(ch.Data.Epoch)), ch)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put checkpoint in map")
	// Flush checkpoints
	st.Checkpoints, err = checks.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush checkpoints")
}
*/

// Cid computes the cid for the CrossMsg
func (ae *AtomicExecParams) Cid() (cid.Cid, error) {
	cst := cbor.NewCborStore(bstore.NewMemory())
	store := blockadt.WrapStore(context.TODO(), cst)
	cArr := blockadt.MakeEmptyArray(store)
	mArr := blockadt.MakeEmptyArray(store)

	// Compute CID for list of messages generated in subnet
	for i, m := range ae.Msgs {
		c := cbg.CborCid(m.Cid())
		if err := cArr.Set(uint64(i), &c); err != nil {
			return cid.Undef, err
		}
	}

	for i, input := range ae.Inputs {
		mc, err := input.State.Cid()
		if err != nil {
			return cid.Undef, err
		}
		c := cbg.CborCid(mc)
		if err := mArr.Set(uint64(i), &c); err != nil {
			return cid.Undef, err
		}
	}

	croot, err := cArr.Root()
	if err != nil {
		return cid.Undef, err
	}
	mroot, err := mArr.Root()
	if err != nil {
		return cid.Undef, err
	}

	return store.Put(store.Context(), &MetaTag{
		MsgsCid:  croot,
		MetasCid: mroot,
	})
}
