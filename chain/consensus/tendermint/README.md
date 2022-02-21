# Eudico with Tendermint Consensus

This is an experimental code for internal research purposes. It shouldn't be used in production.
The design document is located [here](https://hackmd.io/@TqudR0GXRiedtuNRKE_EVA/HJ-e2ZQaY).

## Requirements
Eudico and Tendermint requirements must be satisfied.
The most important one is Go1.17+.

## Install

### Tendermint

```
go get github.com/tendermint/tendermint
cd $GOPATH/src/github.com/tendermint/tendermint
make install
make build
tendermint version
```

See the Tendermint [install instructions](https://github.com/tendermint/tendermint/blob/master/docs/introduction/install.md) for more information.

### Eudico
```
git clone git@github.com:filecoin-project/eudico.git
cd eudico
git submodules update
make eudico
```

## Run

### Tendermint Single Node

To start a one-node Tendermint blockchain use the following commands:
```
./eudico tendermint application
```

```
tendermint init validator --key=secp256k1
tendermint start
./
```

Please make sure that Tendermint uses secp256k1 keys.

### Tendermint Local Testnet

Use the following Tendermint [instructions](https://github.com/tendermint/tendermint/blob/master/docs/tools/docker-compose.md) as a basis.

Use the following target in Tendermint makefile to use secp256k1 keys:
```
localnet-start: localnet-stop build-docker-localnode
    @if ! [ -f build/node0/config/genesis.json ]; then docker run --rm -v $(CURDIR)/build:/tendermint:Z tendermint/localnode testnet --key secp256k1 --config /etc/tendermint/config-template.toml --o . --starting-ip-address 192.167.10.
    docker-compose up
```

Add the following command into Tendermint's testnet docker-compose file for each node:

```
command: node --proxy-app=tcp://host.docker.internal:$PORT
```

After that you can run `./scripts/eud-tendermint-testnet.sh` script.

## Commands

The following is lists of useful commands that can be used in Eudico-Tendermint setup for testing and demonstration purposes.

### Eudico

```
./eudico tendermint genesis gen.gen
./eudico tendermint daemon --gen=gen.gen
./eudico wallet new secp256k1
./eudico wallet import ./f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key
./eudico tendermint miner --default-key
./eudico send t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi 1

```

### Tendermint
```
./eudico tendermint application
tendermint init validator --key=secp256k1
tendermint start
curl -s 'localhost:26657/abci_query?data="1"'
curl -s 'http://localhost:26657/broadcast_tx_sync?tx=0x828a0055017642efe6162dfc3e4e01df770743f22bf903e04455017642efe6162dfc3e4e01df770743f22bf903e0440049000de0b6b3a76400001a00084873450018aef1bd44000187c600405842018172eb88f4f9a59a1e0f0b820d69681403b69a129daed4831729336c6534036b701e4b22572f19c3e89a7341fc4e435ae8b7accf75cf7b3d1e1200108af7640c01'

```
### Networking
```
./eudico net listen

```

### Root Subnet PoW
```
./eudico tspow miner --default-key

```

### Subnet Demo
```
 ./eudico subnet add --consensus 2 --name tendermint
 ./eudico subnet join --subnet=/root/t01001 10
 ./eudico subnet mine  --subnet=/root/t01001
 ./eudico subnet fund --from=X --subnet=/root/t01001 11
 ./eudico --subnet-api=/root/t01001 wallet list
 ./eudico subnet list-subnets
```

### Fault Tolerance Demo

To run a deployment:
```
./scripts/eud-tendermint-testnet.sh
```

Run the following commands in terminal 4 (bottom-right terminal):
```
connect-node0

stop-node1
stop-app1

start-app1
start-node1

```

## How to Add a Consensus Protocol to Eudico
- Register a consensus constant in `chain/consensus/hierarchical/types.go`
- Instantiate a consensus miner in a subnet in `chain/consensus/hierarchical/subnet/consensus/consensus.go`
- Implement genesis block functions in `chain/consensus/hierarchical/actors/subnet/tendermint.go`
- Return the consensus' `TipsExecutor` and `Weight` in `chain/consensus/hierarchical/subnet/utils.go`
- Decide how to compute a state and implement the corresponding logic in `chain/consensus/$CONSENSUS/compute_state.go`
- Implement `Consensus interface` defined in `chain/consensus/iface.go` for the consensus algorithm
- Add the corresponding CLI commands in `cmd/eudico/$CONSENSUS.go`
- Adapt [bad blocks cache](https://github.com/filecoin-project/eudico/blob/0306742e553f6bd6260332b501bb65a5bfc16a76/chain/sync.go#L725) for the case when Tendermint nodes were unreachable
