package sharding

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

// TODO: This re-implementation of the EVENTAPI may not be needed
// once we populate part of the FullNodeAPI with what we need. This will
// happen in the next iteration.
var _ events.EventAPI = &shardEventAPI{}

type shardEventAPI struct {
	sm    *stmgr.StateManager
	chain *store.ChainStore
}

func (sh *Shard) InitEvents(ctx context.Context) error {
	var err error
	sh.eventAPI = &shardEventAPI{
		sm:    sh.sm,
		chain: sh.ch,
	}
	// Starting events to listen to shard events
	sh.events, err = events.NewEvents(ctx, sh.eventAPI)
	return err
}

func (e *shardEventAPI) ChainNotify(ctx context.Context) (<-chan []*api.HeadChange, error) {
	return e.chain.SubHeadChanges(ctx), nil
}
func (e *shardEventAPI) ChainGetBlockMessages(ctx context.Context, msg cid.Cid) (*api.BlockMessages, error) {
	b, err := e.chain.GetBlock(msg)
	if err != nil {
		return nil, err
	}

	bmsgs, smsgs, err := e.chain.MessagesForBlock(b)
	if err != nil {
		return nil, err
	}

	cids := make([]cid.Cid, len(bmsgs)+len(smsgs))

	for i, m := range bmsgs {
		cids[i] = m.Cid()
	}

	for i, m := range smsgs {
		cids[i+len(bmsgs)] = m.Cid()
	}

	return &api.BlockMessages{
		BlsMessages:   bmsgs,
		SecpkMessages: smsgs,
		Cids:          cids,
	}, nil
}

func (e *shardEventAPI) ChainGetTipSetByHeight(ctx context.Context, h abi.ChainEpoch, tsk types.TipSetKey) (*types.TipSet, error) {
	ts, err := e.chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	return e.chain.GetTipsetByHeight(ctx, h, ts, true)
}
func (e *shardEventAPI) ChainGetTipSetAfterHeight(ctx context.Context, h abi.ChainEpoch, tsk types.TipSetKey) (*types.TipSet, error) {
	ts, err := e.chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	return e.chain.GetTipsetByHeight(ctx, h, ts, false)
}

func (e *shardEventAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return e.chain.GetHeaviestTipSet(), nil
}

func (e *shardEventAPI) StateSearchMsg(ctx context.Context, tsk types.TipSetKey, msg cid.Cid, lookbackLimit abi.ChainEpoch, allowReplaced bool) (*api.MsgLookup, error) {
	fromTs, err := e.chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}

	ts, recpt, found, err := e.sm.SearchForMessage(ctx, fromTs, msg, lookbackLimit, allowReplaced)
	if err != nil {
		return nil, err
	}

	if ts != nil {
		return &api.MsgLookup{
			Message: found,
			Receipt: *recpt,
			TipSet:  ts.Key(),
			Height:  ts.Height(),
		}, nil
	}
	return nil, nil
}
func (e *shardEventAPI) ChainGetTipSet(ctx context.Context, tsk types.TipSetKey) (*types.TipSet, error) {
	return e.chain.LoadTipSet(tsk)
}
func (e *shardEventAPI) ChainGetPath(ctx context.Context, from, to types.TipSetKey) ([]*api.HeadChange, error) {
	return e.chain.GetPath(ctx, from, to)
}

func (e *shardEventAPI) StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error) {
	ts, err := e.chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	return e.sm.LoadActor(ctx, actor, ts)
}
