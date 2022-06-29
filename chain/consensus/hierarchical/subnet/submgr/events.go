package submgr

import (
	"context"

	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	checkpoint "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	ctypes "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/types"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
)

const Timeout = 76587687658765876

// struct used to propagate detected changes.
type diffInfo struct {
	checkToSign *signInfo
}

// signInfo propagates signing information.
type signInfo struct {
	checkpoint *schema.Checkpoint
	addr       address.Address
	idAddr     address.Address
}

// listenSubnetEvents is the routine responsible for listening to events
//
// This routine listens mainly for the following events:
// * Pending checkpoints to sign if we are miners in a subnet.
// * New checkpoints for child chains committed in SCA of the subnet.
func (s *Service) listenSubnetEvents(ctx context.Context, sh *Subnet) {
	evs := s.events
	id := address.RootSubnet
	root := true
	finalityThreshold := hierarchical.MinFinality(s.api.Consensus.Type())

	// If subnet is nil, we are listening from the root chain.
	// TODO: Revisit this, there is probably a more elegant way to
	// do this.
	if sh != nil {
		root = false
		id = sh.ID
		evs = sh.events
		sh.resetSigState(abi.ChainEpoch(0))
		finalityThreshold = sh.finalityThreshold
	}

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for subnet", "subnetID", id)
		diff, ok := states.(*diffInfo)
		if !ok {
			log.Error("Error casting states, not of type *diffInfo")
			return true, err
		}

		// Trigger the detected change in subnets.
		return s.triggerChange(ctx, sh, diff)

	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		diff := &diffInfo{}
		change := false
		var err error

		// Root chain checkpointing process is independent from hierarchical consensus
		// so there's no need for checking if there is something to sign in root.
		if !root {
			change, err = s.matchCheckpointSignature(ctx, sh, newTs, diff)
			if err != nil {
				log.Errorw("Error checking checkpoints to sign in subnet", "subnetID", id, "err", err)
				return false, nil, err
			}
		}

		return change, diff, nil

	}

	err := evs.StateChanged(checkFunc, changeHandler, revertHandler, int(finalityThreshold), Timeout, match)
	if err != nil {
		log.Errorw("Error getting state changed", "err", err)
	}
}

