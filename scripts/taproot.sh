#! /bin/bash

# create Bitcoin wallet
curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"createwallet\", \"params\": [\"wow\"]}" \
    -H 'Content-Type:application/json'

# create a new address with getnewadress
ADDRESS=$(curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"getnewaddress\", \"params\": [\"wow\"]}" \
    -H 'Content-Type:application/json' | jq -r '.result')

echo "$ADDRESS"

# create 150 Bitcoin blocks with the coinbase rewards that goes to our own address
# (note: according to Bitcoin's rules, we need to wait before being able to access the coinbase rewards)
curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"generatetoaddress\", \"params\": [150, \"$ADDRESS\"]}" \
    -H 'Content-Type:application/json'
# Note: after this we do not mine Bitcoin blocks anymore.
# To create more Bitcoin blocks, we need to run another script: generate-bitcoin-blocks.sh in
# a new window.

# We now create the transaction that funds the initial public key.
# This initial key is constructed based on the keys of Alice, Bob, Charlie 
# and a commitment to the Eudico genesis block.
# If the hash of the genesis block changes this key needs to be re-generated.
# (To double-check: This hash is defined in eudico delegated consensus: eudico/chain/gen/genesis/genesis.go.)
# In order to compute the initial key, we can uncomment the following lines in the 
# checkpointing/sub.go file:
#       address, _ := pubkeyToTapprootAddress(c.pubkey)
#        fmt.Println(address)
# This address changes when we change either one of the participants keys (i.e., Alice Bob or Charlie) or
# or eudico genesis block (this should not happen very often).
# Note if we change this address, we need to re-start Bitcoin regtest from scratch.
# Ideally we would like to use the transaction id instead of address in order to retrieve the first checkpoint.
# 50 is the amount sent (50 bitcoins)
curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendtoaddress\", \"params\": [\"bcrt1p4ffl08gtqmc00j3cdc8x9tnqy8u4c0lr8vryny8um605qd78w7vs90n7mx\", 50]}" \
    -H 'Content-Type:application/json'


tmux \
    new-session 'EUDICO_PATH=$PWD/data/alice ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window -h 'EUDICO_PATH=$PWD/data/bob ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/bob ./eudico wait-api; EUDICO_PATH=$PWD/data/bob ./eudico log set-level error; EUDICO_PATH=$PWD/data/bob ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4; sleep 3' \; \
    split-window -h 'EUDICO_PATH=$PWD/data/charlie ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/charlie ./eudico wait-api; EUDICO_PATH=$PWD/data/charlie ./eudico log set-level error; EUDICO_PATH=$PWD/data/charlie ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4 /ip4/127.0.0.1/tcp/3001/p2p/12D3KooWNTyoBdMB9bpSkf7PVWR863ejGVPq9ssaaAipNvhPeQ4t; sleep 3' \; \
    select-pane -t 0 \; \
    split-window -v 'EUDICO_PATH=$PWD/data/alice ./eudico wait-api; EUDICO_PATH=$PWD/data/alice ./eudico log set-level error; EUDICO_PATH=$PWD/data/alice ./eudico wallet import --as-default --format=json-lotus kek.key; EUDICO_PATH=$PWD/data/alice ./eudico delegated miner; sleep infinity' \;