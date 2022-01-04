package api

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

type HierarchicalCns interface {
	AddSubnet(ctx context.Context, wallet address.Address, parent hierarchical.SubnetID, name string, consensus uint64, minerStake abi.TokenAmount,
		checkperiod abi.ChainEpoch, delegminer address.Address) (address.Address, error) // perm:write
	JoinSubnet(ctx context.Context, wallet address.Address, value abi.TokenAmount, id hierarchical.SubnetID) (cid.Cid, error) // perm:write
	MineSubnet(ctx context.Context, wallet address.Address, id hierarchical.SubnetID, stop bool) error                        // perm:write
	LeaveSubnet(ctx context.Context, wallet address.Address, id hierarchical.SubnetID) (cid.Cid, error)                       // perm:write
	KillSubnet(ctx context.Context, wallet address.Address, id hierarchical.SubnetID) (cid.Cid, error)                        // perm:write
	ListCheckpoints(ctx context.Context, id hierarchical.SubnetID, num int) ([]*schema.Checkpoint, error)                     // perm:read
	ValidateCheckpoint(ctx context.Context, id hierarchical.SubnetID, epoch abi.ChainEpoch) (*schema.Checkpoint, error)       // perm:read
	GetCrossMsgsPool(ctx context.Context, id hierarchical.SubnetID, num int) ([]*types.Message, error)                        // perm:read
	FundSubnet(ctx context.Context, wallet address.Address, id hierarchical.SubnetID, value abi.TokenAmount) (cid.Cid, error) // perm:write
}
