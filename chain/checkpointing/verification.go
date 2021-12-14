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

func GetFirstCheckpointAddress(taprootAddress string) (Checkpoint, error) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(payload)
	list := result["result"].([]interface{})
	for _, item := range list {
		item_map := item.(map[string]interface{})
		if item_map["address"] == taprootAddress {
			tx_id := item_map["txid"].(string)
			payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getrawtransaction\", \"params\": [\"" + tx_id + "\", true]}"
			result = jsonRPC(payload)
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

func GetNextCheckpointFixed(txid string) (Checkpoint, error) {
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(payload)
	list := result["result"].([]interface{})
	for _, item := range list {
		item_map := item.(map[string]interface{})
		tx_id := item_map["txid"].(string)
		payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getrawtransaction\", \"params\": [\"" + tx_id + "\", true]}"
		result = jsonRPC(payload)
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

func GetLatestCheckpoint(first_pk []byte, first_cp []byte) Checkpoint {
	first_pubkeyTaproot := GenCheckpointPublicKeyTaproot(first_pk, first_cp)
	firstscript := GetTaprootScript(first_pubkeyTaproot)
	taprootAddress := PubkeyToTapprootAddress(first_pubkeyTaproot)
	AddTaprootToWallet(firstscript)
	checkpoint, done := GetFirstCheckpointAddress(taprootAddress)
	AddTaprootToWallet(checkpoint.address)
	var new_checkpoint Checkpoint
	for {
		new_checkpoint, done = GetNextCheckpointFixed(checkpoint.txid)
		if done == nil {
			checkpoint = new_checkpoint
			AddTaprootToWallet(checkpoint.address)
		} else {
			return checkpoint
		}
	}
}
