package sca

import (
	"context"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/types"
)

// ExecStatus defines the different state an execution can be in
type ExecStatus uint64

const (
	// UndefinedState status
	ExecUndefState ExecStatus = iota
	// ExecInitialized and waiting for submissions.
	ExecInitialized
	// ExecSuccess - all submissions matched.
	ExecSuccess
	// ExecAborted - some party aborted the execution
	ExecAborted
)

var ExecStatusStr = map[ExecStatus]string{
	ExecInitialized: "Initialized",
	ExecSuccess:     "Success",
	ExecAborted:     "Aborted",
}

type OutputCid struct {
	Cid string
}

// AtomicExec is the data structure held by SCA for
// atomic executions.
type AtomicExec struct {
	Params    AtomicExecParams
	Submitted map[string]OutputCid
	Status    ExecStatus
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

func (ae *AtomicExecParams) translateInputAddrs(rt runtime.Runtime) {
	aux := make(map[string]LockedState)
	for k := range ae.Inputs {
		addr, err := address.NewFromString(k)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error parsing addr from string")
		sn, err := addr.Subnet()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error parsing subnet")
		raw, err := addr.RawAddr()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error parsing raw address")
		outAddr := SecpBLSAddr(rt, raw)
		out, err := address.NewHCAddress(sn, outAddr)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error generating HAddress")
		aux[out.String()] = ae.Inputs[k]
	}
	ae.Inputs = aux
}

type LockedState struct {
	Cid   string // NOTE: Storing cid as string so it can be used as input parameter in actor fn.
	Actor address.Address
}

func (st *SCAState) putExecWithCid(execMap *adt.Map, c cid.Cid, exec *AtomicExec) error {
	var err error
	if err := execMap.Put(abi.CidKey(c), exec); err != nil {
		return err
	}
	st.AtomicExecRegistry, err = execMap.Root()
	return err
}

func execForRawAddr(m map[string]LockedState, target address.Address) (*LockedState, bool, error) {
	for k := range m {
		addr, err := address.NewFromString(k)
		if err != nil {
			return nil, false, err
		}
		raddr, err := addr.RawAddr()
		if err != nil {
			return nil, false, err
		}
		if raddr == target {
			out := m[k]
			return &out, true, nil
		}
	}
	return nil, false, nil
}

func sameRawAddr(inputs map[string]LockedState) (bool, error) {
	seen := map[address.Address]struct{}{}
	for k := range inputs {
		addr, err := address.NewFromString(k)
		if err != nil {
			return false, err
		}
		raddr, err := addr.RawAddr()
		if err != nil {
			return false, err
		}
		if _, ok := seen[raddr]; ok {
			return true, nil
		}
		seen[raddr] = struct{}{}
	}
	return false, nil
}

func isCommonParent(curr address.SubnetID, inputs map[string]LockedState) (bool, error) {
	cp, err := GetCommonParentForExec(inputs)
	return cp == curr, err
}

func GetCommonParentForExec(inputs map[string]LockedState) (address.SubnetID, error) {
	var cp address.SubnetID
	// Get first subnet to use as reference
	for k := range inputs {
		addr, err := address.NewFromString(k)
		if err != nil {
			return address.UndefSubnetID, err
		}
		cp, err = addr.Subnet()
		if err != nil {
			return address.UndefSubnetID, err
		}
		break
	}
	// Iterate through map
	for k := range inputs {
		addr, err := address.NewFromString(k)
		if err != nil {
			return address.UndefSubnetID, err
		}
		sub, err := addr.Subnet()
		if err != nil {
			return address.UndefSubnetID, err
		}
		cp, _ = cp.CommonParent(sub)
	}
	return cp, nil
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

func (st *SCAState) ListExecs(s adt.Store, addr address.Address) (map[cid.Cid]AtomicExec, error) {
	execMap, err := adt.AsMap(s, st.AtomicExecRegistry, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to load atomic exec: %w", err)
	}
	var ae AtomicExec
	out := make(map[cid.Cid]AtomicExec)
	err = execMap.ForEach(&ae, func(k string) error {
		_, has, err := execForRawAddr(ae.Params.Inputs, addr)
		if err != nil {
			return err
		}
		if has {
			_, c, err := cid.CidFromBytes([]byte(k))
			if err != nil {
				return err
			}
			out[c] = ae
		}
		return nil
	})
	return out, err
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

	for k, input := range ae.Inputs {
		mc, err := abi.CidBuilder.Sum([]byte(input.Cid + input.Actor.String()))
		if err != nil {
			return cid.Undef, err
		}
		c := cbg.CborCid(mc)
		addr, _ := address.NewFromString(k)
		if err := mArr.Put(abi.AddrKey(addr), &c); err != nil {
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

func (st *SCAState) propagateExecResult(rt runtime.Runtime, ae *AtomicExec, output atomic.LockedState, abort bool) {
	visited := map[address.SubnetID]struct{}{}
	for k, v := range ae.Params.Inputs {
		addr, err := address.NewFromString(k)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error parsing address")
		from, err := addr.Subnet()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting subnet from address")
		_, ok := visited[from]
		if ok {
			continue
		}
		// Send result of the execution as cross-msg
		st.sendCrossMsg(rt, st.execResultMsg(rt, from, v.Actor, ae.Params.Msgs[0], output, abort))
		visited[from] = struct{}{}
	}
}

func (st *SCAState) execResultMsg(rt runtime.Runtime, toSub address.SubnetID, toActor address.Address, msg types.Message, output atomic.LockedState, abort bool) types.Message {
	source := builtin.SystemActorAddr

	// to actor address responsible for execution
	to, err := address.NewHCAddress(toSub, toActor)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")
	from, err := address.NewHCAddress(st.NetworkName, source)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")

	// lock params
	lparams, err := atomic.WrapSerializedParams(msg.Method, msg.Params)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error wrapping serialized lock params")
	var (
		method abi.MethodNum
		enc    []byte
	)
	if abort {
		method = atomic.MethodAbort
		enc, err = actors.SerializeParams(lparams)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")
	} else {
		method = atomic.MethodUnlock
		uparams, err := atomic.WrapSerializedUnlockParams(lparams, output.S)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error wrapping merge params")
		enc, err = actors.SerializeParams(uparams)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")
	}

	// Build message.
	return types.Message{
		To:         to,
		From:       from,
		Value:      big.Zero(),
		Nonce:      st.Nonce,
		Method:     method,
		GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
		GasFeeCap:  types.NewInt(0),
		GasPremium: types.NewInt(0),
		Params:     enc,
	}
}
