package tendermint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"golang.org/x/crypto/ripemd160"
	"os"

	"github.com/filecoin-project/go-address"
	abci "github.com/tendermint/tendermint/abci/types"
	httptendermintrpcclient "github.com/tendermint/tendermint/rpc/client/http"
	tenderminttypes "github.com/tendermint/tendermint/types"

	"github.com/filecoin-project/lotus/chain/types"
)

func NodeAddr() string {
	addr := os.Getenv("TENDERMINT_NODE_ADDR")
	if addr == "" {
		return Sidecar
	}
	return addr
}

func parseTendermintBlock(b *tenderminttypes.Block, dst *tendermintBlockInfo, tag []byte) {
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
		//data = {msg...|tag1-tag2-tag3-tag4|type}
		inputTag := txoData[len(txoData)-5:len(txoData)-1]
		if !bytes.Equal(inputTag, tag) {
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
			log.Info("received Tx is signed messages from %s to %s", m.Message.From.String(), m.Message.To.String())
			msgs = append(msgs, m)
		case *types.Message:
			log.Info("received Tx is cross messages from %s to %s in %s", m.From.String(), m.To.String())
			crossMsgs = append(crossMsgs, m)
		default:
			log.Info("unknown message type")
		}
	}
	dst.messages = msgs
	dst.crossMsgs = crossMsgs
}

func getMessageMapFromTendermintBlock(tb *tenderminttypes.Block) (map[[32]byte]bool, error) {
	msgs := make(map[[32]byte]bool)
	for _, msg := range tb.Txs {
		tx := msg.String()
		// Transactions from Tendermint are in the Tx{} format. So we have to remove T,x, { and } characters.
		// Then we have to remove last two characters that are message type.
		txo := tx[3 : len(tx)-3-8]
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
	//TODO: add tag length?
	if ln <= 2 {
		return nil, codeBadRequest, fmt.Errorf("tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	lastByte := tx[ln-1]
	switch lastByte {
	case SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-5])
	case CrossMessageType:
		msg, err = types.DecodeMessage(tx[:ln-5])
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

func findValidatorPubKeyByAddr(validators []*tenderminttypes.Validator, addr []byte) []byte {
	for _, v := range validators {
		if bytes.Equal(v.Address.Bytes(), addr) {
			return v.PubKey.Bytes()
		}
	}
	return nil
}

func getTendermintAddr(pubKey []byte) []byte {
	if len(pubKey) != 33 {
		panic("length of pubkey is incorrect")
	}
	hasherSHA256 := sha256.New()
	_, _ = hasherSHA256.Write(pubKey) // does not error
	sha := hasherSHA256.Sum(nil)

	hasherRIPEMD160 := ripemd160.New()
	_, _ = hasherRIPEMD160.Write(sha) // does not error
	return hasherRIPEMD160.Sum(nil)
}

