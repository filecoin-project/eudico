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

// getMessagesFromMirBlock retrieves Filecoin messages from a Mir block.
func getMessagesFromMirBlock(b []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	for _, tx := range b {
		msg, err := parseTx(tx)
		if err != nil {
			log.Error("unable to parse a Mir block tx:", err)
			continue
		}

		switch m := msg.(type) {
		case *types.SignedMessage:
			msgs = append(msgs, m)
		case *types.UnverifiedCrossMsg:
			crossMsgs = append(crossMsgs, m.Message)
		default:
			log.Error("received unknown tx in Mir block")
		}
	}
	return
}
