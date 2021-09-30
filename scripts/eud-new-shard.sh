#!/usr/bin/env bash
MINER=$(./eudico wallet list | awk 'NR==2 {print $1}')
./eudico send --from=$MINER --method=2 --params-json='{"Name":"dGVzdFNoYXJk", "Consensus": 0}' t064 0

# To interact with a specific shard.
#./eudico send --from=$MINER --shard=testShard --method=2 --params-json='{"Shards":["dGVzdFNoYXJk"]}' t064 0
