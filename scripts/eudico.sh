#!/usr/bin/env bash

# start the miner process
miner() {
    # import wallet
    ./eudico wallet import --format=json-lotus f1*.key || true

    # start the miner
    ./eudico delegated miner
}

# start the node daemon process
daemon() {
    ./eudico delegated daemon --genesis=gen.gen --export-metrics=true
}

# start the eudico stats process
stats() {
    FULL_NODE=$(./eudico auth api-info --perm admin)
    cmd="export ${FULL_NODE}"
    eval ${cmd}

    echo "FULLNODE_API_INFO: ${FULLNODE_API_INFO}"
    
    # start the eudico stats
    ./eudico-stats run --no-sync true
}

while getopts ":hmnsca" option; do
   case $option in
      h) # display Help
        printf "USAGE: Util bash script for eudico testnet commands\n"
        printf "OPTIONS:\n"
        echo "-m    Start the miner"
        echo "-n    Start the node"
        echo "-s    Start the eudico-stats"
        echo "-c    Clear the current node data"
        exit;;
      m) # start the miner
        miner
        exit;;
      n) # start the node
        daemon
        exit;;
      c) # clear the node data
        rm -rf ~/.eudico
        exit;;
      s) # start the eudico stats
        stats
        exit;;
      a) # start all in one
        daemon & > node.log
        sleep 5
        miner & > miner.log
        sleep 5
        stats
        exit;;
     \?) # Invalid option
        printf "Error: Invalid option"
        exit;;
   esac
done