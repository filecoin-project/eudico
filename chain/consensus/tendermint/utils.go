package tendermint

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ipfs/go-cid"
	abci "github.com/tendermint/tendermint/abci/types"
	tmcrypto "github.com/tendermint/tendermint/crypto"
	tmsecp "github.com/tendermint/tendermint/crypto/secp256k1"
	tmjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/libs/rand"
	tmclient "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	randBytesLen = 16
)

func NodeAddr() string {
	addr := os.Getenv(tendermintRPCAddressEnv)
	if addr == "" {
		return defaultTendermintRPCAddress
	}
	return addr
}

func getMessageMapFromTendermintBlock(tb *tmtypes.Block) (map[cid.Cid]bool, error) {
	msgs := make(map[cid.Cid]bool)
	for _, msg := range tb.Txs {
		tx := msg.String()
		// Transactions from Tendermint are in the Tx{} format. So we have to remove T,x, { and } characters.
		// Then we have to remove message type and ID hash length.
		txo := tx[3 : len(tx)-1]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			return nil, err
		}

		msg, _, err := parseTx(txoData)
		if err != nil {
			return nil, err
		}
		switch m := msg.(type) {
		case *types.Message:
			msgs[m.Cid()] = true
		case *types.SignedMessage:
			msgs[m.Cid()] = true
		default:

		}
	}
	return msgs, nil
}

func parseTx(tx []byte) (interface{}, uint32, error) {
	ln := len(tx)
	// This is very simple input validation to be protected against invalid messages.
	if ln <= 2 {
		return nil, http.StatusBadRequest, fmt.Errorf("tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	lastByte := tx[ln-1]
	switch lastByte {
	case common.SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1-32])
	case common.CrossMessageType:
		msg, err = types.DecodeMessage(tx[:ln-1-32])
	case common.RegistrationMessageType:
		msg, err = DecodeRegistrationMessageRequest(tx[:ln-1])
	default:
		err = fmt.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, http.StatusBadRequest, err
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

func findValidatorPubKeyByAddress(validators []*tmtypes.Validator, addr tmcrypto.Address) []byte {
	for _, v := range validators {
		if bytes.Equal(v.Address.Bytes(), addr.Bytes()) {
			return v.PubKey.Bytes()
		}
	}
	return nil
}

func getValidatorsInfo(ctx context.Context, c *tmclient.HTTP) (string, []byte, address.Address, error) {
	var resp *coretypes.ResultStatus
	var err error

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(10 * time.Second)

	shouldRetry := true
	for shouldRetry {
		select {
		case <-ctx.Done():
			log.Info("getting validators info was stopped")
			return "", nil, address.Address{}, xerrors.Errorf("time exceeded while accessing Status method")
		case <-ticker.C:
			resp, err = c.Status(ctx)
			if err == nil {
				shouldRetry = false
			}
		case <-timeout:
			return "", nil, address.Address{}, xerrors.Errorf("unable to access Status method")
		}
	}

	validatorAddress := resp.ValidatorInfo.Address.String()

	kt := resp.ValidatorInfo.PubKey.Type()
	if kt != tmsecp.KeyType {
		return "", nil, address.Address{}, xerrors.Errorf("Tendermint validator uses unsupported key: %s", kt)
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

// registerNetworkViaTxSync registers a new network using the BroadcastTxSync method.
func registerNetworkViaTxSync(
	ctx context.Context,
	c *tmclient.HTTP,
	subnetID address.SubnetID,
) (*RegistrationMessageResponse, error) {
	var err error

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)

	try := true
	var resultQuery *coretypes.ResultABCIQuery

	for try {
		select {
		case <-ctx.Done():
			log.Info("registering network was stopped")
			return nil, nil
		case <-ticker.C:
			regMsg, derr := NewRegistrationMessageBytes(subnetID, rand.Bytes(randBytesLen))
			if derr != nil {
				return nil, xerrors.Errorf("unable to create a registration message: %s", err)
			}

			resultBroadcast, berr := c.BroadcastTxSync(ctx, regMsg)
			if berr != nil {
				return nil, xerrors.Errorf("unable to broadcast a registration request: %s", err)
			}
			if resultBroadcast.Code == http.StatusConflict {
				return nil, xerrors.New(resultBroadcast.Log)
			}

			resultQuery, err = c.ABCIQuery(ctx, "/reg", []byte(subnetID))
			if err != nil {
				return nil, xerrors.Errorf("unable to query Tendermint %s", err)
			}
			if resultQuery.Response.Code == http.StatusAlreadyReported {
				return nil, xerrors.New(resultQuery.Response.Log)
			}
			if resultQuery.Response.Code != 0 {
				log.Info(resultQuery.Response.Log)
				continue
			}
			try = false
		case <-timeout:
			return nil, xerrors.New("registration network time exceeded")
		}
	}

	regSubnetMsg, err := DecodeRegistrationMessageResponse(resultQuery.Response.Value)
	if err != nil {
		return nil, xerrors.Errorf("unable to decode registration response: %w", err)
	}
	return regSubnetMsg, nil
}

func getFilecoinAddrFromTendermintPubKey(pubKey []byte) (address.Address, error) {
	uncompressedProposerPubKey, err := secp.ParsePubKey(pubKey)
	if err != nil {
		return address.Address{}, err
	}

	eudicoAddress, err := address.NewSecp256k1Address(uncompressedProposerPubKey.SerializeUncompressed())
	if err != nil {
		log.Info("unable to create address in Filecoin format:", err)
		return address.Address{}, err
	}

	return eudicoAddress, nil
}

func GetSecp256k1TendermintKey(keyFilePath string) (*types.KeyInfo, error) {
	var pvKey struct {
		Address tmtypes.Address  `json:"address"`
		PubKey  tmcrypto.PubKey  `json:"pub_key"`
		PrivKey tmcrypto.PrivKey `json:"priv_key"`
	}

	keyJSONBytes, err := ioutil.ReadFile(keyFilePath)
	if err != nil {
		return nil, err
	}

	err = tmjson.Unmarshal(keyJSONBytes, &pvKey)
	if err != nil {
		return nil, fmt.Errorf("error reading Tendermint private key from %v: %w", keyFilePath, err)
	}

	if pvKey.PrivKey.Type() != tmsecp.KeyType {
		return nil, fmt.Errorf("unsupported private key type %v", pvKey.PrivKey.Type())
	}

	ki := types.KeyInfo{
		Type:       types.KTSecp256k1,
		PrivateKey: pvKey.PrivKey.Bytes(),
	}

	return &ki, nil
}
