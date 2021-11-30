package checkpointing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/Zondax/multi-party-sig/pkg/math/curve"
	"github.com/btcsuite/btcutil/bech32"
	"github.com/cronokirby/safenum"
)

func TaggedHash(tag string, datas ...[]byte) []byte {
	tagSum := sha256.Sum256([]byte(tag))

	h := sha256.New()
	h.Write(tagSum[:])
	h.Write(tagSum[:])
	for _, data := range datas {
		h.Write(data)
	}
	return h.Sum(nil)
}

func Sha256(data []byte) []byte {
	h := sha256.New()
	h.Write(data[:])
	return h.Sum(nil)
}

func TaprootSignatureHash(tx []byte, utxo []byte, hash_type byte) ([]byte, error) {
	if hash_type != 0x00 {
		return nil, errors.New("only support SIGHASH_DEFAULT (0x00)")
	}

	fmt.Println(hex.EncodeToString(tx))
	fmt.Println(hex.EncodeToString(utxo))

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
	ss = append(ss, Sha256(tx[5:5+36])...)

	// Amount in the previous output (8 bytes)
	ss = append(ss, Sha256(utxo[0:8])...)

	// PubScript in the previous output (35 bytes)
	ss = append(ss, Sha256(utxo[8:8+35])...)

	// Sequence (4 bytes)
	ss = append(ss, Sha256(tx[5+36+1:5+36+1+4])...)

	// Adding new txouts
	ss = append(ss, Sha256(tx[47:len(tx)-4])...)

	// spend type (here key path spending)
	ss = append(ss, 0x00)

	// Input index
	ss = append(ss, []byte{0, 0, 0, 0}...)

	return TaggedHash("TapSighash", ss), nil
}

func PubkeyToTapprootAddress(pubkey []byte) string {
	conv, err := bech32.ConvertBits(pubkey, 8, 5, true)
	if err != nil {
		fmt.Println("Error:", err)
		log.Fatal("I dunno.")
	}

	// Add segwit version byte 1
	conv = append([]byte{0x01}, conv...)

	// regtest human-readable part is "bcrt" according to no documentation ever... (see https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki)
	// Using EncodeM becasue we want bech32m... which has a new checksum
	taprootAddress, err := bech32.EncodeM("bcrt", conv)
	if err != nil {
		fmt.Println(err)
		log.Fatal("Couldn't produce our tapproot address.")
	}
	return taprootAddress
}

func PubkeyToTapprootAddressLegacy(pubkey []byte) string {
	conv, err := bech32.ConvertBits(pubkey, 8, 5, true)
	if err != nil {
		fmt.Println("Error:", err)
		log.Fatal("I dunno.")
	}
	// Add segwit version byte 1
	conv = append([]byte{0x01}, conv...)

	taprootAddressLegacy, err := bech32.Encode("bcrt", conv)
	if err != nil {
		fmt.Println(err)
		log.Fatal("Couldn't produce our tapproot address.")
	}
	return taprootAddressLegacy
}

func ApplyTweakToPublicKeyTaproot(public []byte, tweak []byte) []byte {
	group := curve.Secp256k1{}
	s_tweak := group.NewScalar().SetNat(new(safenum.Nat).SetBytes(tweak))
	p_tweak := s_tweak.ActOnBase()

	P, _ := curve.Secp256k1{}.LiftX(public)

	Y_tweak := P.Add(p_tweak)
	YSecp := Y_tweak.(*curve.Secp256k1Point)
	if !YSecp.HasEvenY() {
		s_tweak.Negate()
		p_tweak := s_tweak.ActOnBase()
		Y_tweak = P.Negate().Add(p_tweak)
		YSecp = Y_tweak.(*curve.Secp256k1Point)
	}
	PBytes := YSecp.XBytes()
	return PBytes
}

func HashMerkleRoot(pubkey []byte, checkpoint []byte) []byte {
	merkle_root := TaggedHash("TapLeaf", []byte{0xc0}, pubkey, checkpoint)
	return merkle_root[:]
}

func HashTweakedValue(pubkey []byte, merkle_root []byte) []byte {
	tweaked_value := TaggedHash("TapTweak", pubkey, merkle_root)
	return tweaked_value[:]
}

func GenCheckpointPublicKeyTaproot(internal_pubkey []byte, checkpoint []byte) []byte {
	merkle_root := HashMerkleRoot(internal_pubkey, checkpoint)
	tweaked_value := HashTweakedValue(internal_pubkey, merkle_root)

	tweaked_pubkey := ApplyTweakToPublicKeyTaproot(internal_pubkey, tweaked_value)
	return tweaked_pubkey
}

func AddTaprootScriptToWallet(taprootScript string) bool {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"importaddress\", \"params\": [\"" + taprootScript + "\", \"\", true]}"
	result := jsonRPC(payload)

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

func GetTaprootScript(pubkey []byte) string {
	return "5120" + hex.EncodeToString(pubkey)
}

// Temporary
func BitcoindGetWalletAddress() string {
	// Create wallet
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createwallet\", \"params\": [\"wow\"]}"
	_ = jsonRPC(payload)
	// We don't cehck error here

	//Get new address
	payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getnewaddress\", \"params\": []}"

	result := jsonRPC(payload)
	address := fmt.Sprintf("%v", result["result"])
	return address
}

func WalletGetTxidFromAddress(taprootAddress string) (string, error) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(payload)
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

func BitcoindPing() bool {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"ping\", \"params\": []}"
	result := jsonRPC(payload)
	return result != nil
}

func PrepareWitnessRawTransaction(rawtx string, sig []byte) string {
	wtx := rawtx[:4*2] + "00" + "01" + rawtx[4*2:len(rawtx)-4*2] + "01" + "40" + hex.EncodeToString(sig) + rawtx[len(rawtx)-4*2:]
	fmt.Println("Raw transaction signed :", wtx)

	return wtx
}

func jsonRPC(payload string) map[string]interface{} {
	// ZONDAX TODO
	// This needs to be in a config file
	url := "http://127.0.0.1:18443"
	method := "POST"

	user := "satoshi"
	password := "amiens"

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
