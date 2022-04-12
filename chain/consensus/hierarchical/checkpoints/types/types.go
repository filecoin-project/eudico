package types

import (
	"github.com/filecoin-project/go-state-types/abi"
)

type EnvelopeType uint64

const (
	SingleSignature EnvelopeType = iota
)

// CheckpointEpoch returns the epoch of the next checkpoint
// that needs to be signed
//
// Return the template of the checkpoint template that has been
// frozen and that is ready for signing and commitment in the
// current window.
func CheckpointEpoch(epoch abi.ChainEpoch, period abi.ChainEpoch) abi.ChainEpoch {
	ind := epoch / period
	return period * ind
}

// WindowEpoch returns the epoch of the active checkpoint window
//
// Determines the epoch to which new checkpoints and xshard transactions need
// to be assigned.
func WindowEpoch(epoch abi.ChainEpoch, period abi.ChainEpoch) abi.ChainEpoch {
	ind := epoch / period
	return period * (ind + 1)
}
