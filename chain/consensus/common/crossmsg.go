package common

import (
	"context"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

func checkCrossMsg(store blockadt.Store, parentSCA *sca.SCAState, id hierarchical.SubnetID, msg *types.Message) error {
	switch hierarchical.GetMsgType(msg) {
	case hierarchical.Fund:
		// sanity-check: the root chain doesn't support topDown messages,
		// so return an error if parentSCA is nil and we are here.
		if parentSCA == nil {
			return xerrors.Errorf("root chains (id=%v) does not support topDown cross msgs", id)
		}
		return checkTopDownMsg(store, parentSCA, id, msg)
	case hierarchical.Release:
		panic("Not implemented")
	case hierarchical.Cross:
		panic("Not implemented")
	}

	return xerrors.Errorf("Unknown cross-msg type")
}

// checkTopDownMsg validates the topdown message.
// - It checks that the msg nonce is larger than AppliedDownTopNonce
// - It checks that the cross-msg is committed in the sca of the parent chain
func checkTopDownMsg(store blockadt.Store, parentSCA *sca.SCAState, id hierarchical.SubnetID, msg *types.Message) error {
	// Check valid nonce
	if msg.Nonce < parentSCA.AppliedDownTopNonce {
		return xerrors.Errorf("topDown msg nonce below AppliedDownTop nonce")
	}

	// check the message for nonce is committed in sca.
	comMsg, found, err := parentSCA.GetTopDownMsg(store, id, msg.Nonce)
	if err != nil {
		return err
	}
	if !found {
		xerrors.Errorf("Now TopDownMsg found for nonce in parent SCA: %d", msg.Nonce)
	}

	if !comMsg.Equals(msg) {
		xerrors.Errorf("Committed and proposed TopDownMsg for nonce %d not equal", msg.Nonce)
	}

	// NOTE: Any additional check required?
	return nil

}

func parentSCAState(ctx context.Context, sm *stmgr.StateManager, submgr subnet.SubnetMgr, id hierarchical.SubnetID) (*sca.SCAState, blockadt.Store, error) {
	api, err := submgr.GetSubnetAPI(id.Parent())
	subnetAct, err := api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	if err != nil {
		return nil, nil, err
	}
	var st sca.SCAState
	pbs := blockstore.NewAPIBlockstore(api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &st); err != nil {
		return nil, nil, err
	}
	return &st, blockadt.WrapStore(ctx, pcst), nil
}
