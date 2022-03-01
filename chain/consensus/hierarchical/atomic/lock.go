package atomic

//go:generate go run ./gen/gen.go

import (
	"bytes"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/ipfs/go-cid"
	xerrors "golang.org/x/xerrors"
)

const (
	MethodLock   abi.MethodNum = 2
	MethodMerge  abi.MethodNum = 3
	MethodAbort  abi.MethodNum = 4
	MethodUnlock abi.MethodNum = 5
)

type Marshalable interface {
	cbor.Marshaler
	cbor.Unmarshaler
}

// LockableState defines the interface required for states
// that needs to be lockable.
type LockableState interface {
	Marshalable
	Merge(other LockableState) error
}

type LockedOutput struct {
	Cid cid.Cid
}

// LockableActor defines the interface that needs to be implemented by actors
// that want to support the atomic execution of some (or all) of their functions.
type LockableActor interface {
	// Lock defines how to lock the state in the actor.
	Lock(rt runtime.Runtime, params *LockParams) *LockedOutput
	// Merge takes external locked state and merges it to the current actors state.
	Merge(rt runtime.Runtime, params *MergeParams) *abi.EmptyValue
	// Unlock merges the output of an execution and unlocks the state.
	Unlock(rt runtime.Runtime, params *UnlockParams) *abi.EmptyValue
	// Abort unlocks the state and aborts the atomic execution.
	Abort(rt runtime.Runtime, params *LockParams) *abi.EmptyValue
}

// LockParams wraps serialized params from a message with the requested methodnum.
type LockParams struct {
	Method abi.MethodNum
	Params []byte
}

func WrapLockParams(m abi.MethodNum, params Marshalable) (*LockParams, error) {
	var buf bytes.Buffer
	if err := params.MarshalCBOR(&buf); err != nil {
		return nil, err
	}
	return &LockParams{m, buf.Bytes()}, nil
}

func WrapSerializedParams(m abi.MethodNum, params []byte) (*LockParams, error) {
	return &LockParams{m, params}, nil
}

func UnwrapLockParams(params *LockParams, out Marshalable) error {
	return out.UnmarshalCBOR(bytes.NewReader(params.Params))
}

// UnlockParams identifies the input params of a message
// along with the ouput state to merge.
type UnlockParams struct {
	Params *LockParams
	State  []byte
}

func WrapUnlockParams(params *LockParams, out LockableState) (*UnlockParams, error) {
	var buf bytes.Buffer
	if err := out.MarshalCBOR(&buf); err != nil {
		return nil, err
	}
	return &UnlockParams{params, buf.Bytes()}, nil
}

func UnwrapUnlockParams(params *UnlockParams, out LockableState) error {
	return out.UnmarshalCBOR(bytes.NewReader(params.State))
}

// MergeParams wraps locked state to merge in params.
type MergeParams struct {
	State []byte
}

func WrapMergeParams(out LockableState) (*MergeParams, error) {
	var buf bytes.Buffer
	if err := out.MarshalCBOR(&buf); err != nil {
		return nil, err
	}
	return &MergeParams{buf.Bytes()}, nil
}

func UnwrapMergeParams(params *MergeParams, out LockableState) error {
	return out.UnmarshalCBOR(bytes.NewReader(params.State))
}

// ValidateIfLocked checks if certain state in locked and thus can be
// modified.
func ValidateIfLocked(states ...*LockedState) error {
	for _, s := range states {
		if s.IsLocked() {
			return xerrors.Errorf("abort. One of the state or more are locked")
		}
	}
	return nil
}

// Cid to identify uniquely locked state.
func (s *LockedState) Cid() (cid.Cid, error) {
	var buf bytes.Buffer
	err := s.MarshalCBOR(&buf)
	if err != nil {
		return cid.Undef, err
	}
	return abi.CidBuilder.Sum(buf.Bytes())
}

// LockState locks the state from being written.
func (s *LockedState) LockState() error {
	if s.Lock {
		return xerrors.Errorf("state already locked")
	}
	s.Lock = true
	return nil
}

// UnlockState frees the lock.
func (s *LockedState) UnlockState() error {
	if !s.Lock {
		return xerrors.Errorf("state already unlocked")
	}
	s.Lock = false
	return nil
}

// LockedState includes a lock in some state.
type LockedState struct {
	Lock bool
	S    []byte
}

func WrapLockableState(s LockableState) (*LockedState, error) {
	var buf bytes.Buffer
	if err := s.MarshalCBOR(&buf); err != nil {
		return nil, err
	}
	return &LockedState{S: buf.Bytes()}, nil
}

func (l *LockedState) SetState(s LockableState) error {
	var buf bytes.Buffer
	if err := s.MarshalCBOR(&buf); err != nil {
		return err
	}
	l.S = buf.Bytes()
	return nil
}

func UnwrapLockableState(s *LockedState, out LockableState) error {
	return out.UnmarshalCBOR(bytes.NewReader(s.S))
}

func (s *LockedState) IsLocked() bool {
	return s.Lock
}

func CidFromOutput(s LockableState) (cid.Cid, error) {
	var buf bytes.Buffer
	err := s.MarshalCBOR(&buf)
	if err != nil {
		return cid.Undef, err
	}
	return abi.CidBuilder.Sum(buf.Bytes())
}
