package subnetmgr

import (
	"bytes"
	"context"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	replace "github.com/filecoin-project/lotus/chain/consensus/actors/atomic-replace"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic/exec"
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

	// FIXME: Make this timeout configurable.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sapi := s.getAPI(id)
	if sapi == nil {
		return sca.ExecUndefState, xerrors.Errorf("not syncing with subnet: %s", id)
	}
	// Check if the exec exists.
	ae, found, err := getAtomicExec(ctx, sapi, execID)
	if err != nil {
		return sca.ExecUndefState, err
	}
	if !found {
		return sca.ExecUndefState, xerrors.Errorf("execution not found in subnet for cid")
	}

	// Getting locked state
	log.Infof("Resolving locked state for off-chain execution")
	locked, toSn, toActor, err := s.resolveLockedStates(ctx, wallet, ae)
	if err != nil {
		return sca.ExecUndefState, err
	}
	log.Debugf("Resolved locked states: %s, %s (err=%s)", locked, toActor, err)

	// getting API for subnet where execution state lives.
	execApi := s.getAPI(toSn)
	if execApi == nil {
		return sca.ExecUndefState, xerrors.Errorf("not syncing with subnet: %s", toSn)
	}
	// get heaviest tipset
	ts := execApi.ChainAPI.Chain.GetHeaviestTipSet()

	// FIXME: Make this state to populate configurable.
	actSt := &replace.ReplaceState{}
	err = exec.ComputeAtomicOutput(ctx, execApi.StateManager, ts, toActor, actSt, locked, ae.Params.Msgs)
	if err != nil {
		return sca.ExecUndefState, err
	}

	// FIXME: Make output state configurable
	spm := &sca.SubmitExecParams{Cid: execID.String(), Output: *actSt.Owners}

	// submit output
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

func (s *SubnetMgr) resolveLockedStates(ctx context.Context, wallet address.Address, ae *sca.AtomicExec) ([]atomic.LockableState, address.SubnetID, address.Address, error) {
	locked := make([]atomic.LockableState, 0)
	var (
		actor address.Address
		sub   address.SubnetID
	)
	for k, in := range ae.Params.Inputs {
		addr, err := address.NewFromString(k)
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, err
		}
		raddr, err := addr.RawAddr()
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, err
		}
		sn, err := addr.Subnet()
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, err
		}
		if raddr == wallet {
			actor = in.Actor
			sub = sn
			continue
		}
		c, err := cid.Parse(in.Cid)
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, err
		}
		res := s.r.WaitLockedStateResolved(ctx, c, sn, in.Actor)
		err = <-res
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, xerrors.Errorf("error resolving locked state: %w", err)
		}
		l, found, err := s.r.ResolveLockedState(ctx, c, sn, in.Actor)
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, err
		}
		if !found {
			return nil, address.UndefSubnetID, address.Undef, xerrors.Errorf("couldn't resolve locked state from subnet")
		}

		// FIXME: Make this configurable
		own := &replace.Owners{}
		err = atomic.UnwrapLockableState(l, own)
		if err != nil {
			return nil, address.UndefSubnetID, address.Undef, err
		}
		locked = append(locked, own)
	}
	if sub == address.UndefSubnetID || actor == address.Undef {
		return nil, sub, actor, xerrors.Errorf("wallet not involved in execution, couldn't find target actor and subnet")
	}
	return locked, sub, actor, nil
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
