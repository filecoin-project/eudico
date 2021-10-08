#!/usr/bin/env bash
MINER=$(./eudico wallet list | awk 'NR==2 {print $1}')

if [[ "$1" == "add" ]]; then
        echo [*] Create new shard with enough stake to mine
        ./eudico send --from=$MINER --method=2 --params-json='{"Name":"dGVzdFNoYXJk", "Consensus": 0, "DelegMiner": "t1r6o5d5s5zjzqhqs4nh3ac7k5e7dhg5oqa7v64oy"}' t064 1
fi

if [[ "$1" == "join" ]]; then
        echo [*] Create new shard without stake to mine
        ./eudico send --from=$MINER --method=2 --params-json='{"Name":"dGVzdFNoYXJk", "Consensus": 0, "DelegMiner": "t1r6o5d5s5zjzqhqs4nh3ac7k5e7dhg5oqa7v64oy"}' t064 0.5
        echo [*] Wait to see if we mine
        sleep 10
        echo [*] Stake enough to start mining
        # The ID to specify here is the bytes representation fo the 
        # CID of the shard. Check logs if you see this fails, it may change in the future.
        ./eudico send --from=$MINER --method=3 --params-json='{"ID":"AVUACXRlc3RTaGFyZA=="}' t064 1
fi

# To interact with a specific shard.
#./eudico send --from=$MINER --shard=testShard --method=2 --params-json='{"Shards":["dGVzdFNoYXJk"]}' t064 0

# To generate the shard name // CID in bytes from a string
# TODO: Make it a convenient script.
# https://play.golang.org/
