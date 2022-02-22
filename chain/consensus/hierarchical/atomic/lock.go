package atomic

import (
	"bytes"
	"encoding/binary"
	"io"

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

type Marshaler interface {
	cbor.Marshaler
	cbor.Unmarshaler
}

// LockableState defines the interface required for states
// that needs to be lockable.
type LockableState interface {
	Marshaler
	Merge(other LockableState) error
}

type LockableActor interface {
	Lock(rt runtime.Runtime, params *LockParams) cid.Cid
	Merge(rt runtime.Runtime, params *UnlockParams) *abi.EmptyValue
	Abort(rt runtime.Runtime, params *LockParams) *abi.EmptyValue
}

type LockParams struct {
	Method abi.MethodNum
	Params Marshaler
}

func (l *LockParams) MarshalCBOR(w io.Writer) error {
	buf := make([]byte, binary.MaxVarintLen64)
	binary.LittleEndian.PutUint64(buf, uint64(l.Method))
	w.Write(buf)
	return l.Params.MarshalCBOR(w)
}

func (l *LockParams) UnmarshalCBOR(r io.Reader) error {
	buf := make([]byte, binary.MaxVarintLen64)
	_, err := r.Read(buf)
	if err != nil {
		return err
	}
	l.Method = abi.MethodNum(binary.LittleEndian.Uint64(buf))
	return l.Params.UnmarshalCBOR(r)
}

// WrapLockableState takes a lockableState and wraps it with
// its own lock ready to use.
func WrapLockableState(s LockableState) *LockedState {
	return &LockedState{S: s}
}

type UnlockParams struct {
	Params *LockParams
	Output LockableState
}

func NewUnlockParamsForTypes(params Marshaler, output LockableState) *UnlockParams {
	return &UnlockParams{
		Params: &LockParams{Params: params},
		Output: output,
	}
}

func WrapUnlockParams(m abi.MethodNum, params Marshaler, output LockableState) *UnlockParams {
	return &UnlockParams{Params: &LockParams{Method: m, Params: params}, Output: output}
}

func (u *UnlockParams) MarshalCBOR(w io.Writer) error {
	err := u.Params.MarshalCBOR(w)
	if err != nil {
		return err
	}
	return u.Output.MarshalCBOR(w)
}

func (u *UnlockParams) UnmarshalCBOR(r io.Reader) error {
	err := u.Params.UnmarshalCBOR(r)
	if err != nil {
		return err
	}
	return u.Output.UnmarshalCBOR(r)
}

func WrapLockParams(m abi.MethodNum, params Marshaler) *LockParams {
	return &LockParams{m, params}
}

func NewLockParamsForType(params Marshaler) *LockParams {
	return &LockParams{Params: params}
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

func (s *LockedState) IsLocked() bool {
	return s.Lock
}

type LockedState struct {
	Lock bool
	S    LockableState
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

func (t *LockedState) State() LockableState {
	return t.S
}

func (l *LockedState) MarshalCBOR(w io.Writer) error {
	buf := l.lockInt()
	w.Write(buf)
	return l.S.MarshalCBOR(w)
}

func (l *LockedState) UnmarshalCBOR(r io.Reader) error {
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err != nil {
		return err
	}
	l.lockFromByte(buf)
	return l.S.UnmarshalCBOR(r)
}

func CidFromOutput(s LockableState) (cid.Cid, error) {
	var buf bytes.Buffer
	err := s.MarshalCBOR(&buf)
	if err != nil {
		return cid.Undef, err
	}
	return abi.CidBuilder.Sum(buf.Bytes())
}