func (s *Service) matchCheckpointSignature(ctx context.Context, sh *Subnet, newTs *types.TipSet, diff *diffInfo) (bool, error) {
	// Get the epoch for the current tipset in subnet.
	subnetEpoch := newTs.Height()

	subnetActAddr, err := sh.ID.Actor()
	if err != nil {
		return false, err
	}
	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(sh.ID)
	if err != nil {
		return false, err
	}

	// Get state of subnet actor in parent for heaviest tipset
	subnetAct, err := parentAPI.StateGetActor(ctx, subnetActAddr, types.EmptyTSK)
	if err != nil {
		return false, err
	}

	var snst subnet.SubnetState
	pbs := blockstore.NewAPIBlockstore(parentAPI)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, subnetAct.Head, &snst); err != nil {
		return false, err
	}
	pstore := adt.WrapStore(ctx, pcst)

	// Check if no checkpoint committed for this window
	signWindow := ctypes.CheckpointEpoch(subnetEpoch, snst.CheckPeriod)

	// Reset state if we have changed signing windows
	if signWindow != sh.sigWindow() {
		sh.resetSigState(signWindow)
		// trigger gc
		go sh.gcCheckBuf(pstore, &snst, signWindow)
	}
	_, found, err := snst.GetCheckpoint(pstore, signWindow)
	if err != nil {
		return false, err
	}
	if found {
		log.Infow("Checkpoint for epoch already committed", "epoch", signWindow)
		return false, nil
	}

	// Get raw checkpoint for this window from SCA of subnet
	scaAct, err := sh.api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, newTs.Key())
	if err != nil {
		return false, err
	}
	var scast sca.SCAState
	bs := blockstore.NewAPIBlockstore(sh.api)
	cst := cbor.NewCborStore(bs)
	if err := cst.Get(ctx, scaAct.Head, &scast); err != nil {
		return false, err
	}
	store := adt.WrapStore(ctx, cst)
	ch, err := sca.RawCheckpoint(&scast, store, signWindow)
	if err != nil {
		log.Errorw("Error getting raw checkpoint", "err", err)
		return false, err
	}
	// Populate checkpoint data
	if err := sh.populateCheckpoint(ctx, pstore, &snst, ch); err != nil {
		log.Errorw("Error populating checkpoint template", "err", err)
		return false, err
	}

	chcid, err := ch.Cid()
	if err != nil {
		return false, err
	}

	// Check if there are votes for this checkpoint
	votes, found, err := snst.GetWindowChecks(pstore, chcid)
	if err != nil {
		return false, err
	}

	// If not check if I am miner and I haven't submitted a vote
	// from all the identities in my wallet.
	wallAddrs, err := s.api.WalletAPI.WalletList(ctx)
	if err != nil {
		return false, err
	}
	for _, waddr := range wallAddrs {
		addr, err := s.api.StateLookupID(ctx, waddr, types.EmptyTSK)
		if err != nil {
			// Disregard errors here. We want to check if the
			// state changes, if we can't check this, well, we keep going!
			continue
		}
		// I'm in the list of miners, check if I have already committed
		// a checkpoint.
		if snst.IsMiner(addr) {
			// If no windowChecks found, or we haven't sent a vote yet
			if !found || !subnet.HasMiner(addr, votes.Miners) {
				sh.sigWaitTick()
				// If wait reached, the tipset is final and we can sign.
				// This wait ensures that we only sign once
				if sh.sigWaitReached() && !sh.hasSigned() {
					diff.checkToSign = &signInfo{ch, waddr, addr}
					// Notify that this epoch for subnet has been marked for signing.
					sh.signed(ch)
					return true, nil
				}
			}
		}

	}
	// If not return.
	return false, nil
}

func (s *Service) triggerChange(ctx context.Context, sh *Subnet, diff *diffInfo) (more bool, err error) {
	// If there's a checkpoint to sign.
	if diff.checkToSign != nil {
		err := s.signAndSubmitCheckpoint(ctx, sh, diff.checkToSign)
		if err != nil {
			log.Errorw("Error signing checkpoint for subnet", "subnetID", sh.ID, "err", err)
			return true, err
		}
		log.Infow("Success signing checkpoint in subnet", "subnetID", sh.ID.String())
	}

	return true, nil
}

func (s *Service) signAndSubmitCheckpoint(ctx context.Context, sh *Subnet, info *signInfo) error {
	log.Infow("Signing checkpoint for subnet", "subnetID", info.checkpoint.Source().String())
	// Using simple signature to sign checkpoint using the subnet wallet.
	ver := checkpoint.NewSingleSigner()
	err := ver.Sign(ctx, sh.api.WalletAPI.Wallet, info.addr, info.checkpoint,
		[]checkpoint.SigningOpts{checkpoint.IDAddr(info.idAddr)}...)
	if err != nil {
		return err
	}
	// Sign checkpoint
	_, err = s.SubmitSignedCheckpoint(ctx, info.addr, sh.ID, info.checkpoint)
	if err != nil {
		return err
	}

	// Trying to push cross-msgs included in checkpoint to corresponding subnet.
	log.Infow("Pushing cross-msgs from checkpoint", "subnetID", info.checkpoint.Source().String())
	subAPI := s.getAPI(sh.ID)
	if subAPI == nil {
		return xerrors.Errorf("Not listening to subnet")
	}
	st, store, err := s.GetSCAState(ctx, sh.ID)
	if err != nil {
		return err
	}

	// Pushing cross-msg in checkpoint to corresponding subnets.
	return sh.r.PushMsgFromCheckpoint(info.checkpoint, st, store)
}
