#!/usr/bin/env bash

make eudico
rm -rvf ~/.eudico
./eudico filcns genesis gen.gen

tmux \
    new-session  './eudico filcns daemon --genesis=gen.gen' \; \
    split-window './eudico wait-api; ./eudico filcns miner t01000; sleep 2'
