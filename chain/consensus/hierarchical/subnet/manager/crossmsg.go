package subnetmgr

import (
	"context"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/actors/registry"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/rand"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
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

func (cm *crossMsgPool) rmPool(id hierarchical.SubnetID) {
	cm.lk.Lock()
	defer cm.lk.Unlock()
	delete(cm.pool, id)
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

// applyMsgs runs the cross-message before providing it to the pool.
// We shouldn't propose invalid cross-messages.
func (s *SubnetMgr) applyMsg(ctx context.Context, sm *stmgr.StateManager, id hierarchical.SubnetID, msg *types.Message) error {
	ts := sm.ChainStore().GetHeaviestTipSet()
	r := rand.NewStateRand(sm.ChainStore(), ts.Cids(), nil)
	makeVmWithBaseState := func(base cid.Cid) (*vm.VM, error) {
		vmopt := &vm.VMOpts{
			StateBase:      base,
			Epoch:          ts.Height(),
			Rand:           r,
			Bstore:         sm.ChainStore().StateBlockstore(),
			Actors:         registry.NewActorRegistry(),
			Syscalls:       sm.Syscalls,
			CircSupplyCalc: sm.GetVMCirculatingSupply,
			NtwkVersion:    sm.GetNtwkVersion,
			BaseFee:        abi.NewTokenAmount(0),
			LookbackState:  stmgr.LookbackStateGetterForTipset(sm, ts),
		}

		return sm.VMConstructor()(ctx, vmopt)
	}

	pstate := ts.ParentState()
	vmi, err := makeVmWithBaseState(pstate)
	if err != nil {
		return xerrors.Errorf("making vm: %w", err)
	}
	return common.ApplyCrossMsg(ctx, vmi, s, nil, msg, ts, dtypes.NetworkName(id))
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

	// TODO: Get bottomup messages and return all cross-messages.
	// NOTE: Down-top transactions are supported also in root chain.
	// topdown, err := s.getBottomUpPool(ctx, id)

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

func (s *SubnetMgr) getParentSCAWithFinality(ctx context.Context, id hierarchical.SubnetID) (*sca.SCAState, adt.Store, error) {

	parentAPI, err := s.getParentAPI(id)
	if err != nil {
		return nil, nil, err
	}
	finTs := parentAPI.ChainAPI.Chain.GetHeaviestTipSet()
	height := finTs.Height()
	if height-finalityThreshold >= 0 {
		// Go back finalityThreshold to ensure the state is final in parent chain
		finTs, err = parentAPI.ChainGetTipSetByHeight(ctx, height-finalityThreshold, types.EmptyTSK)
		if err != nil {
			return nil, nil, err
		}
	}

	// Get parent state back in the past where it should be final.
	// (we don't want to validate in the subnet a state that may be reverted in the parent)
	pAct, err := parentAPI.StateGetActor(ctx, hierarchical.SubnetCoordActorAddr, finTs.Key())
	if err != nil {
		return nil, nil, err
	}
	var st sca.SCAState
	pbs := blockstore.NewAPIBlockstore(parentAPI)
	pcst := cbor.NewCborStore(pbs)
	if err := pcst.Get(ctx, pAct.Head, &st); err != nil {
		return nil, nil, err
	}

	return &st, adt.WrapStore(ctx, pcst), nil
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
		// Pass a few epochs before re-proposing a cross-msg if nonce hasn't changed.
		// TODO: Using != 0 for testing purposes. This logic should be done right.
		// What we need to do here if for each message nonce, check if it was recently
		// proposed and wait one epoch to see if it is applied and applied nonce incremented.
		// If not, we can re-propose in the pool.
		if s.cm.isTopDownApplied(m.Nonce, id, height) {
			continue
		}
		// Apply message to see if it succeeds before considering it for the pool.
		if err := s.applyMsg(ctx, subAPI.StateManager, id, m); err != nil {
			log.Warnf("Error applying cross message when picking it up from CrossMsgPool: %s", err)
			continue
		}
		out = append(out, m)
		s.cm.applyTopDown(m.Nonce, id, height)
	}
	return out, nil
}

func (s *SubnetMgr) getBottomUpPool(ctx context.Context, id hierarchical.SubnetID) ([]*types.Message, error) {
	// 1. Get BottomUpMsgMeta from SCA in the subnet (the ones that need to
	// be applied here).
	// 2. Get Cid and From of CrossMsgMeta
	// 3. (Implement LinkSystem) Check locally if we have the messages behind the
	// cid or if they need to be fetched from the subnet.
	// 4. (Implement cross message exchange protocol) Get the message behind a Cid.
	panic("Not implemented")
}
