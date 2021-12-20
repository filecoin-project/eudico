package hierarchical

// ConsensusType for subnet
type ConsensusType uint64

// List of supported/implemented consensus for subnets.
const (
	Delegated ConsensusType = iota
	PoW
)
