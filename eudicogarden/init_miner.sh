#!/bin/bash
# set -e
rm -rf ~/.genesis-sectors
export LOTUS_LIBP2P_LISTENADDRESSES="/ip4/0.0.0.0/tcp/0"
NUM=$1
if [ $# -ne 1 ]
  then
    echo "Provide the miner index as first argument"
    exit 1
fi
if [ $NUM -gt 9 ]; then
        echo "More than 9 genesis miners not currenlty supported"
        exit 1
fi
unzip genesis-sector-t0100$NUM.zip -d ~/.genesis-sectors
../eudico wait-api
../lotus wallet import --as-default ~/.genesis-sectors/pre-seal-t0100$NUM.key
# If file not imported it means we haven't initialized miner yet
if [ $? -eq 0 ]; then
../lotus-miner init --genesis-miner --actor=t0100$NUM --sector-size=2KiB --pre-sealed-sectors=~/.genesis-sectors --pre-sealed-metadata=~/.genesis-sectors/pre-seal-t0100$NUM.json --nosync
fi
# ../lotus-miner run --nosync
