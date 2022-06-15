package hierarchical

import (
	"context"

	"github.com/ipfs/go-cid"
	"go.uber.org/fx"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/submgr"
	"github.com/filecoin-project/lotus/chain/types"
)

var _ api.HierarchicalCns = &HierarchicalAPI{}

type HierarchicalAPI struct {
	fx.In

	SubnetAPI *submgr.Service
}

func (a *HierarchicalAPI) AddSubnet(ctx context.Context, new *hierarchical.SubnetParams) (address.Address, error) {
	return a.SubnetAPI.AddSubnet(ctx, new)
}

func (a *HierarchicalAPI) JoinSubnet(ctx context.Context, wallet address.Address,
	value abi.TokenAmount, id address.SubnetID, validator string) (cid.Cid, error) {
	return a.SubnetAPI.JoinSubnet(ctx, wallet, value, id, validator)
}

func (a *HierarchicalAPI) SyncSubnet(ctx context.Context, id address.SubnetID, stop bool) error {
	return a.SubnetAPI.SyncSubnet(ctx, id, stop)
}

func (a *HierarchicalAPI) MineSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID, stop bool, params *hierarchical.MiningParams) error {
	return a.SubnetAPI.MineSubnet(ctx, wallet, id, stop, params)
}

func (a *HierarchicalAPI) LeaveSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID) (cid.Cid, error) {
	return a.SubnetAPI.LeaveSubnet(ctx, wallet, id)
}

func (a *HierarchicalAPI) ListSubnets(ctx context.Context, id address.SubnetID) ([]sca.SubnetOutput, error) {
	return a.SubnetAPI.ListSubnets(ctx, id)
}

func (a *HierarchicalAPI) KillSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID) (cid.Cid, error) {
	return a.SubnetAPI.KillSubnet(ctx, wallet, id)
}

func (a *HierarchicalAPI) ListCheckpoints(ctx context.Context,
	id address.SubnetID, num int) ([]*schema.Checkpoint, error) {
	return a.SubnetAPI.ListCheckpoints(ctx, id, num)
}

func (a *HierarchicalAPI) ValidateCheckpoint(ctx context.Context,
	id address.SubnetID, epoch abi.ChainEpoch) (*schema.Checkpoint, error) {
	return a.SubnetAPI.ValidateCheckpoint(ctx, id, epoch)
}

func (a *HierarchicalAPI) GetCrossMsgsPool(ctx context.Context, id address.SubnetID,
	height abi.ChainEpoch) ([]*types.Message, error) {
	return a.SubnetAPI.GetCrossMsgsPool(ctx, id, height)
}

func (a *HierarchicalAPI) GetUnverifiedCrossMsgsPool(ctx context.Context, id address.SubnetID,
	height abi.ChainEpoch) ([]*types.UnverifiedCrossMsg, error) {
	return a.SubnetAPI.GetUnverifiedCrossMsgsPool(ctx, id, height)
}

func (a *HierarchicalAPI) FundSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) {
	return a.SubnetAPI.FundSubnet(ctx, wallet, id, value)
}

func (a *HierarchicalAPI) ReleaseFunds(ctx context.Context, wallet address.Address,
	id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) {
	return a.SubnetAPI.ReleaseFunds(ctx, wallet, id, value)
}

func (a *HierarchicalAPI) CrossMsgResolve(ctx context.Context, id address.SubnetID,
	c cid.Cid, from address.SubnetID) ([]types.Message, error) {
	return a.SubnetAPI.CrossMsgResolve(ctx, id, c, from)
}

func (a *HierarchicalAPI) LockState(
	ctx context.Context, wallet address.Address, actor address.Address,
	subnet address.SubnetID, method abi.MethodNum) (cid.Cid, error) {
	return a.SubnetAPI.LockState(ctx, wallet, actor, subnet, method)
}

func (a *HierarchicalAPI) UnlockState(
	ctx context.Context, wallet address.Address, actor address.Address,
	subnet address.SubnetID, method abi.MethodNum) error {
	return a.SubnetAPI.UnlockState(ctx, wallet, actor, subnet, method)
}

func (a *HierarchicalAPI) InitAtomicExec(
	ctx context.Context, wallet address.Address, inputs map[string]sca.LockedState,
	msgs []types.Message) (cid.Cid, error) {
	return a.SubnetAPI.InitAtomicExec(ctx, wallet, inputs, msgs)
}

func (a *HierarchicalAPI) ListAtomicExecs(ctx context.Context, id address.SubnetID,
	addr address.Address) ([]sca.AtomicExec, error) {
	return a.SubnetAPI.ListAtomicExecs(ctx, id, addr)
}

func (a *HierarchicalAPI) ComputeAndSubmitExec(ctx context.Context, wallet address.Address,
	id address.SubnetID, execID cid.Cid) (sca.ExecStatus, error) {
	return a.SubnetAPI.ComputeAndSubmitExec(ctx, wallet, id, execID)
}

func (a *HierarchicalAPI) AbortAtomicExec(ctx context.Context, wallet address.Address,
	id address.SubnetID, execID cid.Cid) (sca.ExecStatus, error) {
	return a.SubnetAPI.AbortAtomicExec(ctx, wallet, id, execID)
}

func (a *HierarchicalAPI) SubnetChainNotify(ctx context.Context, id address.SubnetID) (<-chan []*api.HeadChange, error) {
	return a.SubnetAPI.SubnetChainNotify(ctx, id)
}

func (a *HierarchicalAPI) SubnetChainHead(ctx context.Context, id address.SubnetID) (*types.TipSet, error) {
	return a.SubnetAPI.SubnetChainHead(ctx, id)
}

func (a *HierarchicalAPI) SubnetStateGetActor(ctx context.Context, id address.SubnetID, addr address.Address,
	tsk types.TipSetKey) (*types.Actor, error) {
	return a.SubnetAPI.SubnetStateGetActor(ctx, id, addr, tsk)
}

func (a *HierarchicalAPI) SubnetStateWaitMsg(ctx context.Context, id address.SubnetID, cid cid.Cid, confidence uint64, limit abi.ChainEpoch,
	allowReplaced bool) (*api.MsgLookup, error) {
	return a.SubnetAPI.SubnetStateWaitMsg(ctx, id, cid, confidence, limit, allowReplaced)
}

func (a *HierarchicalAPI) SubnetStateGetValidators(ctx context.Context, id address.SubnetID) ([]hierarchical.Validator, error) {
	return a.SubnetAPI.SubnetStateGetValidators(ctx, id)
}
