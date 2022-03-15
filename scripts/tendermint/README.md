# Tendermint Scripts

This directory contains scripts to demonstrate various applications of Tendermint consensus in Eudico.

### `eud-tendermint-testnet-ref.sh`
This script runs a 4-node Tendermint local testnet and 4 Eudico nodes, then a root subnet with Tendermint consensus is instantiated.

### `eud-tendermint-testnet.sh`
This script implements the same logic as previous, but it uses Tendermint node as a docker container from docker hub.

### `eud-tendermint-testnet-subnet.sh`
This script runs a 4-node Tendermint local testnet in docker containers and 4 Eudico nodes, a root subnet with PoW consensus is instantiated. Then you can instantiate a new subnet with Tendermint consensus. 