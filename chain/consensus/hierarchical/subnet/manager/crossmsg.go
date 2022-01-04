package subnetmgr

import (
	"context"

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

// GetCrossMsgsPool returns a list with `num` number of of cross messages pending for validation.
//
// if num == 0 there's no limit in the number of cross-messages returned.
func (s *SubnetMgr) GetCrossMsgsPool(
	ctx context.Context, id hierarchical.SubnetID, num int) ([]*types.Message, error) {
	// TODO: Think a bit deeper the locking strategy for subnets.
	// s.lk.RLock()
	// defer s.lk.RUnlock()

	var (
		topdown []*types.Message
		downtop []*types.Message
		err     error
	)

	// topDown messages only supported in subnets, not the root.
	if !s.isRoot(id) {
		topdown, err = s.getTopDownPool(ctx, id)
		if err != nil {
			return nil, err
		}
	}

	// TODO: Get downtop messages and return all cross-messages.
	// NOTE: Down-top transactions are supported also in root chain.
	// topdown, err := s.getDownTopPool(ctx, id)

	out := make([]*types.Message, len(topdown)+len(downtop))
	copy(out[:len(topdown)], topdown)
	copy(out[len(topdown):], downtop)

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

func (s *SubnetMgr) getTopDownPool(ctx context.Context, id hierarchical.SubnetID) ([]*types.Message, error) {

	// Get status for SCA in subnet to determine from which nonce to fetch messages
	subAPI := s.getAPI(id)
	if subAPI == nil {
		xerrors.Errorf("Not listening to subnet")
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

	return sh.TopDownMsgFromNonce(pstore, snst.AppliedDownTopNonce)
}

func (s *SubnetMgr) getDownTopPool(ctx context.Context, id hierarchical.SubnetID) ([]*types.Message, error) {
	// 1. Get DownTopMsgMeta from SCA in the subnet (the ones that need to
	// be applied here).
	// 2. Get Cid and From of CrossMsgMeta
	// 3. (Implement LinkSystem) Check locally if we have the messages behind the
	// cid or if they need to be fetched from the subnet.
	// 4. (Implement cross message exchange protocol) Get the message behind a Cid.
	panic("Not implemented")
}
