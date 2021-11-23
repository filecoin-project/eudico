package subnet

import (
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
)

// ActorIface determines the interface to be implemented by every
// actor representing a subnet..
type ActorIface interface {
	Constructor(rt runtime.Runtime, params cbor.Marshaler) cbor.Marshaler
	Join(rt runtime.Runtime, params cbor.Marshaler) cbor.Marshaler
	Leave(rt runtime.Runtime, params cbor.Marshaler) cbor.Marshaler
	Checkpoint(rt runtime.Runtime, params cbor.Marshaler) cbor.Marshaler
	Kill(rt runtime.Runtime, params cbor.Marshaler) cbor.Marshaler
}
