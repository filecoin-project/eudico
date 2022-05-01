package subnetmgr

import (
	"context"
	"sync"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
)

// finalityWait is the number of epochs that we will wait
// before being able to re-propose a cross-msg. This is used to
// wait for all the state changes to be propagated.
const finalityWait = 15

// UnverifiedCrossMsg is a wrapper on types.Message to provide information related to its type.
type UnverifiedCrossMsg struct {
	Type uint64
	Msg  *types.Message
}

func newCrossMsgPool() *crossMsgPool {
	return &crossMsgPool{pool: make(map[address.SubnetID]*lastApplied)}
}

type crossMsgPool struct {
	lk   sync.RWMutex
	pool map[address.SubnetID]*lastApplied
}

type lastApplied struct {
	lk       sync.RWMutex
	topdown  map[uint64]abi.ChainEpoch // nonce[epoch]
	bottomup map[uint64]abi.ChainEpoch // nonce[epoch]
	height   abi.ChainEpoch
}

func (cm *crossMsgPool) getPool(id address.SubnetID, height abi.ChainEpoch) *lastApplied {
	cm.lk.RLock()
	p, ok := cm.pool[id]
	cm.lk.RUnlock()
	// If no pool for subnet or height higher than the subsequent one.
	// Add a buffer before pruning message pool.
	if !ok || height > p.height+finalityWait {
		cm.lk.Lock()
		p = &lastApplied{
			height:   height,
			topdown:  make(map[uint64]abi.ChainEpoch),
			bottomup: make(map[uint64]abi.ChainEpoch),
		}
		cm.pool[id] = p
		cm.lk.Unlock()
	}

	return p
}

func (cm *crossMsgPool) applyTopDown(n uint64, id address.SubnetID, height abi.ChainEpoch) {
	p := cm.getPool(id, height)
	p.lk.Lock()
	defer p.lk.Unlock()
	p.topdown[n] = height
}

func (cm *crossMsgPool) applyBottomUp(n uint64, id address.SubnetID, height abi.ChainEpoch) {
	p := cm.getPool(id, height)
	p.lk.Lock()
	defer p.lk.Unlock()
	p.bottomup[n] = height
}

func (cm *crossMsgPool) isTopDownApplied(n uint64, id address.SubnetID, height abi.ChainEpoch) bool {
	p := cm.getPool(id, height)
	p.lk.RLock()
	defer p.lk.RUnlock()
	h, ok := p.topdown[n]
	return ok && h != height
}

func (cm *crossMsgPool) isBottomUpApplied(n uint64, id address.SubnetID, height abi.ChainEpoch) bool {
	p := cm.getPool(id, height)
	p.lk.RLock()
	defer p.lk.RUnlock()
	h, ok := p.bottomup[n]
	return ok && h != height
}

// getCrossMsgs returns top-down and bottom-up messages.
func (s *SubnetMgr) getCrossMsgs(
	ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.Message, []*types.Message, error) {
	// TODO: Think a bit deeper the locking strategy for subnets.
	// s.lk.RLock()
	// defer s.lk.RUnlock()

	var (
		topdown  []*types.Message
		bottomup []*types.Message
		err      error
	)

	// top-down messages only supported in subnets, not the root.
	if !s.isRoot(id) {
		topdown, err = s.getTopDownPool(ctx, id, height)
		if err != nil {
			return nil, nil, err
		}
	}

	// Get bottom-up messages and return all cross-messages.
	bottomup, err = s.getBottomUpPool(ctx, id, height)
	if err != nil {
		return nil, nil, err
	}

	return topdown, bottomup, nil
}

// GetCrossMsgsPool returns a list with `num` number of cross messages pending for validation.
func (s *SubnetMgr) GetCrossMsgsPool(
	ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.Message, error) {

	topdown, bottomup, err := s.getCrossMsgs(ctx, id, height)
	if err != nil {
		return nil, err
	}

	out := make([]*types.Message, len(topdown)+len(bottomup))
	copy(out[:len(topdown)], topdown)
	copy(out[len(topdown):], bottomup)

	log.Debugf("Picked up %d cross-msgs from CrossMsgPool", len(out))
	return out, nil
}

