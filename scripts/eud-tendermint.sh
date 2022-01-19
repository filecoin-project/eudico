#!/usr/bin/env bash

make eudico
rm -rvf ~/.eudico
#rm -rvf ~/.tendermint/configmake
rm -rvf ~/.tendermint/data

make tendermint
#tendermint init validator
mkdir ~/.tendermint/data
cp ~/.tendermint/priv_validator_state.json ~/.tendermint/data
#cp ~/.tendermint/genesis.json ~/.tendermint/config

sleep 2;

tmux new-session -d -s "tendermint" \; \
  split-window -t "tendermint:0" -h \; \
  split-window -t "tendermint:0.0" -v \; \
  split-window -t "tendermint:0.2" -v \; \
  send-keys -t "tendermint:0.0" "tendermint start" Enter \; \
  send-keys -t "tendermint:0.1" "./tendermint" Enter \; \
  send-keys -t "tendermint:0.2" "sleep 6; ./eudico tendermint daemon --genesis=./testdata/gen.gen" Enter \; \
  send-keys -t "tendermint:0.3" './eudico wait-api; ./eudico wallet import ./testdata/f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key; sleep 2; ./eudico tendermint miner f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy' Enter \; \
  attach-session -t "tendermint:0.1"

