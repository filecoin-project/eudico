#! /bin/bash

tmux \
    new-session 'EUDICO_PATH=$PWD/data/dom ./eudico delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/dom ./eudico wait-api; EUDICO_PATH=$PWD/data/dom ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4 /ip4/127.0.0.1/tcp/3001/p2p/12D3KooWNTyoBdMB9bpSkf7PVWR863ejGVPq9ssaaAipNvhPeQ4t /ip4/127.0.0.1/tcp/3002/p2p/12D3KooWF1aFCGUtsGEaqNks3QADLUDZxW7ot7jZPSoDiAFKJuM6; sleep 3' \; \
