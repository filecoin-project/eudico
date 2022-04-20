package mir

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

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

func getMessagesFromMirBlock(b []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	for _, tx := range b {
		msg, err := parseTx(tx)
		if err != nil {
			log.Error("unable to parse a message from Mir block:", err)
			continue
		}

		switch m := msg.(type) {
		case *types.SignedMessage:
			msgs = append(msgs, m)
		case *types.UnverifiedCrossMsg:
			crossMsgs = append(crossMsgs, m.Msg)
		default:
			log.Error("received an unknown message in Mir block")
		}
	}
	return
}
