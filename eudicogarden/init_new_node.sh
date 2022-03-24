#!/bin/bash
NUM=$1
BOOTSTRAP_MADDR=$2
if [ $# -ne 2 ]
  then
    echo "Provide the miner index as first argument and bootstrap maddr as second"
    exit 1
fi

tmux new-session -d -s "eud" \; \
        split-window -t "eud:0" -v \; \
        split-window -t "eud:0" -h \; \
        send-keys -t "eud:0.1" "./start_node.sh" Enter \; \
        send-keys -t "eud:0.2" "../eudico wait-api && ../eudico net connect $BOOTSTRAP_MADDR && sleep 10" Enter \; #\
        # send-keys -t "eud:0.2" "./init_miner.sh $NUM && sleep 120" Enter \; \
        # send-keys -t "eud:0.2" "./start_miner.sh $NUM" Enter \; #\
        # attach-session -t "eud:0.0"
