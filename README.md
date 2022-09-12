
# Bitcoin checkpointing MVP demo
This fork allows to run the [Pikachu protocol](https://arxiv.org/abs/2208.05408) on top of [Eudico](https://github.com/filecoin-project/eudico), a fork of Lotus, the main client for Filecoin nodes.
We propose two different options to run this demo: either do a demo with 4 nodes on your local machine, or deploy a Kubernetes container to test with around 20 nodes.

NB: this meant to be a demo and by no means used in production.


## Pre-requisite

### Install Go
Install golang.
Follow https://go.dev/doc/install and test your installation is complete with `go version`.
### Install docker

Follow this
https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository

And postintall step to avoid using `sudo`.
https://docs.docker.com/engine/install/linux-postinstall/

### Install rust


https://doc.rust-lang.org/book/ch01-01-installation.html


## Run 4 nodes demo

On a linux machine.

### Get the correct data

At the moment the following repo: https://github.com/sa8/B2-4nodes-demo-data must be copied in a new folder called `data` inside eudico for the 4-nodes demo to work. We start by cloning it this repo. 

```
$ git clone https://github.com/sa8/B2-4nodes-demo-data.git fil-taproot-data
```
(The default `data` folder that is currently in eudico contains the data compatible with the Kubernetes demo hence whenever the repo is pulled, we should run `cp -R ../fil-taproot-data/data ./` (or run the script `./scripts/delete_eudico_data.sh`) to make sure the data corresponds to the "small" demo and not the Kubernetes one.)

### Build Eudico

Install requirements.
```
$ sudo apt update
$ sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget tmux -y && sudo apt upgrade -y

```

Build Eudico executable from B2 branch (checkout previous commit that is stable):
```
$ git clone https://github.com/filecoin-project/eudico.git
$ cd eudico
$ git checkout mocked-power-actor
$ git checkout 97af8c342f3e76f18702b161a18e4c7480c56626
$ make clean eudico
$ cp -R ../fil-taproot-data/data ./
```


If you rebuild do this instead to make it faster.
```
$ make eudico
```






### Start dockerize Bitcoin node

Create bitcoin.conf file in `$HOME/` and copy the following:
```toml
regtest=1
rpcallowip=0.0.0.0/0
txindex=1
server=1
disablewallet=0

fallbackfee=0.001
paytxfee=0.001
maxtxfee=1000
mintxfee=0.00000001

rpcuser=satoshi
rpcpassword=amiens

[regtest]
rpcport=18443
```

Start docker container (using https://hub.docker.com/r/rllola/bitcoind-fil-demo)
```
$ docker pull rllola/bitcoind-fil-demo
$ docker run -d --network=host --mount type=bind,source=${HOME}/bitcoin.conf,target=/root/.bitcoin/bitcoin.conf --name bitcoind_regtest rllola/bitcoind-fil-demo
```

### Start dockerize Minio

```
$ docker run -d --net=host -e "MINIO_ROOT_USER=lola" -e "MINIO_ROOT_PASSWORD=123secure" quay.io/minio/minio server /data --console-address "0.0.0.0:9001"
```
Note: if using a virtual machine, make sure to allow connection to port 9001.

Connect to the MiniO console in your browser by typing: 
`http://$ipaddress:9001`, where `$ipadress` is your IP address (the one of your VM if applicable).
Use the username `lola` and password `123secure` to connect to the Minio console.
Create a bucket named `eudico`.

![](https://i.imgur.com/pVAGoWS.png)



### Create key file
Create the key of the eudico miner.

```
$ echo '{"Type":"secp256k1","PrivateKey":"8VcW07ADswS4BV2cxi5rnIadVsyTDDhY1NfDH19T8Uo="}' > kek.key
```

This private key was generated separetely. If this key is changed the script will need to be updated accordingly.

### Generate gen.gen

```
$ export LOTUS_SKIP_GENESIS_CHECK=_yes_
$ ./eudico delegated genesis t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba gen.gen
```

This creates a new file call `gen.gen` that contains the genesis block.
`t1d...xba` is the address that corresponds to the eudico key of our miner.

### Run script permission

Required after `git clone` because the data folder doesn't keep the right permission.
```
$ ./scripts/data-permissions.sh
```

### Run demo

```
$ ./scripts/taproot.sh
```

![](https://i.imgur.com/TjrzdEV.png)

You will see 4 tmux terminals showing 3 nodes and a miner running. Logs will differ from screenshot.
The very first checkpoint will only be triggered after we add mocked power actor (see below) to the chain.
Alice Bob and Charlie will sign the first checkpoint using their pre-generated keys. After this the new keys generated during the DKG will be used. (The initial keys were generated using the frost-libp2p library.)


#### Create more Bitcoin blocks

Run the following command to generate more Bitcoin blocks (needed for the demo to continue running).

```
$ ./scripts/generate-bitcoin-blocks.sh
```



#### Trigger a DKG and signing

Send a transaction to trigger the DKG using this command in your terminal.
Note: the following command adds all the current node (Alice, Bob, Charlie ) to the list of mocked miners (using the mocked power actor). This will trigger a new DKG and the signing will start.
Alternatively you can run `./scripts/add-initial-mpower.sh.`

```
EUDICO_PATH=$PWD/data/alice ./eudico send --from t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba --method 2 --params-json "{\"Miners\":[\"12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4\", \"12D3KooWNTyoBdMB9bpSkf7PVWR863ejGVPq9ssaaAipNvhPeQ4t\", \"12D3KooWF1aFCGUtsGEaqNks3QADLUDZxW7ot7jZPSoDiAFKJuM6\"]}" t065 0
```
Now we can add a new node, Dominic with the following command in a new terminal.

```
$ ./scripts/verification.sh
```
This creates a new eudico node for Dominic.
Next, we trigger a new DKG by adding Dominic mocked power to the chain, after the DKG is complete, Alice, Bob and Charlie will sign a transaction to "update" the key on the Bitcoin blockchain and after this Dominic can start participating in the checkpointing.

```
$ EUDICO_PATH=$PWD/data/alice ./eudico send --from t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba --method 2 --params-json "{\"Miners\":[\"12D3KooWSpyoi7KghH98SWDfDFMyAwuvtP8MWWGDcC1e1uHWzjSm\"]}" t065 0
```



### Restarting the demo

If you want to re-run the simulations from scratch, you will need to remove the data in Alice, Bob and Charlie folders first, except from their keys. You can do this by running `./scripts/delete_eudico_data.sh`.

To restart the demo run `./scripts/restart-demo.sh`.


## Run kubernetes 20 nodes demo




### On your server

### Initialization (do only once)
Clone fork:

```
$ git clone https://github.com/sa8/B2-eudico-devnet.git
$ cd B2-eudico-devnet
```


The file `eudico.dockerfile#L5-L7` defines where 
Eudico is build from. /!\ make sure that the repo is "up-to-date" (i.e. regtest if you want regtest, testnet if you want testnet).

Install depedencies (do it only once) 
```
$ sudo apt install make
$ make install_deps
```

<!-- Install kubectl (see https://kubernetes.io/fr/docs/tasks/tools/install-kubectl/) -->
```
$ sudo apt-get update && sudo apt-get install -y apt-transport-https
$ curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add -
$ echo "deb https://apt.kubernetes.io/ kubernetes-xenial main" | sudo tee -a /etc/apt/sources.list.d/kubernetes.list
$ sudo apt-get update
$ sudo apt-get install -y kubectl
```

### Starting the demo
```
$ make start
$ make show_config
```

#### On your computer

Install Lens as a kubernetes client: https://k8slens.dev/desktop.html.

Open Lens and add our newly created cluster by copy-pasting the result of `make show_config` into Lens (File->Add Cluster).

![](https://i.imgur.com/B4fuJv4.png)

Be sure to update the `server` field with your remote machine IP address. (If using AWS you need to authorize the port).

If using Lightsail: replace `certificate-authority-data: ......` with `insecure-skip-tls-verify: true.`

#### On your server again


<!-- #### Start Minio

```
$ kubectl apply -f ./deploy/minio
or
$ make run_minio
```
(Might need sudo) -->

##### Start Bitcoin
```
$ make build_regtest
$ make import_regtest_miner
```

Build bitcoin
```
$ make build_bitcoin
$ make import_bitcoin
```

```
$ kubectl apply -f ./deploy/bitcoin
or
$ make run_bitcoin
```

When re-lauching: you only need to run minio, bitcoin, eudico (no need to build and import).
If eudico has been changed, delete eudico and reimport everything. (`make delete_deployment`, `kubectl delete -f ./deploy/bitcoin`).
To re-launch bitcoin, start from make `build_regtest`.

##### Deploy the 3 first eudico nodes
/!\ Make sure that the correct data are on your eudico repo before deploying eudico.

```
$ make build_eudico
$ make import_eudico
```

```
$ kubectl apply -f ./deploy
or 
$ make run_eudico
```


##### Re-build eudico

To rebuild eudico use the command `make rebuild_eudico` as it builds the docker image without the cache. 


```
$ make rebuild_eudico
$ make import_eudico
```

#### Trigger the first DKG ( 3 nodes)

```
$ eudico send --from t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba --method 2 --params-json "{\"Miners\":[\"12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4\", \"12D3KooWNTyoBdMB9bpSkf7PVWR863ejGVPq9ssaaAipNvhPeQ4t\", \"12D3KooWF1aFCGUtsGEaqNks3QADLUDZxW7ot7jZPSoDiAFKJuM6\"]}" t065 0
```

#### Scale to 20 nodes

To add more nodes you run the following command:
````

$ kubectl scale --replicas=20 -f ./deploy/deployment.yaml

````

#### Trigger the DKG (20 nodes)

You will need to enter `eudico-node-0` shell.
```
$ cat /config/peerID.txt
```
This will show the nodes we are connecting too.

Add all the peer id listed in the `Miners` parameter field : 
```
$ eudico send --from t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba --method 2 --params-json "{\"Miners\":[\"12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4\", \"12D3KooWNTyoBdMB9bpSkf7PVWR863ejGVPq9ssaaAipNvhPeQ4t\", \"12D3KooWF1aFCGUtsGEaqNks3QADLUDZxW7ot7jZPSoDiAFKJuM6\", \"...\"]}" t065 0
```

Or run a script to get the command. Go to eudico/data and type the following:
```
$ cd data
$ vim peerID.txt
# copy the list of IDs to this file
$ python3 add_node_script.py peerID.txt
```
Copy paste the command to node 0 terminal.

### Open MinIO
In Lens, click on the minIO pod. Go in the main container and click on "forward" next to 9001/TCP. Click open in browser.



### Deleting pods
```
$ kubectl delete -f ./deploy/
$ kubectl delete -f ./deploy/bitcoin
$ kubectl delete -f ./deploy/minio
```

### Delete cluster
`k3d cluster delete eudico`

## Developer notes

### Funding bitcoin transaction

In the bitcoin bash script we have hardcoded a first taproot address to prefund our taproot address with some coins.

In case the pre-generated keys or the `gen.gen` (genesis block) are modified, this address should be updated with the new associated one!

https://github.com/Zondax/eudico/blob/zondax/eudico/scripts/taproot.sh#L22
```
curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendtoaddress\", \"params\": [\"bcrt1p4ffl08gtqmc00j3cdc8x9tnqy8u4c0lr8vryny8um605qd78w7vs90n7mx\", 50]}" \
    -H 'Content-Type:application/json'
```
e.g here `bcrt1p4ffl08gtqmc00j3cdc8x9tnqy8u4c0lr8vryny8um605qd78w7vs90n7mx` is associated with our `gen.gen` file that you found (https://github.com/Zondax/filecoin-eudico-devnet/blob/main/gen.gen) same with distributed keys.

### Monitoring Services (not tested)

Using prometheus and grafana for monitoring. The following makefile target will use the configs from `/deploy/monitoring`.

```
$ sudo make run_monitoring
```
The grafana dashboard is running inside the cluster and needs to be exposed in order to access it locally or remotely. To expose grafana is the following command:
```
$ sudo make expose_grafana
```
or using command in case port 3000 is already in use, replace [port] below
```
$ sudo kubectl -n monitoring port-forward --address 0.0.0.0 service/grafana [port]:3000 &
```

### Get Data from Lens

```
 kubectl cp default/eudico-node-22:12D3KooWHxW1LtUsyiVaTgJLYiiEMowb52rRmkHUjRfuncAoESSCdata.txt   data/test-22.txt
```

## Troubleshooting

### Docker error permission

Be sure to have visited the [postintall section](https://docs.docker.com/engine/install/linux-postinstall/) to update your docker permission.

### Cant connect to kubernetes cluster from lens

You need to replace the IP address with the remote IP address when copy/paste the config.



### Crashing at start

In the 4 nodes demo, between each start you whould kill and restart `bitcoind_regtest` to wipe out the precedents transactions linked to checkpointing.

If error is 
```
checkpointing/sub.go:348 couldnt create checkpoint: did not find checkpoint
```

Verify that the bitcoin address written in the `./scripts/taproot.sh`
file is the same as the one output by the nodes in the demo (the bitcoin address is printed when the eudico node start).
If not copy it in `./scripts/taproot.sh`.


If the message error is
```
ERROR: initializing node: starting node: could not build arguments for function "github.com/filecoin-project/lotus/node/modules/lp2p".PstoreAddSelfKeys (/home/ubuntu/eudico/node/modules/lp2p/libp2p.go:97): failed to build peer.ID: could not build arguments for function "reflect".makeFuncStub (/usr/local/go/src/reflect/asm_amd64.s:14): failed to build crypto.PubKey: could not build arguments for function "reflect".makeFuncStub (/usr/local/go/src/reflect/asm_amd64.s:14): failed to build crypto.PrivKey: received non-nil error from function "reflect".makeFuncStub (/usr/local/go/src/reflect/asm_amd64.s:14): permissions of key: 'libp2p-host' are too relaxed, required: 0600, got: 0664
```
you need to run ./scripts/data-permission.sh

### Lens; Bitcoin pending
Try re-doing build/import/run.

### Permission error

If this error 
```
    ERROR: initializing node: starting node: could not build arguments for function "github.com/filecoin-project/lotus/node/modules/lp2p".PstoreAddSelfKeys (/home/ubuntu/eudico/node/modules/lp2p/libp2p.go:97): failed to build peer.ID: could not build arguments for function "reflect".makeFuncStub (/usr/local/go/src/reflect/asm_amd64.s:14): failed to build crypto.PubKey: could not build arguments for function "reflect".makeFuncStub (/usr/local/go/src/reflect/asm_amd64.s:14): failed to build crypto.PrivKey: received non-nil error from function "reflect".makeFuncStub (/usr/local/go/src/reflect/asm_amd64.s:14): permissions of key: 'libp2p-host' are too relaxed, required: 0600, got: 0664
```

Run this
```
$ ./scripts/data-permissions.sh
```

It means the keys files it is trying to load have a permission too relax and they won't run until you make it more restrictive.

### K8s issues
Make sure you got the right data.
You may need to regenerate the genesis file depending on what changed in eudico (e.g. rebasing, new actor, etc).