package checkpointing

import (
	"errors"
	"fmt"
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

	//iterate through list of transactions
	for _, item := range list {
		item_map := item.(map[string]interface{})
		// Check if address match taproot adress given if yes return it
		if item_map["address"] == taprootAddress {
			tx_id := item_map["txid"].(string)
			payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getrawtransaction\", \"params\": [\"" + tx_id + "\", true]}"
			result = jsonRPC(url, payload)
			reader := result["result"].(map[string]interface{})
			//fmt.Println(result)
			// vout is the list of outputs of the transaction
			vout := reader["vout"].([]interface{})
			taprootOut := vout[0].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			new_address := taprootOut["hex"].(string)
			var cid string
			if len(vout) > 1 {
				cidOut := vout[1].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
				cid = cidOut["hex"].(string)
			} else {
				cid = "0000"
			}
			return Checkpoint{txid: tx_id, address: new_address, cid: cid[4:]}, nil
		}
	}
	return Checkpoint{}, errors.New("Did not find checkpoint")
}

func GetNextCheckpointFixed(url, txid string) (Checkpoint, error) {
	//List 500000000 transactions (only includes the ones from/to our wallet)
	// * stands for no label (i.e. transactions without a specific label)
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	result := jsonRPC(url, payload)
	list := result["result"].([]interface{})
	// for each transaction in the list
	fmt.Println("transaction id (inside getnextcheckpointfixed): ", txid)
	for _, item := range list {
		item_map := item.(map[string]interface{})
		// get the tx id
		tx_id := item_map["txid"].(string)
		//get the associated raw tx
		payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getrawtransaction\", \"params\": [\"" + tx_id + "\", true]}"
		result = jsonRPC(url, payload)
		//fmt.Println(result)
		reader := result["result"].(map[string]interface{})
		//fmt.Println("getnextcheckpointfixed print: ", reader)
		new_txid := reader["txid"].(string)
		//read the ix id on the input
		vin := reader["vin"].([]interface{})[0].(map[string]interface{})["txid"]
		if vin == nil {
			continue
		}
		//fmt.Println("vin string: ", vin.(string))
		//check that the input of the transaction is equal to the txid
		if vin.(string) == txid {
			fmt.Println("found txid")
			vout := reader["vout"].([]interface{})
			taprootOut := vout[0].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			new_address := taprootOut["hex"].(string)
			cidOut := vout[1].(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			cid := cidOut["hex"].(string)
			fmt.Println("Worked fine")
			return Checkpoint{txid: new_txid, address: new_address, cid: cid[4:]}, nil
		}
	}
	//fmt.Println("Did not work fine")
	return Checkpoint{}, errors.New("Did not find checkpoint")
}

func GetLatestCheckpoint(url string, first_pk []byte, first_cp []byte) (*Checkpoint, error) {
	first_pubkeyTaproot := genCheckpointPublicKeyTaproot(first_pk, first_cp)
	firstscript := getTaprootScript(first_pubkeyTaproot)
	taprootAddress, err := pubkeyToTapprootAddress(first_pubkeyTaproot)
	if err != nil {
		log.Errorf("Error when getting the last checkpoint from bitcoin", err)
		return nil, err
	}

	/*
		Bitcoin node only allow to collect transaction from addresses that are registered in the wallet
		In this step we import taproot script (and not the address) in the wallet node to then be able to ask
		for transaction linked to it.
	*/
	addTaprootToWallet(url, firstscript)
	fmt.Println(firstscript)
	checkpoint, done := GetFirstCheckpointAddress(url, taprootAddress)
	// Again we add taproot "address" (actually the script) to the wallet in the Bitcoin node
	addTaprootToWallet(url, checkpoint.address)
	var new_checkpoint Checkpoint
	fmt.Println("Starting get last checkpoint loop")
	for {
		new_checkpoint, done = GetNextCheckpointFixed(url, checkpoint.txid)
		if done == nil {
			checkpoint = new_checkpoint
			addTaprootToWallet(url, checkpoint.address)
			fmt.Println(checkpoint)
		} else {
			// Return once we have found the last one in bitcoin
			return &checkpoint, nil
		}
	}
}

func CheckIfFirstTxHasBeenSent(url string, first_pk []byte, first_cp []byte) (bool, string, error) {
	// the following will only work if we use one bitcoin node for our demo
	first_pubkeyTaproot := genCheckpointPublicKeyTaproot(first_pk, first_cp)
	firstscript := getTaprootScript(first_pubkeyTaproot)
	taprootAddress, err := pubkeyToTapprootAddress(first_pubkeyTaproot)
	if err != nil {
		log.Errorf("Error when getting the last checkpoint from bitcoin", err)
		return false, "", err
	}

	/*
		Bitcoin node only allow to collect transaction from addresses that are registered in the wallet
		In this step we import taproot script (and not the address) in the wallet node to then be able to ask
		for transaction linked to it.
	*/
	addTaprootToWallet(url, firstscript)

	//now we check the transactions associated with our taproot address
	//first list the tx
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"listtransactions\", \"params\": [\"*\", 500000000, 0, true]}"
	// url is the url of the bitcoin node with the RPC port
	result := jsonRPC(url, payload)
	list := result["result"].([]interface{})
	//iterate through list of transactions
	for _, item := range list {
		item_map := item.(map[string]interface{})
		// Check if address match taproot adress given
		// if yes i means there exist some transaction associTED WITH THIS address
		if item_map["address"] == taprootAddress {
			log.Infow("Initial transaction has already been sent")
			txid := item_map["txid"].(string)
			return true, txid, nil

			//check if something was sent from the address (i.e. do we need to go to next checkpoint?)
			// if item_map["category"].(string) == "sent" {
			// }
		}
	}

	return false, "", nil
}
