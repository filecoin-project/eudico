package mir

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

// parseTx parses a raw byte transaction from Mir node into a Filecoin message.
func parseTx(tx []byte) (msg interface{}, err error) {
	ln := len(tx)
	if ln <= 2 {
		return nil, xerrors.Errorf("tx len %d is too small", ln)
	}

	lastByte := tx[ln-1]
	switch lastByte {
	case common.SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case common.CrossMessageType:
		msg, err = types.DecodeUnverifiedCrossMessage(tx[:ln-1])
	default:
		err = xerrors.Errorf("unknown message type %d", lastByte)
	}

	return
}
