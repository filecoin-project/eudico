#!/usr/bin/env bash

NODE_PATH="~/.eudico-node2/"
API_PORT="1235"
NODE_KEY="t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi"
GEN="./testdata/gen.gen"
PEER=$1

rm -rf ~/.eudico-node2/
export EUDICO_PATH=$NODE_PATH
./eudico tendermint daemon --genesis=$GEN --api=$API_PORT

