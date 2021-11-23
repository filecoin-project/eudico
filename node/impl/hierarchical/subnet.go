package hierarchical

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/ipfs/go-cid"
	"go.uber.org/fx"
)

var _ api.HierarchicalCns = &HierarchicalAPI{}

type HierarchicalAPI struct {
	fx.In

	Sub *subnet.SubnetMgr
}

func (a *HierarchicalAPI) AddSubnet(
	ctx context.Context, wallet address.Address,
	parent hierarchical.SubnetID, name string,
	consensus uint64, minerStake abi.TokenAmount,
	delegminer address.Address) (address.Address, error) {

	return a.Sub.AddSubnet(ctx, wallet, parent, name, consensus, minerStake, delegminer)
}

func (a *HierarchicalAPI) JoinSubnet(ctx context.Context, wallet address.Address,
	value abi.TokenAmount, id hierarchical.SubnetID) (cid.Cid, error) {
	return a.Sub.JoinSubnet(ctx, wallet, value, id)
}

func (a *HierarchicalAPI) MineSubnet(ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID, stop bool) error {
	return a.Sub.MineSubnet(ctx, wallet, id, stop)
}

func (a *HierarchicalAPI) LeaveSubnet(ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID) (cid.Cid, error) {
	return a.Sub.LeaveSubnet(ctx, wallet, id)
}

func (a *HierarchicalAPI) KillSubnet(ctx context.Context, wallet address.Address,
	id hierarchical.SubnetID) (cid.Cid, error) {
	return a.Sub.KillSubnet(ctx, wallet, id)
}
