package subnetmgr

import (
	"context"
	"fmt"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

// finalityWait is the number of epochs that we will wait
// before being able to re-propose a cross-msg. This is used to
// wait for all the state changes to be propagated.
const finalityWait = 5

func newCrossMsgPool() *crossMsgPool {
	return &crossMsgPool{pool: make(map[hierarchical.SubnetID]*lastApplied)}
}

type crossMsgPool struct {
	lk   sync.RWMutex
	pool map[hierarchical.SubnetID]*lastApplied
}

type lastApplied struct {
	lk       sync.RWMutex
	topdown  map[uint64]abi.ChainEpoch // nonce[epoch]
	bottomup map[uint64]abi.ChainEpoch // nonce[epoch]
	height   abi.ChainEpoch
}

func (cm *crossMsgPool) getPool(id hierarchical.SubnetID, height abi.ChainEpoch) *lastApplied {
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

func (cm *crossMsgPool) applyTopDown(n uint64, id hierarchical.SubnetID, height abi.ChainEpoch) {
	p := cm.getPool(id, height)
	p.lk.Lock()
	defer p.lk.Unlock()
	p.topdown[n] = height
}

func (cm *crossMsgPool) applyBottomUp(n uint64, id hierarchical.SubnetID, height abi.ChainEpoch) {
	p := cm.getPool(id, height)
	p.lk.Lock()
	defer p.lk.Unlock()
	p.bottomup[n] = height
}

func (cm *crossMsgPool) isTopDownApplied(n uint64, id hierarchical.SubnetID, height abi.ChainEpoch) bool {
	p := cm.getPool(id, height)
	p.lk.RLock()
	defer p.lk.RUnlock()
	h, ok := p.topdown[n]
	return ok && h != height
}

func (cm *crossMsgPool) isBottomUpApplied(n uint64, id hierarchical.SubnetID, height abi.ChainEpoch) bool {
	p := cm.getPool(id, height)
	p.lk.RLock()
	defer p.lk.RUnlock()
	h, ok := p.bottomup[n]
	return ok && h != height
}

// GetCrossMsgsPool returns a list with `num` number of of cross messages pending for validation.
//
// height determines the current consensus height
func (s *SubnetMgr) GetCrossMsgsPool(
	ctx context.Context, id hierarchical.SubnetID, height abi.ChainEpoch) ([]*types.Message, error) {
	// TODO: Think a bit deeper the locking strategy for subnets.
	// s.lk.RLock()
	// defer s.lk.RUnlock()

	var (
		topdown  []*types.Message
		bottomup []*types.Message
		err      error
	)

	// topDown messages only supported in subnets, not the root.
	if !s.isRoot(id) {
		topdown, err = s.getTopDownPool(ctx, id, height)
		if err != nil {
			return nil, err
		}
	}

	// Get bottomup messages and return all cross-messages.
	// bottomup, err = s.getBottomUpPool(ctx, id, height)
	// if err != nil {
	//         return nil, err
	// }

	out := make([]*types.Message, len(topdown)+len(bottomup))
	copy(out[:len(topdown)], topdown)
	copy(out[len(topdown):], bottomup)

	log.Debugf("Picked up %d cross-msgs from CrossMsgPool", len(out))
	return out, nil
}

// FundSubnet injects funds in a subnet and returns the Cid of the
// message of the parent chain that included it.
func (s *SubnetMgr) FundSubnet(
	ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID, value abi.TokenAmount) (cid.Cid, error) {

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
	id hierarchical.SubnetID, value abi.TokenAmount) (cid.Cid, error) {

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

func (s *SubnetMgr) getSCAStateWithFinality(ctx context.Context, api *API, id hierarchical.SubnetID) (*sca.SCAState, adt.Store, error) {
	var err error
	finTs := api.ChainAPI.Chain.GetHeaviestTipSet()
	height := finTs.Height()

	// Avoid negative epochs
	if height-finalityThreshold >= 0 {
		// Go back finalityThreshold to ensure the state is final in parent chain
		finTs, err = api.ChainGetTipSetByHeight(ctx, height-finalityThreshold, types.EmptyTSK)
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

// getParentSCAWithFinality returns the state of the SCA of the parent with `finalityThreshold`
// epochs ago to ensure that no reversion happens and we can operate with the state we got.
func (s *SubnetMgr) getParentSCAWithFinality(ctx context.Context, id hierarchical.SubnetID) (*sca.SCAState, adt.Store, error) {
	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return nil, nil, err
	}
	return s.getSCAStateWithFinality(ctx, parentAPI, id)
}

func (s *SubnetMgr) getTopDownPool(ctx context.Context, id hierarchical.SubnetID, height abi.ChainEpoch) ([]*types.Message, error) {

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

	// Get tipset at height-finalityThreshold to ensure some level of finality
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

func (s *SubnetMgr) getBottomUpPool(ctx context.Context, id hierarchical.SubnetID, height abi.ChainEpoch) ([]*types.Message, error) {
	subAPI := s.getAPI(id)
	if subAPI == nil {
		return nil, xerrors.Errorf("Not listening to subnet")
	}
	// Get tipset at height-finalityThreshold to ensure some level of finality
	// to get pool of cross-messages.
	st, pstore, err := s.getSCAStateWithFinality(ctx, subAPI, id)
	if err != nil {
		return nil, err
	}
	metas, err := st.BottomUpMsgFromNonce(pstore, st.AppliedBottomUpNonce)
	if err != nil {
		return nil, err
	}

	fmt.Println(">>>>> TODO: Received metas, we need to resolve the CID for meta", metas)
	return []*types.Message{}, nil

	// 1. Get BottomUpMsgMeta from SCA in the subnet (the ones that need to
	// be applied here).
	// 2. Get Cid and From of CrossMsgMeta
	// 3. (Implement LinkSystem) Check locally if we have the messages behind the
	// cid or if they need to be fetched from the subnet.
	// 4. (Implement cross message exchange protocol) Get the message behind a Cid.
}
