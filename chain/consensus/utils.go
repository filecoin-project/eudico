package consensus

import (
	"context"
	"errors"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/crypto"
)

var log = logging.Logger("consensus")

var ErrTemporal = errors.New("temporal error")

func verifyBlsAggregate(ctx context.Context, sig *crypto.Signature, msgs []cid.Cid, pubks [][]byte) error {
	_, span := trace.StartSpan(ctx, "syncer.verifyBlsAggregate")
	defer span.End()
	span.AddAttributes(
		trace.Int64Attribute("msgCount", int64(len(msgs))),
	)

	msgsS := make([]ffi.Message, len(msgs))
	pubksS := make([]ffi.PublicKey, len(msgs))
	for i := 0; i < len(msgs); i++ {
		msgsS[i] = msgs[i].Bytes()
		copy(pubksS[i][:], pubks[i][:ffi.PublicKeyBytes])
	}

	sigS := new(ffi.Signature)
	copy(sigS[:], sig.Data[:ffi.SignatureBytes])

	if len(msgs) == 0 {
		return nil
	}

	valid := ffi.HashVerify(sigS, msgsS, pubksS)
	if !valid {
		return xerrors.New("bls aggregate signature failed to verify")
	}
	return nil
}
