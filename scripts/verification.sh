#! /bin/bash

tmux \
    new-session 'EUDICO_PATH=$PWD/data/dom ./eudico delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/dom ./eudico wait-api; EUDICO_PATH=$PWD/data/dom ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4; sleep 3' \; \
