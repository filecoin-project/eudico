#!/bin/bash
set -e
NUM=$1
if [ $# -ne 1 ]
  then
    echo "Provide the number of genesis miners as first argument"
    exit 1
fi
if [ $NUM -gt 9 ]; then
        echo "More than 9 genesis miners not currenlty supported"
        exit 1
fi
rm -rf ~/.genesis-sectors
echo [*] Genesis template
../lotus-seed genesis new genesis.json
echo [*] Adding pre-sealed miners
for((i=0;i<$NUM;i++))
do
../lotus-seed pre-seal --sector-size 2KiB --num-sectors 2 --miner-addr t0100$i
../lotus-seed genesis add-miner genesis.json ~/.genesis-sectors/pre-seal-t0100$i.json
cd ~/.genesis-sectors && zip -r genesis-sector-t0100$i.zip ./* && cd - && mv ~/.genesis-sectors/genesis-sector-t0100$i.zip .
rm -r ~/.genesis-sectors
done

echo [*] Generating genesis
../lotus-seed genesis car --out eudicogarden.car genesis.json
