// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package checkpointing

import (
	"fmt"
	"io"
	"math"
	"sort"

	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = cid.Undef
var _ = math.E
var _ = sort.Sort

var lengthBufResolveMsg = []byte{131}

func (t *ResolveMsg) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufResolveMsg); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.Type (checkpointing.MsgType) (uint64)

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.Type)); err != nil {
		return err
	}

	// t.Cid (string) (string)
	if len(t.Cid) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.Cid was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajTextString, uint64(len(t.Cid))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.Cid)); err != nil {
		return err
	}

	// t.Content (checkpointing.MsgData) (struct)
	if err := t.Content.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *ResolveMsg) UnmarshalCBOR(r io.Reader) error {
	*t = ResolveMsg{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 3 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Type (checkpointing.MsgType) (uint64)

	{

		maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.Type = MsgType(extra)

	}
	// t.Cid (string) (string)

	{
		sval, err := cbg.ReadStringBuf(br, scratch)
		if err != nil {
			return err
		}

		t.Cid = string(sval)
	}
	// t.Content (checkpointing.MsgData) (struct)

	{

		if err := t.Content.UnmarshalCBOR(br); err != nil {
			return xerrors.Errorf("unmarshaling t.Content: %w", err)
		}

	}
	return nil
}

var lengthBufMsgData = []byte{128}

func (t *MsgData) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufMsgData); err != nil {
		return err
	}

	return nil
}

func (t *MsgData) UnmarshalCBOR(r io.Reader) error {
	*t = MsgData{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 0 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	return nil
}