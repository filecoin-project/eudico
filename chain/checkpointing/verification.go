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
	// List all the transactions and look for one that match the taproot address
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	// url is the url of the bitcoin node with the RPC port
	result := jsonRPC(url, payload)
	list := result["result"].([]interface{})
	for _, item := range list {
		item_map := item.(map[string]interface{})

		// Check if address match taproot adress given if yes return it
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
	first_pubkeyTaproot := genCheckpointPublicKeyTaproot(first_pk, first_cp)
	firstscript := getTaprootScript(first_pubkeyTaproot)
	taprootAddress, err := pubkeyToTapprootAddress(first_pubkeyTaproot)
	if err != nil {
		return nil, err
	}

	/*
		Bitcoin node only allow to collect transaction from addresses that are registered in the wallet
		In this step we import taproot script (and not the address) in the wallet node to then be able to ask
		for transaction linked to it.
	*/
	addTaprootToWallet(url, firstscript)
	checkpoint, done := GetFirstCheckpointAddress(url, taprootAddress)
	// Aging we add taproot "address" (actually the script) to the wallet in the Bitcoin node
	addTaprootToWallet(url, checkpoint.address)
	var new_checkpoint Checkpoint
	for {
		new_checkpoint, done = GetNextCheckpointFixed(url, checkpoint.txid)
		if done == nil {
			checkpoint = new_checkpoint
			addTaprootToWallet(url, checkpoint.address)
		} else {
			// Return once we have found the last one in bitcoin
			return &checkpoint, nil
		}
	}
}
