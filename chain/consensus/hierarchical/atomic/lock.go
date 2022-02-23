package atomic

import (
	"bytes"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/ipfs/go-cid"
	xerrors "golang.org/x/xerrors"
)

const (
	MethodLock  abi.MethodNum = 2
	MethodMerge abi.MethodNum = 3
	MethodAbort abi.MethodNum = 4
)

// LockableState defines the interface required for states
// that needs to be lockable.
type LockableState interface {
	cbor.Marshaler
	cbor.Unmarshaler
	Merge(other LockableState) error
}

type LockedOutput struct {
	Cid cid.Cid
}

type LockableActor interface {
	Lock(rt runtime.Runtime, params *LockParams) *LockedOutput
	Merge(rt runtime.Runtime, params *UnlockParams) *abi.EmptyValue
	Abort(rt runtime.Runtime, params *LockParams) *abi.EmptyValue
}

type LockParams struct {
	Method abi.MethodNum
	Params []byte
}

func WrapLockParams(m abi.MethodNum, params LockableState) (*LockParams, error) {
	var buf bytes.Buffer
	if err := params.MarshalCBOR(&buf); err != nil {
		return nil, err
	}
	return &LockParams{m, buf.Bytes()}, nil
}

func UnwrapLockParams(params *LockParams, out LockableState) error {
	return out.UnmarshalCBOR(bytes.NewReader(params.Params))
}

type UnlockParams struct {
	Params *LockParams
	Output []byte
}

func WrapUnlockParams(params *LockParams, out LockableState) (*UnlockParams, error) {
	var buf bytes.Buffer
	if err := out.MarshalCBOR(&buf); err != nil {
		return nil, err
	}
	return &UnlockParams{params, buf.Bytes()}, nil
}

func UnwrapUnlockParams(params *UnlockParams, out LockableState) error {
	return out.UnmarshalCBOR(bytes.NewReader(params.Output))
}

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

func (t *LockedState) lockInt() []byte {
	if t.Lock {
		return []byte{1}
	}
	return []byte{0}
}

func (t *LockedState) lockFromByte(b []byte) {
	if b[0] == 1 {
		t.Lock = true
		return
	}
	t.Lock = false
}

func CidFromOutput(s LockableState) (cid.Cid, error) {
	var buf bytes.Buffer
	err := s.MarshalCBOR(&buf)
	if err != nil {
		return cid.Undef, err
	}
	return abi.CidBuilder.Sum(buf.Bytes())
}
