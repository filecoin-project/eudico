package subnetmgr

import (
	"bytes"
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

func (s *SubnetMgr) LockState(
	ctx context.Context, wallet address.Address, actor address.Address,
	subnet address.SubnetID, method abi.MethodNum) (cid.Cid, error) {

	sapi, err := s.GetSubnetAPI(subnet)
	if err != nil {
		return cid.Undef, err
	}

	// FIXME: Disregarding params to lock for now
	lpm, err := atomic.WrapSerializedParams(method, []byte{})
	if err != nil {
		return cid.Undef, err
	}
	serparams, err := actors.SerializeParams(lpm)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed serializing init actor params: %s", err)
	}

	smsg, aerr := sapi.MpoolPushMessage(ctx, &types.Message{
		To:     actor,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: atomic.MethodLock,
		Params: serparams,
	}, nil)
	if aerr != nil {
		return cid.Undef, aerr
	}

	msg := smsg.Cid()
	mw, aerr := sapi.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return cid.Undef, aerr
	}

	r := &atomic.LockedOutput{}
	if err := r.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		return cid.Undef, xerrors.Errorf("error unmarshalling locked output: %s", err)
	}
	return r.Cid, nil
}

func (s *SubnetMgr) InitAtomicExec(
	ctx context.Context, wallet address.Address, inputs map[string]sca.LockedState,
	msgs []types.Message) (cid.Cid, error) {

	// Compute common parent of subnets.
	cp, err := sca.GetCommonParentForExec(inputs)
	if err != nil {
		return cid.Undef, err
	}

	sapi, err := s.GetSubnetAPI(cp)
	if err != nil {
		return cid.Undef, err
	}

	ipm := &sca.AtomicExecParams{Inputs: inputs, Msgs: msgs}
	serparams, err := actors.SerializeParams(ipm)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed serializing init actor params: %s", err)
	}

	smsg, aerr := sapi.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: sca.Methods.InitAtomicExec,
		Params: serparams,
	}, nil)
	if aerr != nil {
		return cid.Undef, aerr
	}

	msg := smsg.Cid()
	mw, aerr := sapi.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return cid.Undef, aerr
	}

	r := &atomic.LockedOutput{}
	if err := r.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		return cid.Undef, xerrors.Errorf("error unmarshalling locked output: %s", err)
	}
	return r.Cid, nil
}

func (s *SubnetMgr) ListAtomicExecs(
	ctx context.Context, id address.SubnetID, addr address.Address) ([]*sca.AtomicExec, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	api, err := s.GetSubnetAPI(id)
	if err != nil {
		return nil, err
	}

	scaAct, err := api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	var st sca.SCAState
	pbs := blockstore.NewAPIBlockstore(api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, scaAct.Head, &st); err != nil {
		return nil, err
	}
	pstore := adt.WrapStore(ctx, pcst)
	m, err := st.ListExecs(pstore, addr)
	if err != nil {
		return nil, err
	}
	out := make([]*sca.AtomicExec, 0)
	for _, v := range m {
		out = append(out, &v)
	}
	return out, nil
}

func getAtomicExec(ctx context.Context, api *API, c cid.Cid) (*sca.AtomicExec, bool, error) {
	var st sca.SCAState
	scaAct, err := api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	if err != nil {
		return nil, false, err
	}
	pbs := blockstore.NewAPIBlockstore(api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, scaAct.Head, &st); err != nil {
		return nil, false, err
	}
	pstore := adt.WrapStore(ctx, pcst)
	return st.GetAtomicExec(pstore, c)
}

func (s *SubnetMgr) ComputeAndSubmitExec(ctx context.Context, wallet address.Address,
	id address.SubnetID, execID cid.Cid) (sca.ExecStatus, error) {

	panic("not implemented yet")
	/*
		sapi := s.getAPI(id)
		if sapi == nil {
			return sca.ExecUndefState, xerrors.Errorf("not syncing with subnet")
		}
		// Check if the exec exists.
		ae, found, err := getAtomicExec(ctx, sapi, execID)
		if err != nil {
			return sca.ExecUndefState, err
		}
		if !found {
			return sca.ExecUndefState, xerrors.Errorf("execution not found in subnet for cid")
		}
			// FIXME: Make this state to populate configurable.
			actSt := replace.ReplaceState{}
			// TODO: Get locked states for the other subnets for the execution.
			err = exec.ComputeAtomicOutput(ctx, sapi.StateManager, actSt, lockedm, ae.Params.Msgs)
			if err != nil {
				return sca.ExecUndefState, err
			}

			spm := &sca.SubmitExecParams{Cid: execID.String(), Output: output}
			serparams, err := actors.SerializeParams(spm)
			if err != nil {
				return sca.ExecUndefState, xerrors.Errorf("failed serializing init actor params: %s", err)
			}

			smsg, aerr := sapi.MpoolPushMessage(ctx, &types.Message{
				To:     hierarchical.SubnetCoordActorAddr,
				From:   wallet,
				Value:  abi.NewTokenAmount(0),
				Method: sca.Methods.SubmitAtomicExec,
				Params: serparams,
			}, nil)
			if aerr != nil {
				return sca.ExecUndefState, aerr
			}

			msg := smsg.Cid()
			mw, aerr := sapi.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
			if aerr != nil {
				return sca.ExecUndefState, aerr
			}

			r := &sca.SubmitOutput{}
			if err := r.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
				return sca.ExecUndefState, xerrors.Errorf("error unmarshalling output: %s", err)
			}
			return r.Status, nil
	*/
}
func (s *SubnetMgr) AbortAtomicExec(ctx context.Context, wallet address.Address,
	id address.SubnetID, execID cid.Cid) (sca.ExecStatus, error) {

	sapi := s.getAPI(id)
	if sapi == nil {
		return sca.ExecUndefState, xerrors.Errorf("not syncing with subnet")
	}
	// Check if the exec exists.
	_, found, err := getAtomicExec(ctx, sapi, execID)
	if err != nil {
		return sca.ExecUndefState, err
	}
	if !found {
		return sca.ExecUndefState, xerrors.Errorf("execution not found in subnet for cid")
	}
	spm := &sca.SubmitExecParams{Cid: execID.String(), Abort: true}
	serparams, err := actors.SerializeParams(spm)
	if err != nil {
		return sca.ExecUndefState, xerrors.Errorf("failed serializing init actor params: %s", err)
	}

	smsg, aerr := sapi.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: sca.Methods.SubmitAtomicExec,
		Params: serparams,
	}, nil)
	if aerr != nil {
		return sca.ExecUndefState, aerr
	}

	msg := smsg.Cid()
	mw, aerr := sapi.StateWaitMsg(ctx, msg, build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return sca.ExecUndefState, aerr
	}

	r := &sca.SubmitOutput{}
	if err := r.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		return sca.ExecUndefState, xerrors.Errorf("error unmarshalling output: %s", err)
	}
	return r.Status, nil
}
