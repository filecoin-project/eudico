#!/usr/bin/env bash

make eudico
rm -rvf ~/.eudico
./eudico delegated genesis $(echo f1* | tr '.' ' ' | awk '{print $1}') gen.gen

tmux \
    new-session  './eudico delegated daemon --genesis=gen.gen' \; \
    split-window './eudico wait-api; ./eudico wallet import --format=json-lotus 'f1*'; ./eudico delegated miner; sleep 2'
