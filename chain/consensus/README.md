# Consensus Interface

`Consensus interface` is an interface that is used to agree on the next block using the concrete implementation of a consensus protocol.
Eudico has several implementations of different BFT-type and Nakamoto-type consensus protocols for research purposes. 

## How to Add a New Consensus Protocol to Eudico
1. Register a consensus type constant in `chain/consensus/hierarchical/types.go`
2. Instantiate a consensus and the corresponding miner for a subnet via `New()`, `Wight()`, and `Mine()` functions of `chain/consensus/hierarchical/subnet/consensus/consensus.go`
3. Create a new file with implementations of genesis block functions in `chain/consensus/hierarchical/actors/subnet/`
4. Implement the `Consensus` interface defined in `chain/consensus/iface.go` for the target consensus protocol
5. Add the corresponding CLI commands in `cmd/eudico/$CONSENSUS.go`
6. Adapt [bad blocks cache](https://github.com/filecoin-project/eudico/blob/0306742e553f6bd6260332b501bb65a5bfc16a76/chain/sync.go#L725) for the consensus protocol if needed.
   For example, it may be necessary to not process blocks if consensus RPCs are unreachable.

