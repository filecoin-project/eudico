package checkpointing

import (
	"errors"
)

type BitcoinTx struct {
	txid  string
	value string
}

type Checkpoint struct {
	txid    string
	address string
	cid     string
}

func GetFirstCheckpointAddress(url, taprootAddress string) (Checkpoint, error) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(url, payload)
	list := result["result"].([]interface{})
	for _, item := range list {
		item_map := item.(map[string]interface{})
		if item_map["address"] == taprootAddress {
			tx_id := item_map["txid"].(string)
			payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getrawtransaction\", \"params\": [\"" + tx_id + "\", true]}"
			result = jsonRPC(url, payload)
			reader := result["result"].(map[string]interface{})

			vout := reader["vout"].([]interface{})
			taprootOut := vout[0].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			new_address := taprootOut["hex"].(string)

			cidOut := vout[1].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			cid := cidOut["hex"].(string)
			return Checkpoint{txid: tx_id, address: new_address, cid: cid[4:]}, nil
		}
	}
	return Checkpoint{}, errors.New("Did not find checkpoint")
}

func GetNextCheckpointFixed(url, txid string) (Checkpoint, error) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(url, payload)
	list := result["result"].([]interface{})
	for _, item := range list {
		item_map := item.(map[string]interface{})
		tx_id := item_map["txid"].(string)
		payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getrawtransaction\", \"params\": [\"" + tx_id + "\", true]}"
		result = jsonRPC(url, payload)
		reader := result["result"].(map[string]interface{})
		new_txid := reader["txid"].(string)
		vin := reader["vin"].([]interface{})[0].(map[string]interface{})["txid"]
		if vin == nil {
			continue
		}
		if vin.(string) == txid {
			vout := reader["vout"].([]interface{})
			taprootOut := vout[0].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			new_address := taprootOut["hex"].(string)
			cidOut := vout[1].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			cid := cidOut["hex"].(string)
			return Checkpoint{txid: new_txid, address: new_address, cid: cid[4:]}, nil
		}
	}
	return Checkpoint{}, errors.New("Did not find checkpoint")
}

func GetLatestCheckpoint(url string, first_pk []byte, first_cp []byte) (*Checkpoint, error) {
	first_pubkeyTaproot := GenCheckpointPublicKeyTaproot(first_pk, first_cp)
	firstscript := GetTaprootScript(first_pubkeyTaproot)
	taprootAddress, err := PubkeyToTapprootAddress(first_pubkeyTaproot)
	if err != nil {
		return nil, err
	}

	AddTaprootToWallet(url, firstscript)
	checkpoint, done := GetFirstCheckpointAddress(url, taprootAddress)
	AddTaprootToWallet(url, checkpoint.address)
	var new_checkpoint Checkpoint
	for {
		new_checkpoint, done = GetNextCheckpointFixed(url, checkpoint.txid)
		if done == nil {
			checkpoint = new_checkpoint
			AddTaprootToWallet(url, checkpoint.address)
		} else {
			return &checkpoint, nil
		}
	}
}
