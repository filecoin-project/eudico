package common

import (
	"context"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

func checkCrossMsg(pstore, snstore blockadt.Store, parentSCA, snSCA *sca.SCAState, msg *types.Message) error {
	switch hierarchical.GetMsgType(msg) {
	case hierarchical.Fund:
		// sanity-check: the root chain doesn't support topDown messages,
		// so return an error if parentSCA is nil and we are here.
		if parentSCA == nil {
			return xerrors.Errorf("root chains (id=%v) does not support topDown cross msgs", snSCA.NetworkName)
		}
		return checkTopDownMsg(pstore, parentSCA, snSCA, msg)
	case hierarchical.Release:
		panic("Not implemented")
	case hierarchical.Cross:
		panic("Not implemented")
	}

	return xerrors.Errorf("Unknown cross-msg type")
}

// checkTopDownMsg validates the topdown message.
// - It checks that the msg nonce is larger than AppliedBottomUpNonce in the subnet SCA
// Recall that applying crossMessages increases the AppliedNonce of the SCA in the subnet
// where the message is applied.
// - It checks that the cross-msg is committed in the sca of the parent chain
func checkTopDownMsg(pstore blockadt.Store, parentSCA, snSCA *sca.SCAState, msg *types.Message) error {
	// Check valid nonce in subnet where message is applied.
	if msg.Nonce < snSCA.AppliedBottomUpNonce {
		return xerrors.Errorf("topDown msg nonce reuse in subnet")
	}

	// check the message for nonce is committed in sca.
	comMsg, found, err := parentSCA.GetTopDownMsg(pstore, snSCA.NetworkName, msg.Nonce)
	if err != nil {
		return xerrors.Errorf("getting topDown msgs: %w", err)
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

func ApplyCrossMsg(ctx context.Context, vmi *vm.VM, submgr subnet.SubnetMgr,
	em stmgr.ExecMonitor, msg *types.Message,
	ts *types.TipSet, netName dtypes.NetworkName) error {
	switch hierarchical.GetMsgType(msg) {
	case hierarchical.Fund:
		return applyFundMsg(ctx, vmi, submgr, em, msg, ts, netName)
	case hierarchical.Release:
		panic("Not implemented")
	case hierarchical.Cross:
		panic("Not implemented")
	}

	return xerrors.Errorf("Unknown cross-msg type")
}

func applyFundMsg(ctx context.Context, vmi *vm.VM, submgr subnet.SubnetMgr,
	em stmgr.ExecMonitor, msg *types.Message, ts *types.TipSet,
	netName dtypes.NetworkName) error {
	// sanity-check: the root chain doesn't support topDown messages,
	// so return an error if submgr is nil.
	if submgr == nil {
		return xerrors.Errorf("Root chain doesn't have parent and doesn't support topDown cross msgs")
	}

	// Get SECPK address for ID from parent chain included in message.
	id := hierarchical.SubnetID(netName)
	api, err := submgr.GetSubnetAPI(id.Parent())
	if err != nil {
		return xerrors.Errorf("getting subnet API: %w", err)
	}
	secpaddr, err := api.StateAccountKey(ctx, msg.From, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting secp address: %w", err)
	}
	// Translating parent actor ID of address to SECPK for its application
	// in subnet.
	msg.From = secpaddr
	msg.To = secpaddr
	return applyMsg(ctx, vmi, em, msg, ts)
}

func applyMsg(ctx context.Context, vmi *vm.VM, em stmgr.ExecMonitor,
	msg *types.Message, ts *types.TipSet) error {
	// Serialize params
	params := &sca.ApplyParams{
		Msg: *msg,
	}
	serparams, err := actors.SerializeParams(params)
	if err != nil {
		return xerrors.Errorf("failed serializing init actor params: %s", err)
	}
	apply := &types.Message{
		From:       builtin.SystemActorAddr,
		To:         hierarchical.SubnetCoordActorAddr,
		Nonce:      msg.Nonce,
		Value:      big.Zero(),
		GasFeeCap:  types.NewInt(0),
		GasPremium: types.NewInt(0),
		GasLimit:   1 << 30,
		Method:     sca.Methods.ApplyMessage,
		Params:     serparams,
	}

	// If the destination account hasn't been initialized, init the account actor.
	st := vmi.StateTree()
	_, acterr := st.GetActor(params.Msg.To)
	if acterr != nil {
		log.Debugw("Initializing To address for crossmsg", "address", params.Msg.To)
		_, _, err := vmi.CreateAccountActor(ctx, apply, params.Msg.To)
		if err != nil {
			return xerrors.Errorf("failed to initialize address for crossmsg: %w", err)
		}
	}

	ret, actErr := vmi.ApplyImplicitMessage(ctx, apply)
	if actErr != nil {
		return xerrors.Errorf("failed to apply cross message :%w", actErr)
	}
	if em != nil {
		if err := em.MessageApplied(ctx, ts, apply.Cid(), apply, ret, true); err != nil {
			return xerrors.Errorf("callback failed on reward message: %w", err)
		}
	}

	if ret.ExitCode != 0 {
		return xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
	}
	log.Debugw("Applied cross msg implicitly (original msg Cid)", "cid", msg.Cid())
	return nil
}

func getSCAState(ctx context.Context, sm *stmgr.StateManager, submgr subnet.SubnetMgr, id hierarchical.SubnetID) (*sca.SCAState, blockadt.Store, error) {

	var st sca.SCAState
	// if submgr == nil we are in root, so we can load the actor using the state manager.
	if submgr == nil {
		ts := sm.ChainStore().GetHeaviestTipSet()
		subnetAct, err := sm.LoadActor(ctx, hierarchical.SubnetCoordActorAddr, ts)
		if err != nil {
			return nil, nil, xerrors.Errorf("loading actor state: %w", err)
		}
		if err := sm.ChainStore().ActorStore(ctx).Get(ctx, subnetAct.Head, &st); err != nil {
			return nil, nil, xerrors.Errorf("getting actor state: %w", err)
		}
		return &st, sm.ChainStore().ActorStore(ctx), nil
	}

	api, err := submgr.GetSubnetAPI(id)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting subnet API: %w", err)
	}
	subnetAct, err := api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading actor state: %w", err)
	}
	pbs := blockstore.NewAPIBlockstore(api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &st); err != nil {
		return nil, nil, xerrors.Errorf("getting actor state: %w", err)
	}
	return &st, blockadt.WrapStore(ctx, pcst), nil
}
