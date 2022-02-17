package subnetmgr

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	ctypes "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

func (s *SubnetMgr) SubmitSignedCheckpoint(
	ctx context.Context, wallet address.Address,
	id address.SubnetID, ch *schema.Checkpoint) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	SubnetActor, err := id.Actor()
	if err != nil {
		return cid.Undef, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	b, err := ch.MarshalBinary()
	if err != nil {
		return cid.Undef, err
	}
	params := &sca.CheckpointParams{Checkpoint: b}
	serparams, err := actors.SerializeParams(params)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed serializing init actor params: %s", err)
	}

	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:       SubnetActor,
		From:     wallet,
		Value:    abi.NewTokenAmount(0),
		Method:   subnet.Methods.SubmitCheckpoint,
		Params:   serparams,
		GasLimit: 1_000_000_000, // NOTE: Adding high gas limit to ensure that the message is accepted.
	}, nil)
	if aerr != nil {
		log.Errorf("Error MpoolPushMessage: %s", aerr)
		return cid.Undef, aerr
	}

	msg := smsg.Cid()

	chcid, _ := ch.Cid()
	log.Infow("Success signing checkpoint in subnet", "subnetID", id, "message", msg, "cid", chcid)
	return smsg.Cid(), nil
}

func (s *SubnetMgr) ListCheckpoints(
	ctx context.Context, id address.SubnetID, num int) ([]*schema.Checkpoint, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	subnetActAddr, err := id.Actor()
	if err != nil {
		return nil, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return nil, err
	}

	subAPI := s.getAPI(id)
	if subAPI == nil {
		return nil, xerrors.Errorf("Not listening to subnet")
	}

	subnetAct, err := parentAPI.StateGetActor(ctx, subnetActAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	var snst subnet.SubnetState
	pbs := blockstore.NewAPIBlockstore(parentAPI)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &snst); err != nil {
		return nil, err
	}
	pstore := adt.WrapStore(ctx, pcst)
	out := make([]*schema.Checkpoint, 0)
	ts := subAPI.ChainAPI.Chain.GetHeaviestTipSet()
	currEpoch := ts.Height()
	for i := 0; i < num; i++ {
		signWindow := ctypes.CheckpointEpoch(currEpoch, snst.CheckPeriod)
		signWindow = abi.ChainEpoch(int(signWindow) - i*int(snst.CheckPeriod))
		if signWindow < 0 {
			break
		}
		ch, found, err := snst.GetCheckpoint(pstore, signWindow)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (s *SubnetMgr) ValidateCheckpoint(
	ctx context.Context, id address.SubnetID, epoch abi.ChainEpoch) (*schema.Checkpoint, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get actor from subnet ID
	subnetActAddr, err := id.Actor()
	if err != nil {
		return nil, err
	}

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return nil, err
	}

	subAPI := s.getAPI(id)
	if subAPI == nil {
		xerrors.Errorf("Not listening to subnet")
	}

	subnetAct, err := parentAPI.StateGetActor(ctx, subnetActAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	var snst subnet.SubnetState
	pbs := blockstore.NewAPIBlockstore(parentAPI)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &snst); err != nil {
		return nil, err
	}
	pstore := adt.WrapStore(ctx, pcst)
	ts := subAPI.ChainAPI.Chain.GetHeaviestTipSet()

	// If epoch < 0 we are singalling that we want to verify the
	// checkpoint for the latest epoch submitted.
	if epoch < 0 {
		currEpoch := ts.Height()
		epoch = ctypes.CheckpointEpoch(currEpoch-snst.CheckPeriod, snst.CheckPeriod)
	}

	ch, found, err := snst.GetCheckpoint(pstore, epoch)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, xerrors.Errorf("no checkpoint committed in epoch: %s", epoch)
	}
	prevCid, err := snst.PrevCheckCid(pstore, epoch)
	if err != nil {
		return nil, err
	}

	if pchc, _ := ch.PreviousCheck(); prevCid != pchc {
		return ch, xerrors.Errorf("verification failed, previous checkpoints not equal: %s, %s", prevCid, pchc)
	}

	if ch.Epoch() != epoch {
		return ch, xerrors.Errorf("verification failed, wrong epoch: %s, %s", ch.Epoch(), epoch)
	}

	subts, err := subAPI.ChainGetTipSetByHeight(ctx, epoch, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	if !ch.EqualTipSet(subts.Key()) {
		chtsk, _ := ch.TipSet()
		return ch, xerrors.Errorf("verification failed, checkpoint includes wrong tipSets : %s, %s", ts.Key(), chtsk)
	}

	// TODO: Verify that the checkpoint has been committed in the corresponding SCA as a sanity check.
	// TODO: Verify that committed childs are correct
	// TODO: Any other verification?
	return ch, nil
}
