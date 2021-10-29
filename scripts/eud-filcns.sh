#!/usr/bin/env bash

make eudico
rm -rvf ~/.eudico
./eudico filcns genesis gen.gen

tmux \
    new-session  './eudico filcns daemon --genesis=gen.gen' \; \
    split-window './eudico wait-api; ./eudico filcns miner t01000; sleep 2'

# NOTE: In this case the only key with funds initially is the genesis miner
# key generated in filcns genesis and that can be found in 
# $LOTUS_PATH/network_name/*.key. Although at this point it will be already
# imported as the default address (this is done while initializing the miner,
# the miner function tries to initialize the repo for the miner if it hasn't 
# been initialized yet (see  chain/consensus/filcns/miner/mine.go).
