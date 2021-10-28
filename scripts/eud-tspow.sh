#!/usr/bin/env bash

make eudico
rm -rvf ~/.eudico
./eudico tspow genesis gen.gen
miner=$(echo f1* | tr '.' ' ' | awk '{print $1}')

tmux \
    new-session  './eudico delegated daemon --genesis=gen.gen' \; \
    split-window './eudico wait-api; ./eudico wallet import --as-default --format=json-lotus 'f1*'; ./eudico tspow miner $miner; sleep 2'
