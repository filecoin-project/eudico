#! /bin/bash

curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"createwallet\", \"params\": [\"wow\"]}" \
    -H 'Content-Type:application/json'

ADDRESS=$(curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"getnewaddress\", \"params\": [\"wow\"]}" \
    -H 'Content-Type:application/json' | jq -r '.result')

echo "$ADDRESS"

curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"generatetoaddress\", \"params\": [150, \"$ADDRESS\"]}" \
    -H 'Content-Type:application/json'

tmux \
    new-session 'EUDICO_PATH=$PWD/data/alice ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window -h 'EUDICO_PATH=$PWD/data/bob ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/bob ./eudico wait-api; EUDICO_PATH=$PWD/data/bob ./eudico log set-level error; EUDICO_PATH=$PWD/data/bob ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4; sleep 3' \; \
    split-window -h 'EUDICO_PATH=$PWD/data/charlie ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/charlie ./eudico wait-api; EUDICO_PATH=$PWD/data/charlie ./eudico log set-level error; EUDICO_PATH=$PWD/data/charlie ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4 /ip4/127.0.0.1/tcp/3001/p2p/12D3KooWNTyoBdMB9bpSkf7PVWR863ejGVPq9ssaaAipNvhPeQ4t; sleep 3' \; \
    select-pane -t 0 \; \
    split-window -v 'EUDICO_PATH=$PWD/data/alice ./eudico wait-api; EUDICO_PATH=$PWD/data/alice ./eudico log set-level error; EUDICO_PATH=$PWD/data/alice ./eudico wallet import --as-default --format=json-lotus kek.key; EUDICO_PATH=$PWD/data/alice ./eudico delegated miner; sleep infinity' \;