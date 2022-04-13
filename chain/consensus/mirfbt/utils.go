package mirbft

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

func parseTx(tx []byte) (interface{}, error) {
	ln := len(tx)
	if ln <= 2 {
		return nil, xerrors.Errorf("tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	lastByte := tx[ln-1]
	switch lastByte {
	case common.SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case common.CrossMessageType:
		msg, err = types.DecodeMessage(tx[:ln-1])
	default:
		err = xerrors.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, err
	}

	return msg, nil
}
