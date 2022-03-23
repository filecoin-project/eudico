package hierarchical

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	snmgr "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/manager"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	"go.uber.org/fx"
)

var _ api.HierarchicalCns = &HierarchicalAPI{}

type HierarchicalAPI struct {
	fx.In

	Sub *snmgr.SubnetMgr
}

func (a *HierarchicalAPI) AddSubnet(
	ctx context.Context, wallet address.Address,
	parent address.SubnetID, name string,
	consensus uint64, minerStake abi.TokenAmount,
	checkPeriod abi.ChainEpoch,
	delegminer address.Address) (address.Address, error) {

	return a.Sub.AddSubnet(ctx, wallet, parent, name, consensus, minerStake, checkPeriod, delegminer)
}

func (a *HierarchicalAPI) JoinSubnet(ctx context.Context, wallet address.Address,
	value abi.TokenAmount, id address.SubnetID) (cid.Cid, error) {
	return a.Sub.JoinSubnet(ctx, wallet, value, id)
}

func (a *HierarchicalAPI) SyncSubnet(ctx context.Context, id address.SubnetID, stop bool) error {
	return a.Sub.SyncSubnet(ctx, id, stop)
}

func (a *HierarchicalAPI) MineSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID, stop bool) error {
	return a.Sub.MineSubnet(ctx, wallet, id, stop)
}

func (a *HierarchicalAPI) LeaveSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID) (cid.Cid, error) {
	return a.Sub.LeaveSubnet(ctx, wallet, id)
}

func (a *HierarchicalAPI) KillSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID) (cid.Cid, error) {
	return a.Sub.KillSubnet(ctx, wallet, id)
}

func (a *HierarchicalAPI) ListCheckpoints(ctx context.Context,
	id address.SubnetID, num int) ([]*schema.Checkpoint, error) {
	return a.Sub.ListCheckpoints(ctx, id, num)
}

func (a *HierarchicalAPI) ValidateCheckpoint(ctx context.Context,
	id address.SubnetID, epoch abi.ChainEpoch) (*schema.Checkpoint, error) {
	return a.Sub.ValidateCheckpoint(ctx, id, epoch)
}

func (a *HierarchicalAPI) GetCrossMsgsPool(ctx context.Context, id address.SubnetID,
	height abi.ChainEpoch) ([]*types.Message, error) {
	return a.Sub.GetCrossMsgsPool(ctx, id, height)
}

func (a *HierarchicalAPI) FundSubnet(ctx context.Context, wallet address.Address,
	id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) {
	return a.Sub.FundSubnet(ctx, wallet, id, value)
}

func (a *HierarchicalAPI) ReleaseFunds(ctx context.Context, wallet address.Address,
	id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) {
	return a.Sub.ReleaseFunds(ctx, wallet, id, value)
}

func (a *HierarchicalAPI) CrossMsgResolve(ctx context.Context, id address.SubnetID,
	c cid.Cid, from address.SubnetID) ([]types.Message, error) {
	return a.Sub.CrossMsgResolve(ctx, id, c, from)
}
