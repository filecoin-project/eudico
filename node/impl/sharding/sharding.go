package sharding

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/sharding"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/ipfs/go-cid"
	"go.uber.org/fx"
)

var _ api.Sharding = &ShardingAPI{}

type ShardingAPI struct {
	fx.In

	Sub *sharding.ShardingSub
}

func (a *ShardingAPI) AddShard(
	ctx context.Context, wallet address.Address,
	parent naming.SubnetID, name string,
	consensus uint64, minerStake abi.TokenAmount,
	delegminer address.Address) (address.Address, error) {

	return a.Sub.AddShard(ctx, wallet, parent, name, consensus, minerStake, delegminer)
}

func (a *ShardingAPI) JoinShard(ctx context.Context, wallet address.Address,
	value abi.TokenAmount, id naming.SubnetID) (cid.Cid, error) {
	return a.Sub.JoinShard(ctx, wallet, value, id)
}

func (a *ShardingAPI) Mine(ctx context.Context, wallet address.Address,
	id naming.SubnetID, stop bool) error {
	return a.Sub.Mine(ctx, wallet, id, stop)
}

func (a *ShardingAPI) Leave(ctx context.Context, wallet address.Address,
	id naming.SubnetID) (cid.Cid, error) {
	return a.Sub.Leave(ctx, wallet, id)
}

func (a *ShardingAPI) Kill(ctx context.Context, wallet address.Address,
	id naming.SubnetID) (cid.Cid, error) {
	return a.Sub.Kill(ctx, wallet, id)
}
