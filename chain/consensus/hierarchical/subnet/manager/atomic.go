package subnetmgr

import (
	"bytes"
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/atomic"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
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

// XXX: TODO: Add inputs, how to pass the input to init the execution?
func (s *SubnetMgr) InitAtomicExec(
	ctx context.Context, wallet address.Address, inputs []sca.LockedState,
	msgs []types.Message, method abi.MethodNum) (cid.Cid, error) {

	// Compute common parent of subnets.
	cp := inputs[0].From
	for _, i := range inputs[1:] {
		cp, _ = cp.CommonParent(i.From)
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
