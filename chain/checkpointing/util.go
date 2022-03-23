package checkpointing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/Zondax/multi-party-sig/pkg/math/curve"
	"github.com/btcsuite/btcutil/bech32"
	"github.com/cronokirby/safenum"
)

type VerificationShare struct {
	Share string
}

// Define what share.toml should look like
type TaprootConfigTOML struct {
	Threshold          int
	PrivateShare       string
	PublicKey          string
	VerificationShares map[string]VerificationShare
}

// Bitcoin uses tagged hashed for taproot output definition
// see: https://en.bitcoin.it/wiki/BIP_0341
// e.g. the tag can be "TapTweak" when tweaking the key
// or "TapLeaf" or "TapBranch" when creating the output script
// of the taproot output, etc
func taggedHash(tag string, datas ...[]byte) []byte {
	tagSum := sha256.Sum256([]byte(tag))

	h := sha256.New()
	h.Write(tagSum[:])
	h.Write(tagSum[:])
	for _, data := range datas {
		h.Write(data)
	}
	return h.Sum(nil)
}

func sha256Util(data []byte) []byte {
	h := sha256.New()
	h.Write(data[:])
	return h.Sum(nil)
}

func TaprootSignatureHash(tx []byte, utxo []byte, hash_type byte) ([]byte, error) {
	if hash_type != 0x00 {
		return nil, errors.New("only support SIGHASH_DEFAULT (0x00)")
	}

	var ss []byte

	ext_flag := 0x00
	ss = append(ss, byte(ext_flag))

	// Epoch
	ss = append(ss, 0x00)

	// version (4 bytes)
	ss = append(ss, tx[:4]...)
	// locktime
	ss = append(ss, tx[len(tx)-4:]...)
	// Transaction level data
	// !IMPORTANT! This only work because we have 1 utxo.
	// Please check https://github.com/bitcoin/bips/blob/master/bip-0341.mediawiki#common-signature-message

	// Previous output (txid + index = 36 bytes)
	ss = append(ss, sha256Util(tx[5:5+36])...)

	// Amount in the previous output (8 bytes)
	ss = append(ss, sha256Util(utxo[0:8])...)

	// PubScript in the previous output (35 bytes)
	ss = append(ss, sha256Util(utxo[8:8+35])...)

	// Sequence (4 bytes)
	ss = append(ss, sha256Util(tx[5+36+1:5+36+1+4])...)

	// Adding new txouts
	ss = append(ss, sha256Util(tx[47:len(tx)-4])...)

	// spend type (here key path spending)
	ss = append(ss, 0x00)

	// Input index
	ss = append(ss, []byte{0, 0, 0, 0}...)

	return taggedHash("TapSighash", ss), nil
}

func pubkeyToTapprootAddress(pubkey []byte) (string, error) {
	conv, err := bech32.ConvertBits(pubkey, 8, 5, true)
	if err != nil {
		return "", err
	}

	// Add segwit version byte 1
	conv = append([]byte{0x01}, conv...)

	// regtest human-readable part is "bcrt" according to no documentation ever... (see https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki)
	// Using EncodeM becasue we want bech32m... which has a new checksum
	if Regtest {
		taprootAddress, err := bech32.EncodeM("bcrt", conv)
		if err != nil {
			return "", err
		}
		return taprootAddress, nil
	}
	// for testnet the human-readable part is "tb"
	taprootAddress, err := bech32.EncodeM("tb", conv)
	if err != nil {
		return "", err
	}
	return taprootAddress, nil
}

func applyTweakToPublicKeyTaproot(public []byte, tweak []byte) []byte {
	group := curve.Secp256k1{}
	s_tweak := group.NewScalar().SetNat(new(safenum.Nat).SetBytes(tweak))
	p_tweak := s_tweak.ActOnBase()

	P, _ := curve.Secp256k1{}.LiftX(public)

	Y_tweak := P.Add(p_tweak)
	YSecp := Y_tweak.(*curve.Secp256k1Point)
	// if the Y coordinate is odd, we need to "flip" its sign
	if !YSecp.HasEvenY() {
		s_tweak.Negate()
		p_tweak := s_tweak.ActOnBase()
		Y_tweak = P.Negate().Add(p_tweak)
		YSecp = Y_tweak.(*curve.Secp256k1Point)
	}
	PBytes := YSecp.XBytes()
	return PBytes
}

