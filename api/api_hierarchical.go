package api

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

type HierarchicalCns interface {
	AddSubnet(ctx context.Context, wallet address.Address, parent address.SubnetID, name string, consensus uint64, minerStake abi.TokenAmount,
		checkperiod abi.ChainEpoch, delegminer address.Address) (address.Address, error) // perm:write
	JoinSubnet(ctx context.Context, wallet address.Address, value abi.TokenAmount, id address.SubnetID) (cid.Cid, error)   // perm:write
	SyncSubnet(ctx context.Context, id address.SubnetID, stop bool) error                                                  // perm:write
	MineSubnet(ctx context.Context, wallet address.Address, id address.SubnetID, stop bool) error                          // perm:read
	LeaveSubnet(ctx context.Context, wallet address.Address, id address.SubnetID) (cid.Cid, error)                         // perm:write
	KillSubnet(ctx context.Context, wallet address.Address, id address.SubnetID) (cid.Cid, error)                          // perm:write
	ListCheckpoints(ctx context.Context, id address.SubnetID, num int) ([]*schema.Checkpoint, error)                       // perm:read
	ValidateCheckpoint(ctx context.Context, id address.SubnetID, epoch abi.ChainEpoch) (*schema.Checkpoint, error)         // perm:read
	GetCrossMsgsPool(ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.Message, error)            // perm:read
	FundSubnet(ctx context.Context, wallet address.Address, id address.SubnetID, value abi.TokenAmount) (cid.Cid, error)   // perm:write
	ReleaseFunds(ctx context.Context, wallet address.Address, id address.SubnetID, value abi.TokenAmount) (cid.Cid, error) // perm:write
	CrossMsgResolve(ctx context.Context, id address.SubnetID, c cid.Cid, from address.SubnetID) ([]types.Message, error)   // perm:read
}
