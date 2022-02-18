package tendermint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/minio/blake2b-simd"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	tmsecp "github.com/tendermint/tendermint/crypto/secp256k1"
	"github.com/tendermint/tendermint/libs/rand"
	tmclient "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
	"golang.org/x/crypto/ripemd160"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types"
)

func NodeAddr() string {
	addr := os.Getenv(tendermintRPCAddressEnv)
	if addr == "" {
		return defaultTendermintRPCAddress
	}
	return addr
}

func parseTendermintBlock(b *tmtypes.Block, dst *tendermintBlockInfo, tag []byte) *tendermintBlockInfo {
	var msgs []*types.SignedMessage
	var crossMsgs []*types.Message

	for _, tx := range b.Txs {
		stx := tx.String()
		// Transactions from Tendermint are in the "Tx{....}" format.
		// So we have to remove T,x,{,} characters.
		txo := stx[3 : len(stx)-1]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			log.Error("unable to decode Tendermint tx:", err)
			continue
		}
		//data = {msg...|8 byte tag| type}
		inputTag := txoData[len(txoData)-(tagLength+1) : len(txoData)-1]
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
			log.Infof("found signed message from %s to %s with %s tokens", m.Message.From.String(), m.Message.To.String(), m.Message.Value)
			msgs = append(msgs, m)
		case *types.Message:
			log.Infof("found cross message from %s to %s with %s tokens", m.From.String(), m.To.String(), m.Value)
			crossMsgs = append(crossMsgs, m)
		default:
			log.Info("unknown message type")
		}
	}
	dst.messages = msgs
	dst.crossMsgs = crossMsgs

	return dst
}

func getMessageMapFromTendermintBlock(tb *tmtypes.Block) (map[[32]byte]bool, error) {
	msgs := make(map[[32]byte]bool)
	for _, msg := range tb.Txs {
		tx := msg.String()
		// Transactions from Tendermint are in the Tx{} format. So we have to remove T,x, { and } characters.
		// Then we have to remove last two fields that are message type and tag.
		txo := tx[3 : len(tx)-1-2-2*tagLength]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			return nil, err
		}
		id := blake2b.Sum256(txoData)
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
		msg, err = types.DecodeSignedMessage(tx[:ln-tagLength-1])
	case CrossMessageType:
		msg, err = types.DecodeMessage(tx[:ln-tagLength-1])
	case RegistrationMessageType:
		msg, err = DecodeRegistrationMessageRequest(tx[:ln-1])
	default:
		err = fmt.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, codeBadRequest, err
	}

	return msg, abci.CodeTypeOK, nil
}

func GetTendermintID(ctx context.Context) (address.Address, error) {
	client, err := tmclient.New(NodeAddr())
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

func findValidatorPubKeyByAddress(validators []*tmtypes.Validator, addr crypto.Address) []byte {
	for _, v := range validators {
		if bytes.Equal(v.Address.Bytes(), addr.Bytes()) {
			return v.PubKey.Bytes()
		}
	}
	return nil
}

func getTendermintAddress(pubKey []byte) []byte {
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

func getValidatorsInfo(ctx context.Context, c *tmclient.HTTP) (string, []byte, address.Address, error) {
	var resp *coretypes.ResultStatus
	var err error

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	shouldRetry := true
	for shouldRetry {
		select {
		case <-ticker.C:
			resp, err = c.Status(ctx)
			if err == nil {
				shouldRetry = false
			}
		case <-time.After(10 * time.Second):
			shouldRetry = false
			return "", nil, address.Address{}, xerrors.Errorf("unable to access Status method")
		}
	}

	validatorAddress := resp.ValidatorInfo.Address.String()

	keyType := resp.ValidatorInfo.PubKey.Type()
	if keyType != tmsecp.KeyType {
		return "", nil, address.Address{}, xerrors.Errorf("Tendermint validator uses unsupported key type: %s", keyType)
	}

	validatorPubKey := resp.ValidatorInfo.PubKey.Bytes()

	uncompressedValidatorPubKey, err := secp.ParsePubKey(validatorPubKey)
	if err != nil {
		return "", nil, address.Address{}, xerrors.Errorf("unable to parse pub key %w", err)
	}

	clientAddress, err := address.NewSecp256k1Address(uncompressedValidatorPubKey.SerializeUncompressed())
	if err != nil {
		return "", nil, address.Address{}, xerrors.Errorf("unable to calculate client address: %w", err)
	}

	return validatorAddress, validatorPubKey, clientAddress, nil
}

func registerNetwork(ctx context.Context, c *tmclient.HTTP, regReq []byte) (*RegistrationMessageResponse, error) {
	// TODO: explore whether we need to remove registration functionality or improve it
	// https://github.com/tendermint/tendermint/issues/7678
	// https://github.com/tendermint/tendermint/issues/3414

	regResp, err := c.BroadcastTxCommit(ctx, regReq)
	if err != nil {
		return nil, xerrors.Errorf("unable to broadcast registration request: %s", err)
	}

	regSubnetMsg, err := DecodeRegistrationMessageResponse(regResp.DeliverTx.Data)
	if err != nil {
		return nil, xerrors.Errorf("unable to decode registration response: %w", err)
	}
	return regSubnetMsg, nil
}

func registerNetworkNew(ctx context.Context, c *tmclient.HTTP, subnetID address.SubnetID, tag []byte) (*RegistrationMessageResponse, error) {
	var err error

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	try := true
	var resq *coretypes.ResultABCIQuery
	for try {
		select {
		case <-ctx.Done():
			log.Info("registering network was stopped")
			return nil, nil
		case <-ticker.C:
			regMsg, derr := NewRegistrationMessageBytes(subnetID, tag[:tagLength], rand.Bytes(16))
			if derr != nil {
				return nil, xerrors.Errorf("unable to create a registration message: %s", err)
			}

			_, err = c.BroadcastTxSync(ctx, regMsg)
			if err != nil {
				log.Infof("unable to broadcast a registration request: %s", err)
				continue
			}

			resq, err = c.ABCIQuery(ctx, "/reg", []byte(subnetID))
			if err != nil {
				log.Infof("unable to query Tendermint %s", err)
				continue
			}
			try = false
		case <-time.After(60 * time.Second):
			return nil, xerrors.New("time exceeded")
		}
	}

	regSubnetMsg, err := DecodeRegistrationMessageResponse(resq.Response.Value)
	if err != nil {
		return nil, xerrors.Errorf("unable to decode registration response: %w", err)
	}
	return regSubnetMsg, nil
}

func getFilecoinAddrByTendermintPubKey(pubKey []byte) (address.Address, error) {
	uncompressedProposerPubKey, err := secp.ParsePubKey(pubKey)

	eudicoAddress, err := address.NewSecp256k1Address(uncompressedProposerPubKey.SerializeUncompressed())
	if err != nil {
		log.Info("unable to create address in Filecoin format:", err)
		return address.Address{}, err
	}

	return eudicoAddress, nil
}
