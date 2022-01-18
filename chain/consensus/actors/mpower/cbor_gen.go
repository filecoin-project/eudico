package mpower

import (
	"fmt"
	"io"

	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf

var lengthBufState = []byte{130}

func (t *State) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufState); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.MinerCount (int64) (int64)
	if t.MinerCount >= 0 {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.MinerCount)); err != nil {
			return err
		}
	} else {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajNegativeInt, uint64(-t.MinerCount-1)); err != nil {
			return err
		}
	}

	// t.Miners ([]string) (slice)
	if len(t.Miners) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.Miners was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajArray, uint64(len(t.Miners))); err != nil {
		return err
	}
	for _, v := range t.Miners {
		if err := marshalCBORString(w, v); err != nil {
			return err
		}
	}

	return nil
}

func marshalCBORString(w io.Writer, s string) error {
	if err := cbg.WriteMajorTypeHeader(w, cbg.MajTextString, uint64(len(s))); err != nil {
		return err
	}

	if _, err := io.WriteString(w, s); err != nil {
		return err
	}

	return nil
}

func (t *State) UnmarshalCBOR(r io.Reader) error {
	*t = State{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.MinerCount (int64) (int64)
	{
		maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
		var extraI int64
		if err != nil {
			return err
		}
		switch maj {
		case cbg.MajUnsignedInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 positive overflow")
			}
		case cbg.MajNegativeInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 negative oveflow")
			}
			extraI = -1 - extraI
		default:
			return fmt.Errorf("wrong type for int64 field: %d", maj)
		}

		t.MinerCount = int64(extraI)
	}

	// t.Miners ([]string) (slice)
	maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("t.Miners: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}

	if extra > 0 {
		t.Miners = make([]string, extra)
	}

	for i := 0; i < int(extra); i++ {

		m, err := cbg.ReadString(br)
		if err != nil {
			return err
		}

		t.Miners[i] = m
	}

	return nil
}

var lengthBufAddMinerParams = []byte{129}

func (t *AddMinerParams) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufAddMinerParams); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.Miners ([]string) (slice)
	if len(t.Miners) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.Miners was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajArray, uint64(len(t.Miners))); err != nil {
		return err
	}
	for _, v := range t.Miners {
		if err := marshalCBORString(w, v); err != nil {
			return err
		}
	}

	return nil
}

func (t *AddMinerParams) UnmarshalCBOR(r io.Reader) error {
	*t = AddMinerParams{}

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

	// t.Miners ([]string) (slice)
	maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("t.Miners: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}

	if extra > 0 {
		t.Miners = make([]string, extra)
	}

	for i := 0; i < int(extra); i++ {

		m, err := cbg.ReadString(br)
		if err != nil {
			return err
		}

		t.Miners[i] = m
	}

	return nil
}
