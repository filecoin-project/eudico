package manager

import (
	"context"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

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
)

// signingState keeps track of checkpoint signing state for an epoch.
type signingState struct {
	wait        abi.ChainEpoch
	currEpoch   abi.ChainEpoch
	signed      bool
	checkBuffer map[abi.ChainEpoch]*schema.Checkpoint
}

func newSigningState() *signingState {
	return &signingState{
		checkBuffer: make(map[abi.ChainEpoch]*schema.Checkpoint),
	}
}

// getPrevCheck checks if a high-confidence prevCheck cid can be retrieved
// subnet state or we need to use the one from our checkBuffer.
func (sh *Subnet) getPrevCheck(store adt.Store, st *subnet.SubnetState, epoch abi.ChainEpoch) (cid.Cid, error) {
	sh.checklk.RLock()
	defer sh.checklk.RUnlock()
	ep := epoch - st.CheckPeriod
	prevCp, found, err := st.GetCheckpoint(store, ep)
	if err != nil {
		return cid.Undef, err
	}
	if !found {
		// if not use the previous from buffer
		prevCp, ok := sh.signingState.checkBuffer[ep]
		if !ok {
			// If nothing found in state or buffer
			return schema.NoPreviousCheck, nil
		}
		return prevCp.Cid()

	}
	return prevCp.Cid()
}

// gc the check buffer to prevent it from growing indefinitely
func (sh *Subnet) gcCheckBuf(store adt.Store, st *subnet.SubnetState, epoch abi.ChainEpoch) {
	ep := epoch - st.CheckPeriod
	for ep >= 0 {
		_, found, err := st.GetCheckpoint(store, ep)
		if err != nil {
			log.Errorf("error getting checkpoint in gc: %w", err)
			return
		}
		if found {
			sh.checklk.Lock()
			// remove if it is in buffer
			_, ok := sh.signingState.checkBuffer[ep]
			if !ok {
				// past cleaned
				sh.checklk.Unlock()
				return
			}
			delete(sh.signingState.checkBuffer, ep)
			sh.checklk.Unlock()
		}
		ep = ep - st.CheckPeriod
	}
}

func (sh *Subnet) resetSigState(epoch abi.ChainEpoch) {
	sh.checklk.Lock()
	defer sh.checklk.Unlock()
	sh.signingState.currEpoch = epoch
	sh.signingState.wait = 0
	sh.signingState.signed = false
}

func (sh *Subnet) sigWaitTick() {
	sh.checklk.Lock()
	defer sh.checklk.Unlock()
	sh.signingState.wait++
}

func (sh *Subnet) signed(ch *schema.Checkpoint) {
	sh.checklk.Lock()
	defer sh.checklk.Unlock()
	sh.signingState.signed = true
	sh.signingState.checkBuffer[ch.Epoch()] = ch
}

func (sh *Subnet) hasSigned() bool {
	sh.checklk.RLock()
	defer sh.checklk.RUnlock()
	return sh.signingState.signed
}

func (sh *Subnet) sigWaitReached() bool {
	sh.checklk.RLock()
	defer sh.checklk.RUnlock()

	return sh.signingState.wait >= sh.finalityThreshold
}

func (sh *Subnet) sigWindow() abi.ChainEpoch {
	sh.checklk.RLock()
	defer sh.checklk.RUnlock()
	return sh.signingState.currEpoch
}

// PopulateCheckpoint sets previous checkpoint and tipsetKey.
func (sh *Subnet) populateCheckpoint(ctx context.Context, store adt.Store, st *subnet.SubnetState, ch *schema.Checkpoint) error {
	// Set Previous.
	prevCid, err := sh.getPrevCheck(store, st, ch.Epoch())
	if err != nil {
		return err
	}
	ch.SetPrevious(prevCid)

	// Set tipsetKeys for the epoch.
	ts, err := sh.api.ChainGetTipSetByHeight(ctx, ch.Epoch(), types.EmptyTSK)
	if err != nil {
		return err
	}
	ch.SetTipsetKey(ts.Key())
	return nil
}

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
