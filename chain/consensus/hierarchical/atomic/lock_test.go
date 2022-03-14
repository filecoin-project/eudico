package atomic_test

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/stretchr/testify/require"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

func TestMarshal(t *testing.T) {
	s := &SampleState{S: "something to test"}
	l, err := atomic.WrapLockableState(s)
	require.NoError(t, err)
	var buf bytes.Buffer

	err = l.LockState()
	require.NoError(t, err)
	err = l.MarshalCBOR(&buf)
	require.NoError(t, err)

	// Unmarshal and check equal
	l2, err := atomic.WrapLockableState(&SampleState{})
	require.NoError(t, err)
	err = l2.UnmarshalCBOR(&buf)
	require.NoError(t, err)
	require.True(t, reflect.DeepEqual(l2, l))
	sm := &SampleState{}
	err = atomic.UnwrapLockableState(l2, sm)
	require.NoError(t, err)
	require.Equal(t, s, sm)

	p, err := atomic.WrapLockParams(12, s)
	require.NoError(t, err)
	err = p.MarshalCBOR(&buf)
	require.NoError(t, err)

	// Unmarshal and check equal
	p2 := &atomic.LockParams{}
	err = p2.UnmarshalCBOR(&buf)
	require.NoError(t, err)
	require.Equal(t, p, p2)
	out := &SampleState{}
	err = atomic.UnwrapLockParams(p2, out)
	require.NoError(t, err)
	require.Equal(t, out, s)

	so := &SampleState{S: "some output"}
	u, err := atomic.WrapUnlockParams(p, so)
	require.NoError(t, err)
	err = u.MarshalCBOR(&buf)
	require.NoError(t, err)

	// Unmarshal and check equal
	u2 := &atomic.UnlockParams{}
	err = u2.UnmarshalCBOR(&buf)
	require.NoError(t, err)
	require.Equal(t, p, p2)
	err = atomic.UnwrapUnlockParams(u2, out)
	require.NoError(t, err)
	require.Equal(t, out, so)

	m, err := atomic.WrapMergeParams(so)
	require.NoError(t, err)
	err = m.MarshalCBOR(&buf)
	require.NoError(t, err)

	// Unmarshal and check equal
	m2 := &atomic.MergeParams{}
	err = m2.UnmarshalCBOR(&buf)
	require.NoError(t, err)
	err = atomic.UnwrapMergeParams(m2, out)
	require.NoError(t, err)
	require.Equal(t, out, so)
	// TODO: Marshal wrapping the wrong type.
}

func TestLock(t *testing.T) {
	s := &SampleState{S: "something to test"}
	l, err := atomic.WrapLockableState(s)
	require.NoError(t, err)
	err = l.LockState()
	require.NoError(t, err)
	err = l.LockState()
	require.Error(t, err)
	err = l.UnlockState()
	require.NoError(t, err)
	err = l.UnlockState()
	require.Error(t, err)
}

type SampleState struct {
	S string
}

var _ atomic.LockableState = &SampleState{}

var lengthBufSampleState = []byte{129}

func (t *SampleState) Merge(other atomic.LockableState, output bool) error {
	// Naïve merging with the other value.
	// It's up to the developer to chose the best way
	// to merge
	tt, ok := other.(*SampleState)
	if !ok {
		return xerrors.Errorf("type of LokableState not SampleState")
	}
	t.S = tt.S
	return nil
}

func (t *SampleState) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufSampleState); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.S (string) (string)
	if len(t.S) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.S was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajTextString, uint64(len(t.S))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.S)); err != nil {
		return err
	}
	return nil
}

func (t *SampleState) UnmarshalCBOR(r io.Reader) error {
	*t = SampleState{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 1 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.S (string) (string)

	{
		sval, err := cbg.ReadStringBuf(br, scratch)
		if err != nil {
			return err
		}

		t.S = string(sval)
	}
	return nil
}