// GetUnverifiedCrossMsgsPool returns a list with `num` number of cross messages with their type information
// (top-down or bottom-up) pending for validation.
func (s *SubnetMgr) GetUnverifiedCrossMsgsPool(
	ctx context.Context, id address.SubnetID, height abi.ChainEpoch,
) ([]*types.UnverifiedCrossMsg, error) {
	topdown, bottomup, err := s.getCrossMsgs(ctx, id, height)
	if err != nil {
		return nil, err
	}
	var out []*types.UnverifiedCrossMsg

	for _, msg := range topdown {
		out = append(out, &types.UnverifiedCrossMsg{
			Type: uint64(hierarchical.TopDown),
			Msg:  msg,
		})
	}

	for _, msg := range bottomup {
		out = append(out, &types.UnverifiedCrossMsg{
			Type: uint64(hierarchical.BottomUp),
			Msg:  msg,
		})
	}

	log.Debugf("Picked up %d unverified cross-msgs from CrossMsgPool", len(out))
	return out, nil
}

// FundSubnet injects funds in a subnet and returns the Cid of the
// message of the parent chain that included it.
func (s *SubnetMgr) FundSubnet(
	ctx context.Context, wallet address.Address,
	id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get the api for the parent network hosting the subnet actor
	// for the subnet.
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	params := &sca.SubnetIDParam{ID: id.String()}
	serparams, err := actors.SerializeParams(params)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed serializing init actor params: %s", err)
	}

	// Get the parent and the actor to know where to send the message.
	smsg, aerr := parentAPI.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   wallet,
		Value:  value,
		Method: sca.Methods.Fund,
		Params: serparams,
	}, nil)
	if aerr != nil {
		log.Errorf("Error MpoolPushMessage: %s", aerr)
		return cid.Undef, aerr
	}

	return smsg.Cid(), nil
}

// ReleaseFunds releases some funds from a subnet
func (s *SubnetMgr) ReleaseFunds(
	ctx context.Context, wallet address.Address,
	id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) {

	// TODO: Think a bit deeper the locking strategy for subnets.
	s.lk.RLock()
	defer s.lk.RUnlock()

	// Get the api for the subnet
	api, err := s.GetSubnetAPI(id)
	if err != nil {
		return cid.Undef, err
	}

	// Send a release message to SCA in subnet
	smsg, aerr := api.MpoolPushMessage(ctx, &types.Message{
		To:     hierarchical.SubnetCoordActorAddr,
		From:   wallet,
		Value:  value,
		Method: sca.Methods.Release,
		Params: nil,
	}, nil)
	if aerr != nil {
		log.Errorf("Error MpoolPushMessage: %s", aerr)
		return cid.Undef, aerr
	}

	return smsg.Cid(), nil
}

func (s *SubnetMgr) getSCAStateWithFinality(ctx context.Context, api *API, id address.SubnetID) (*sca.SCAState, adt.Store, error) {
	var err error
	finTs := api.ChainAPI.Chain.GetHeaviestTipSet()
	height := finTs.Height()

	// Avoid negative epochs
	if height-FinalityThreshold >= 0 {
		// Go back FinalityThreshold to ensure the state is final in parent chain
		finTs, err = api.ChainGetTipSetByHeight(ctx, height-FinalityThreshold, types.EmptyTSK)
		if err != nil {
			return nil, nil, err
		}
	}

	// Get parent state back in the past where it should be final.
	// (we don't want to validate in the subnet a state that may be reverted in the parent)
	pAct, err := api.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, finTs.Key())
	if err != nil {
		return nil, nil, err
	}
	var st sca.SCAState
	pbs := blockstore.NewAPIBlockstore(api)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, pAct.Head, &st); err != nil {
		return nil, nil, err
	}

	return &st, adt.WrapStore(ctx, pcst), nil
}

// getParentSCAWithFinality returns the state of the SCA of the parent with `FinalityThreshold`
// epochs ago to ensure that no reversion happens and we can operate with the state we got.
func (s *SubnetMgr) getParentSCAWithFinality(ctx context.Context, id address.SubnetID) (*sca.SCAState, adt.Store, error) {
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return nil, nil, err
	}
	return s.getSCAStateWithFinality(ctx, parentAPI, id)
}

