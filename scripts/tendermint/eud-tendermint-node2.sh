#!/usr/bin/env bash

NODE_PATH="~/.eudico-node2/"
API_PORT="1235"
NODE_KEY="t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi"
GEN="./testdata/tendermint.gen"
PEER=$1

rm -rf ~/.eudico-node2/

# ./eudico tendermint daemon --genesis=./testdata/gen.gen --api=1235
# ./eudico net connect /ip4/192.168.88.254/tcp/53029/p2p/12D3KooWRmsgJzb2vJzWtqkXrvdZpAGcgFyxLzNTtFk8mWqbCYFu
# ./eudico wallet import ./testdata/t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi.key
# ./eudico tendermint miner t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi

tmux new-session -d -s "tendermint2" \; \
  setenv EUDICO_PATH $NODE_PATH \; \
  split-window -t "tendermint2:0" -h \; \
  send-keys -t "tendermint2:0.0" "export EUDICO_PATH=$NODE_PATH;
   ./eudico tendermint daemon --genesis=$GEN --api=$API_PORT" Enter \; \
  send-keys -t "tendermint2:0.1" "export EUDICO_PATH=$NODE_PATH;
   ./eudico wait-api;
   ./eudico net connect $PEER;
   ./eudico net peers;
   ./eudico wallet import ./testdata/$NODE_KEY.key; sleep 2;
   ./eudico set-default t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi;
   ./eudico tendermint miner $NODE_KEY" Enter \; \
  attach-session -t "tendermint2:0.0"
