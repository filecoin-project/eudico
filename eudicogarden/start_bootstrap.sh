#!/bin/bash
tmux new-session -d -s "eud" \; \
        split-window -t "eud:0" -v \; \
        send-keys -t "eud:0.1" "../eudico filcns daemon --genesis=eudicogarden.car --profile=bootstrapper" Enter \;