func (s *SubnetMgr) getTopDownPool(ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.Message, error) {

	// Get status for SCA in subnet to determine from which nonce to fetch messages
	subAPI := s.getAPI(id)
	if subAPI == nil {
		return nil, xerrors.Errorf("Not listening to subnet")
	}
	subnetAct, err := subAPI.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	var snst sca.SCAState
	bs := blockstore.NewAPIBlockstore(subAPI)
	cst := cbor.NewCborStore(bs)
	if err := cst.Get(ctx, subnetAct.Head, &snst); err != nil {
		return nil, err
	}

	// Get tipset at height-FinalityThreshold to ensure some level of finality
	// to get pool of cross-messages.
	st, pstore, err := s.getParentSCAWithFinality(ctx, id)
	if err != nil {
		return nil, err
	}
	// Get topDown messages from parent SCA
	sh, found, err := st.GetSubnet(pstore, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, xerrors.Errorf("subnet with ID %v not found", id)
	}

	msgs, err := sh.TopDownMsgFromNonce(pstore, snst.AppliedTopDownNonce)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Message, 0)
	for _, m := range msgs {
		// The pool waits a few epochs before re-proposing a cross-msg if the applied nonce
		// hasn't changed in order to give enough time for state changes to propagate.
		if s.cm.isTopDownApplied(m.Nonce, id, height) {
			continue
		}
		// FIXME: Instead of applying the message to check if it fails before including in
		// the cross-msg pool, we include every cross-msg and if it fails it is handled by
		// the SCA when applied. We could probably check here if it fails, and for failing messages
		// revert the source transaction. Check https://github.com/filecoin-project/eudico/issues/92
		// for further details.
		// // Apply message to see if it succeeds before considering it for the pool.
		// if err := s.applyMsg(ctx, subAPI.StateManager, id, m); err != nil {
		//         log.Warnf("Error applying cross message when picking it up from CrossMsgPool: %s", err)
		//         continue
		// }
		out = append(out, m)
		s.cm.applyTopDown(m.Nonce, id, height)
	}
	return out, nil
}

func (s *SubnetMgr) getBottomUpPool(ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.Message, error) {
	subAPI := s.getAPI(id)
	if subAPI == nil {
		return nil, xerrors.Errorf("Not listening to subnet")
	}
	// Get tipset at height-FinalityThreshold to ensure some level of finality
	// to get pool of cross-messages.
	st, pstore, err := s.getSCAStateWithFinality(ctx, subAPI, id)
	if err != nil {
		return nil, err
	}
	// BottomUpMessage work a bit different from topDown messages. We accept
	// several messages with the same nonce because Metas batch several messages
	// inside the same package. To prevent applying messages twice we look in
	// the pool for messages with AppliedBottomUpNonce+1, because the previous
	// nonce was already applied in the previous batch (see sca_actor::ApplyMsg)
	toApply := st.AppliedBottomUpNonce + 1
	metas, err := st.BottomUpMsgFromNonce(pstore, toApply)
	if err != nil {
		return nil, err
	}

	// Get resolver for subnet
	r := s.getSubnetResolver(id)

	out := make([]*types.Message, 0)
	isFound := make(map[uint64][]types.Message)
	// Resolve CrossMsgs behind meta or send pull message.
	for _, mt := range metas {
		// Resolve CrossMsgs behind meta.
		c, err := mt.Cid()
		if err != nil {
			return nil, err
		}
		cross, found, err := r.ResolveCrossMsgs(ctx, c, address.SubnetID(mt.From))
		if err != nil {
			return nil, err
		}
		if found {
			// Mark that the meta with specific nonce has been
			// fully resolved including the messages
			isFound[uint64(mt.Nonce)] = cross
		}
	}

	// We return from AppliedBottomUpNonce all the metas that have been resolved
	// successfully. They need to be applied sequentially, so the moment we find
	// an unresolved meta we return.
	// FIXME: This approach may affect the liveliness of hierarchical consensus.
	// Assuming data availability and honest nodes we should include a fallback
	// scheme to prevent the protocol from stalling.
	for i := toApply; i < toApply+uint64(len(metas)); i++ {
		cross, ok := isFound[i]
		// If not found, return
		if !ok {
			return out, nil
		}
		// The pool waits a few epochs before re-proposing a cross-msg if the applied nonce
		// hasn't changed in order to give enough time for state changes to propagate.
		if s.cm.isBottomUpApplied(i, id, height) {
			continue
		}
		for _, m := range cross {
			// Add the meta nonce to the message nonce
			m.Nonce = i
			// Append for return
			out = append(out, &m) // nolint
		}
		s.cm.applyBottomUp(i, id, height)
	}

	return out, nil
}

func (s *SubnetMgr) getSubnetResolver(id address.SubnetID) *resolver.Resolver {
	r := s.r
	if !s.isRoot(id) {
		r = s.subnets[id].r
	}
	return r
}

func (s *SubnetMgr) CrossMsgResolve(ctx context.Context, id address.SubnetID, c cid.Cid, from address.SubnetID) ([]types.Message, error) {
	r := s.getSubnetResolver(id)
	msgs, _, err := r.ResolveCrossMsgs(ctx, c, from)
	return msgs, err
}

func (s *SubnetMgr) WaitCrossMsgResolved(ctx context.Context, id address.SubnetID, c cid.Cid, from address.SubnetID) chan error {
	r := s.getSubnetResolver(id)
	return r.WaitCrossMsgsResolved(ctx, c, from)
}
