package api

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/sharding/actors/naming"
	"github.com/ipfs/go-cid"
)

type Sharding interface {
	AddShard(ctx context.Context, wallet address.Address, parent naming.SubnetID, name string, consensus uint64, minerStake abi.TokenAmount,
		delegminer address.Address) (address.Address, error) // perm:write
	JoinShard(ctx context.Context, wallet address.Address, value abi.TokenAmount, id naming.SubnetID) (cid.Cid, error) // perm:write
	Mine(ctx context.Context, wallet address.Address, id naming.SubnetID, stop bool) error                             // perm:write
	Leave(ctx context.Context, wallet address.Address, id naming.SubnetID) (cid.Cid, error)                            // perm:write
	Kill(ctx context.Context, wallet address.Address, id naming.SubnetID) (cid.Cid, error)                             // perm:write
}