func hashMerkleRoot(pubkey []byte, checkpoint []byte) []byte {
	merkle_root := taggedHash("TapLeaf", []byte{0xc0}, pubkey, checkpoint)
	return merkle_root[:]
}

func hashTweakedValue(pubkey []byte, merkle_root []byte) []byte {
	tweaked_value := taggedHash("TapTweak", pubkey, merkle_root)
	return tweaked_value[:]
}

func genCheckpointPublicKeyTaproot(internal_pubkey []byte, checkpoint []byte) []byte {
	merkle_root := hashMerkleRoot(internal_pubkey, checkpoint)
	tweaked_value := hashTweakedValue(internal_pubkey, merkle_root)

	tweaked_pubkey := applyTweakToPublicKeyTaproot(internal_pubkey, tweaked_value)
	return tweaked_pubkey
}

func addTaprootToWallet(url, taprootScript string) bool {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"importaddress\", \"params\": [\"" + taprootScript + "\", \"\", false]}"
	if Regtest {
		payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"importaddress\", \"params\": [\"" + taprootScript + "\", \"\", true]}"
	}
	//time.Sleep(6 * time.Second)
	result := jsonRPC(url, payload)

	if result["error"] == nil {
		return true
	}

	err := result["error"].(map[string]interface{})
	if err["code"].(float64) == -4 {
		// Particular case where we are already in the process of adding the key
		// because we are using 1 bitcoin node for all
		return true
	}

	return false
}

func getTaprootScript(pubkey []byte) string {
	// 1 <20-size>  pubkey
	return "5120" + hex.EncodeToString(pubkey)
}

func walletGetTxidFromAddress(url, taprootAddress string) (string, error) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(url, payload)

	list := result["result"].([]interface{})
	for _, item := range list {
		item_map := item.(map[string]interface{})
		if item_map["address"] == taprootAddress {
			txid := item_map["txid"].(string)
			return txid, nil
		}
	}
	return "", errors.New("did not find checkpoint")
}

func bitcoindPing(url string) bool {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"ping\", \"params\": []}"
	result := jsonRPC(url, payload)
	return result != nil
}

func prepareWitnessRawTransaction(rawtx string, sig []byte) string {
	wtx := rawtx[:4*2] + "00" + "01" + rawtx[4*2:len(rawtx)-4*2] + "01" + "40" + hex.EncodeToString(sig) + rawtx[len(rawtx)-4*2:]

	return wtx
}

func parseUnspentTxOut(utxo []byte) (amount, script []byte) {
	return utxo[0:8], utxo[9:]
}

// return details about specified utxo
func getTxOut(url, txid string, index int) (float64, []byte) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"gettxout\", \"params\": [\"" + txid + "\", " + strconv.Itoa(index) + "]}"
	result := jsonRPC(url, payload)

	if result == nil {
		panic("Cannot retrieve previous transaction.")
	}
	if result["result"] == nil {
		panic("No transaction returned (maybe the output has already be spent")

	}
	taprootTxOut := result["result"].(map[string]interface{})
	scriptPubkey := taprootTxOut["scriptPubKey"].(map[string]interface{})
	scriptPubkeyBytes, _ := hex.DecodeString(scriptPubkey["hex"].(string))

	return taprootTxOut["value"].(float64), scriptPubkeyBytes
}

func jsonRPC(url, payload string) map[string]interface{} {
	// ZONDAX TODO
	// This needs to be in a config file
	method := "POST"
	user := "sarah"
	password := "pikachutestnetB2"
	if Regtest {
		user = "satoshi"
		password = "amiens"
	}

	client := &http.Client{}

	p := strings.NewReader(payload)
	req, err := http.NewRequest(method, url, p)

	if err != nil {
		fmt.Println(err)
		return nil
	}
	req.SetBasicAuth(user, password)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(body), &result)
	return result
}

func sameStringSlice(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	// create a map of string -> int
	diff := make(map[string]int, len(x))
	for _, _x := range x {
		// 0 value for int is 0, so just increment a counter for the string
		diff[_x]++
	}
	for _, _y := range y {
		// If the string _y is not in diff bail out early
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y] -= 1
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	return len(diff) == 0
}
