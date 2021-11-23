package subnet

import (
	"context"
	"reflect"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	cbor "github.com/ipfs/go-ipld-cbor"
)

// listenSCAEvents is the routine responsible for listening to events in
// the SCA of each subnet.
//
// TODO: This is a placeholder. with what we have implemented so far there
// is no need to synchronously listen to new events. We'll need this once
// we propagate checkpoints and support cross-subnet transactions.
func (s *SubnetMgr) listenSCAEvents(ctx context.Context, sh *Subnet) {
	api := s.api
	evs := s.events
	id := hierarchical.RootSubnet

	// If subnet is nil, we are listening from the root chain.
	// TODO: Revisit this, there is probably a most elegan way to
	// do this.
	if sh != nil {
		id = sh.ID
		api = sh.api
		evs = sh.events
	}

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for SCA in subnet", "subnetID", id)

		// Trigger the detected change in subnets.
		return s.triggerChange(ctx, api, struct{}{})

	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		oldAct, err := api.StateGetActor(ctx, sca.SubnetCoordActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}
		newAct, err := api.StateGetActor(ctx, sca.SubnetCoordActorAddr, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		var oldSt, newSt sca.SCAState

		bs := blockstore.NewAPIBlockstore(api)
		cst := cbor.NewCborStore(bs)
		if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
			return false, nil, err
		}
		if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
			return false, nil, err
		}

		// If there was some change in the state, for now, trigger change function.
		if !reflect.DeepEqual(newSt, oldSt) {
			return true, nil, nil
		}

		return false, nil, nil

	}

	err := evs.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (s *SubnetMgr) triggerChange(ctx context.Context, api *API, diff struct{}) (more bool, err error) {
	log.Warnw("No logic implemented yet when SCA changes are detected", "subnetID", api.NetworkName)
	// TODO: This will be populated when checkpointing and cross-subnet transactions come.
	return true, nil
}
