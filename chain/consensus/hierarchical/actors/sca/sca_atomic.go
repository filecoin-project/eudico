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

// ExecStatus defines the different state an execution can be in
type ExecStatus uint64

const (
	// ExecInitialized and waiting for submissions.
	ExecInitialized ExecStatus = iota
	// ExecSuccess - all submissions matched.
	ExecSuccess
	// ExecAborted - some party aborted the execution
	ExecAborted
)

// AtomicExec is the data structure held by SCA for
// atomic executions.
type AtomicExec struct {
	Params AtomicExecParams
	Output map[string]cid.Cid
	Status ExecStatus
}

type SubmitExecParams struct {
	Cid    string // NOTE: Using Cid as string so it can be sed as input parameter
	Abort  bool
	Output atomic.LockedState
}

type SubmitOutput struct {
	Status ExecStatus
}

// AtomicExecParams determines the conditions (input, msgs) for the atomic
// execution.
//
// Parties involved in the protocol use this information to perform their
// off-chain execution stage.
type AtomicExecParams struct {
	Msgs   []types.Message
	Inputs map[string]LockedState
}

type LockedState struct {
	From address.SubnetID
	Cid  string // NOTE: Storing cid as string so it can be used as input parameter in actor fn.
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

// Cid computes the cid for the CrossMsg
func (ae *AtomicExecParams) Cid() (cid.Cid, error) {
	cst := cbor.NewCborStore(bstore.NewMemory())
	store := blockadt.WrapStore(context.TODO(), cst)
	cArr := blockadt.MakeEmptyArray(store)
	mArr := blockadt.MakeEmptyMap(store)

	// Compute CID for list of messages generated in subnet
	for i, m := range ae.Msgs {
		c := cbg.CborCid(m.Cid())
		if err := cArr.Set(uint64(i), &c); err != nil {
			return cid.Undef, err
		}
	}

	for _, input := range ae.Inputs {
		mc, err := abi.CidBuilder.Sum([]byte(input.From.String() + input.Cid))
		if err != nil {
			return cid.Undef, err
		}
		c := cbg.CborCid(mc)
		if err := mArr.Put(abi.CidKey(mc), &c); err != nil {
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
