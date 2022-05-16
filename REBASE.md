# REBASE NOTES
This documents includes a set of things to bear-in-mind/double-check when rebasing from `filecoin-project/lotus`.
These changes need to be done manually, and not performing them may lead to bugs hard to track. 
- `chain/consensus/genesis/` is a copy of the package `chain/gen/genesis` slightly modified
and use to generate the genesis files of subnets in-the-fly while avoiding dependency cycles. If rebasing any change
affects `chain/gen/genesis` it needs to be appropriately propagated to `chain/consensus/genesis/`.
- `chain/consensus/common/executor.go` implements the message execution logic for all Eudico consensus algorithms
except for filecoin consensus which is implemented in `chain/consensus/common/filcns_executor.go`. Changes in lotus
over `chain/consensus/compute_state.go` need to be propagated also to the aforementioned packages. 
- `chain/common/cns_validation` includes all common consensus validation rules for all consensus algorithms. Any new rule or change introduced to consensus validation rules in `chain/consensus/filcns/filecoin.go` need to
be propagated to the aforementioned file.
