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
```
### Tendermint
```
tendermint init validator
tendermint start
go run ./cmd/tendermint
curl -s 'localhost:26657/abci_query?data="1"'
```
