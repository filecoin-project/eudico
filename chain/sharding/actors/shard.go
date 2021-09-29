package actor

import (
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/specs-actors/v5/actors/builtin"
	"github.com/filecoin-project/specs-actors/v5/actors/runtime"
	cid "github.com/ipfs/go-cid"
)

var _ runtime.VMActor = ShardActor{}

var ShardActorAddr = func() addr.Address {
	a, err := addr.NewIDAddress(64)
	if err != nil {
		panic(err)
	}
	return a
}()

type ShardState struct {
	Shards [][]byte
}

func ConstructShardState() *ShardState {
	return &ShardState{}
}

type ShardActor struct{}

func (a ShardActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.Add,
	}
}

func (a ShardActor) Code() cid.Cid {
	return ShardActorCodeID
}

func (a ShardActor) IsSingleton() bool {
	return false
}

func (a ShardActor) State() cbor.Er {
	return new(ShardState)
}

var _ runtime.VMActor = SplitActor{}

func (a ShardActor) Constructor(rt runtime.Runtime, params *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	rt.StateCreate(ConstructShardState())
	return nil
}

func (a ShardActor) Add(rt runtime.Runtime, params *ShardState) *abi.EmptyValue {
	rt.ValidateImmediateCallerAcceptAny()
	var st ShardState
	rt.StateTransaction(&st, func() {
		st.Shards = append(st.Shards, params.Shards...)
	})
	return nil
}
