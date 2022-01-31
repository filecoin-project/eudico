package tendermint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/filecoin-project/go-address"
	abci "github.com/tendermint/tendermint/abci/types"
	httptendermintrpcclient "github.com/tendermint/tendermint/rpc/client/http"
	tenderminttypes "github.com/tendermint/tendermint/types"

	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
)

func NodeAddr() string {
	addr := os.Getenv("TENDERMINT_NODE_ADDR")
	if addr == "" {
		return Sidecar
	}
	return addr
}

func parseTendermintBlock(b *tenderminttypes.Block, dst *tendermintBlockInfo) {
	var msgs []*types.SignedMessage
	var crossMsgs []*types.Message

	for _, tx := range b.Txs {
		stx := tx.String()
		// Transactions from Tendermint are in the Tx{} format.
		txo := stx[3 : len(stx)-1]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			log.Error("unable to decode Tendermint messages:", err)
			continue
		}
		msg, _, err := parseTx(txoData)
		if err != nil {
			log.Error("unable to decode a message from Tendermint block:", err)
			continue
		}
		log.Info("received Tx:", msg)

		switch m := msg.(type) {
		case *types.SignedMessage:
			msgs = append(msgs, m)
		case *types.Message:
			crossMsgs = append(crossMsgs, m)
		default:
			log.Info("unknown message type")
		}
	}
	dst.messages = msgs
	dst.crossMsgs = crossMsgs
}

func signBlock(b *types.FullBlock, h []byte) error {
	b.Header.BlockSig = &crypto.Signature{
		//TODO: use this incorrect type to not modify "crypto/signature" upstream
		Type: crypto.SigTypeSecp256k1,
		Data: h,
	}
	return nil
}


func getMessageMapFromTendermintBlock(tb *tenderminttypes.Block) (map[[32]byte]bool, error) {
	msgs := make(map[[32]byte]bool)
	for _, msg := range tb.Txs {
		tx := msg.String()
		// Transactions from Tendermint are in the Tx{} format. So we have to remove T,x, { and } characters.
		// Then we have to remove last two characters that are message type.
		txo := tx[3 : len(tx)-3]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			return nil, err
		}
		id := sha256.Sum256(txoData)
		msgs[id] = true
	}
	return msgs, nil
}

func parseTx(tx []byte) (interface{}, uint32, error) {
	ln := len(tx)
	if ln <= 2 {
		return nil, codeBadRequest, fmt.Errorf("tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	lastByte := tx[ln-1]
	switch lastByte {
	case SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case CrossMessageType:
		msg, err = types.DecodeMessage(tx[:ln-1])
	case RegistrationMessageType:
		msg, err = DecodeRegistrationMessage(tx[:ln-1])
	default:
		err = fmt.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, codeBadRequest, err
	}

	return msg, abci.CodeTypeOK, nil
}

func GetTendermintID(ctx context.Context) (address.Address, error) {
	client, err := httptendermintrpcclient.New(Sidecar)
	if err != nil {
		panic("unable to access a tendermint client")
	}
	info, err := client.Status(ctx)
	if err != nil {
		panic(err)
	}
	id := string(info.NodeInfo.NodeID)
	addr, err := address.NewFromString(id)
	if err != nil {
		panic(err)
	}
	return addr, nil
}
