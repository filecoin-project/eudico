#!/bin/bash

# init the node and the genesis
init() {
    # generate the wallet
    ./lotus-keygen -t secp256k1

    # generate the genesis
    ADDR=$(echo f1* | tr '.' ' ' | awk '{print $1}')
    ./eudico delegated genesis $ADDR gen.gen

    mkdir -p credentials
    mv gen.gen ./credentials
    mv f1* ./credentials
}

# start the miner process for the root net
miner() {
    # import wallet
    ./eudico wallet import --format=json-lotus ./credentials/f1*.key || true

    # start the miner
    ./eudico delegated miner
}

fund_subnet() {
  subnet_id=$1
  fund=$2
  ./eudico subnet fund --subnet ${subnet_id} ${fund}
}

# start the miner process for subnets
mine_subnet() {
  subnet_id=$1
  ./eudico subnet mine --subnet ${subnet_id}
}

create_subnet() {
  subnet_id=$(./eudico subnet add --name test1 --consensus POW | grep -o "/root/.*")
  echo "create subnet ${subnet_id}"
  ./eudico subnet join --subnet ${subnet_id} 2
  echo "join subnet ${subnet_id}"
}

create_subnets() {
  num=10
  for i in $(seq 1 $num); do create_subnet; done
}

# start the node daemon process
daemon() {
    ./eudico delegated daemon --genesis=./credentials/gen.gen --export-metrics=true
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

while getopts ":hmnscaiv" option; do
   case $option in
      h) # display Help
        printf "USAGE: Util bash script for eudico testnet commands\n"
        printf "OPTIONS:\n"
        echo "-m    Start the miner"
        echo "-n    Start the node"
        echo "-s    Start the eudico-stats"
        echo "-c    Clear the current node data"
        echo "-a    Start all in one node"
        echo "-i    Init the key and genesis"
        echo "-k    Create subnets"
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
      i) # init the node and genesis
        init
        exit;;
      v) # create subnets
        create_subnets
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