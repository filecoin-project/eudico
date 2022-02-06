# Tendermint Consensus Integration

## How to Add Consensus to Eudico
 - Register a consensus constant in `chain/consensus/hierarchical/types.go`
 - Instantiate a consensus miner in a subnet in `chain/consensus/hierarchical/subnet/consensus/consensus.go`
 - Implement genesis block functions in `chain/consensus/hierarchical/actors/subnet/tendermint.go`
 - Return the consensus' `TipsExecutor` and `Weight` in `chain/consensus/hierarchical/subnet/utils.go`
 - Decide how to compute a state and implement the corresponding logic in `chain/consensus/$CONSENSUS/compute_state.go`
 - Implement `Consensus interface` defined in `chain/consensus/iface.go` for the consensus algorithm
 - Add the corresmonding CLI commands in `cmd/eudico/$CONSENSUS.go`

## Commands
### Eudico
```
./eudico tspow daemon
./eudico wallet new secp256k1
#./eudico delegated genesis f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy gen.gen
./eudico tendermint genesis  gen.gen
./eudico wallet import ./f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key
./eudico send t1sj56f45kttzepbo7rq3mxlvn3alwc6sp4h2jbmi 1

```
### Tendermint
```
tendermint init validator
tendermint start
go run ./cmd/tendermint
curl -s 'localhost:26657/abci_query?data="1"'

curl -s 'http://localhost:26657/broadcast_tx_sync?tx=0x828a0055017642efe6162dfc3e4e01df770743f22bf903e04455017642efe6162dfc3e4e01df770743f22bf903e0440049000de0b6b3a76400001a00084873450018aef1bd44000187c600405842018172eb88f4f9a59a1e0f0b820d69681403b69a129daed4831729336c6534036b701e4b22572f19c3e89a7341fc4e435ae8b7accf75cf7b3d1e1200108af7640c01'

```

### Subnet
```
 ./eudico subnet add --consensus 2 --name tndr
 ./eudico subnet join --subnet=/root/t01001 10
 ./eudico subnet mine  --subnet=/root/t01001
 ./eudico subnet fund --from=f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy --subnet=/root/t01001 10
 ./eudico --subnet-api=/root/t01001 wallet list
 
 ./eudico subnet list-subnets
```
