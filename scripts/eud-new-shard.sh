#!/usr/bin/env bash
MINER=$(./eudico wallet list | awk 'NR==2 {print $1}')
./eudico send --from=$MINER --method=2 --params-json='{"Shards": ["test"]}' t064 0
