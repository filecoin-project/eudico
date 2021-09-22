package sharding

import (
	"context"
	"fmt"
	"reflect"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/libp2p/go-libp2p-core/host"
)

type ShardingSub struct {
	events *events.Events
	api    api.FullNode

	host *host.Host
}

func NewShardSub(api impl.FullNodeAPI, host *host.Host) (*ShardingSub, error) {
	e, err := events.NewEvents(context.TODO(), &api)
	if err != nil {
		return nil, err
	}

	return &ShardingSub{
		events: e,
		api:    &api,
		host: host,
	}, nil
}

func (s *ShardingSub) Start() {
	ctx := context.TODO()

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		fmt.Println(fmt.Sprintf("STATE CHANGE %#v", states))

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		oldAct, err := s.api.StateGetActor(ctx, delegcns.ShardActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}
		newAct, err := s.api.StateGetActor(ctx, delegcns.ShardActorAddr, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		var oldSt, newSt delegcns.ShardState

		bs := blockstore.NewAPIBlockstore(s.api)
		cst := cbor.NewCborStore(bs)
		if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
			return false, nil, err
		}
		if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
			return false, nil, err
		}

		if reflect.DeepEqual(newSt, oldSt) {
			return false, nil, nil
		}

		diff := map[string]struct{}{}
		for _, shard := range newSt.Shards {
			diff[string(shard)] = struct{}{}
		}
		for _, shard := range oldSt.Shards {
			delete(diff, string(shard))
		}
		return true, diff, nil
	}

	err := s.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}
