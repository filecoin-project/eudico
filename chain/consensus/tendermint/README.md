# Eudico with Tendermint Consensus

This is an experimental code for internal research purposes. It shouldn't be used in production.
The design document can be found [here](https://hackmd.io/@consensuslab/SJg-BGBeq).

## Requirements
Eudico and Tendermint requirements must be satisfied.
The most important one is Go1.17+.

## Install

### Tendermint

This section describes how to work with Tendermint using the github repo.
```
git clone https://github.com/tendermint/tendermint.git
cd tendermint
```

We don't recommend running the Tendermint code from `master` branch.
Instead, use the last stable version (at the moment of writing, we recommend version `0.35.1`):
```
git checkout $(git describe --tags `git rev-list --tags --max-count=1`)
```

Then install and run it:
```
make install
tendermint version
```

See the Tendermint [install instructions](https://github.com/tendermint/tendermint/blob/master/docs/introduction/install.md) for more information.

### Tendermint in Docker

To start a docker container with a Tendermint validator suitable for Eudico, run the following commands:

```
docker run -d -P -v /tmp:/tendermint --entrypoint /usr/bin/tendermint tendermint/tendermint:v0.35.1 init --key secp256k1 validator
docker run -d -P -v /tmp:/tendermint --entrypoint /usr/bin/tendermint tendermint/tendermint:v0.35.1 start
```

To start the Eudico application:
```
./eudico tendermint application
```

### Eudico
```
git clone git@github.com:filecoin-project/eudico.git
cd eudico
git submodule update --init --recursive
make eudico
```

## Run

### Tendermint Single Node

To start a one-node Tendermint blockchain use the following commands:
```
./eudico tendermint application
```

Please make sure that Tendermint uses secp256k1 keys.

```
tendermint init validator --key=secp256k1
tendermint start
```

### Tendermint Local Testnet

Use the following Tendermint [instructions](https://github.com/tendermint/tendermint/blob/master/docs/tools/docker-compose.md) as a basis.

Edit `localnet-start` target in the Tendermint [makefile](https://github.com/tendermint/tendermint/blob/2ffb26260053c87e4b44c0d00063494d771dcfec/Makefile#L269-L271) and add `--key secp256k1` option.

The edited target should look like the follows:
```
localnet-start: localnet-stop build-docker-localnode
    @if ! [ -f build/node0/config/genesis.json ]; then docker run --rm -v $(CURDIR)/build:/tendermint:Z tendermint/localnode testnet --key secp256k1 --config /etc/tendermint/config-template.toml --o . --starting-ip-address 192.167.10.2; fi
    docker-compose up
```

Add the following command into the Tendermint's testnet [docker-compose](https://github.com/tendermint/tendermint/blob/master/docker-compose.yml) file for each node 
and ports in 26650-26653:

```
command: node --proxy-app=tcp://host.docker.internal:$PORT
```

After that you should be able to run `./scripts/eud-tendermint-testnet.sh` script.

## Useful Commands

The following commands can be used in Eudico-Tendermint setup for testing and demonstration purposes.

### Eudico

```
./eudico tendermint genesis gen.gen
./eudico tendermint daemon --gen=gen.gen
./eudico wallet new secp256k1
./eudico wallet import ./f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key
./eudico tendermint miner --default-key
./eudico send t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi 1

```

#### Benchmarks

To perform simple benchmarks for the running deployment:
```
./eudico benchmark consensus
```

###  Tendermint
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

### Root Subnet with PoW
```
./eudico tspow miner --default-key

```

### Tmux

To stop a demo running via tmux:
```
tmux kill-session -t tendermint
```

All other Tmux commands can be found in the [Tmux Cheat Sheet](https://tmuxcheatsheet.com/).

### Tendermint Consensus in Subnet

To create a deployment run the following script:
```
./scripts/tendermint/eud-tendermint-testnet-subnet.sh
```

Then run the following commands:
```
 ./eudico subnet add --consensus 2 --name tendermint
 ./eudico subnet join --subnet=/root/t01001 10
 ./eudico subnet mine  --subnet=/root/t01001
 ./eudico --subnet-api=/root/t01001 wallet list
 ./eudico subnet fund --from=X --subnet=/root/t01001 11
 ./eudico --subnet-api=/root/t01001 send <addr> 10
 ./eudico subnet list-subnets
 ./eudico subnet release --from=X --subnet=/root/t01001
```

`t01001` name is just an example, in your setup the real subnet name may be different.

### Tendermint Consensus with Fault Injection

To run a deployment use the following script:
```
./scripts/mir/eud-tendermint-testnet.sh
```

Run the following commands in terminal 4 (that will be the bottom-right terminal):
```
stop-node1
stop-app1

start-app1
start-node1
```

## Testing

### Sanity Tests

The following command run Eudico consensus sanity testsuite:
```
make eudico-test
```

### Acceptance Tests

 The following acceptance tests can be used to make sure that the Tendermint consensus integration satisfies to the Eudico consensus requirements:
 1. Verify messages (funds) can be sent between accounts: nonce are increased, funds are updated.
 2. Verify the Tendermint consensus can be instantiated in the root net.
 3. Verify the Tendermint consensus can be instantiated in a subnet within the hierarchical framework.
 4. Verify cross-messages can be sent.
 5. Verify a Tendermint node can be restarted while mining.