package api

import (
	"context"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/types"
)

type HierarchicalCns interface {
	AddSubnet(ctx context.Context, addr address.Address, parent address.SubnetID, name string, cns uint64, stake abi.TokenAmount, checkPeriod abi.ChainEpoch, threshold abi.ChainEpoch, p *hierarchical.ConsensusParams) (address.Address, error) // perm:write
	JoinSubnet(ctx context.Context, wallet address.Address, value abi.TokenAmount, id address.SubnetID, validatorAddr string) (cid.Cid, error)                                                                                                    // perm:write
	SyncSubnet(ctx context.Context, id address.SubnetID, stop bool) error                                                                                                                                                                         // perm:write
	MineSubnet(ctx context.Context, wallet address.Address, id address.SubnetID, stop bool, p *hierarchical.MiningParams) error                                                                                                                   // perm:read
	LeaveSubnet(ctx context.Context, wallet address.Address, id address.SubnetID) (cid.Cid, error)                                                                                                                                                // perm:write
	ListSubnets(ctx context.Context, id address.SubnetID) ([]sca.SubnetOutput, error)                                                                                                                                                             // perm:read
	KillSubnet(ctx context.Context, wallet address.Address, id address.SubnetID) (cid.Cid, error)                                                                                                                                                 // perm:write
	ListCheckpoints(ctx context.Context, id address.SubnetID, num int) ([]*schema.Checkpoint, error)                                                                                                                                              // perm:read
	ValidateCheckpoint(ctx context.Context, id address.SubnetID, epoch abi.ChainEpoch) (*schema.Checkpoint, error)                                                                                                                                // perm:read
	GetCrossMsgsPool(ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.Message, error)                                                                                                                                   // perm:read
	GetUnverifiedCrossMsgsPool(ctx context.Context, id address.SubnetID, height abi.ChainEpoch) ([]*types.UnverifiedCrossMsg, error)                                                                                                              // perm:read
	FundSubnet(ctx context.Context, wallet address.Address, id address.SubnetID, value abi.TokenAmount) (cid.Cid, error)                                                                                                                          // perm:write
	ReleaseFunds(ctx context.Context, wallet address.Address, id address.SubnetID, value abi.TokenAmount) (cid.Cid, error)                                                                                                                        // perm:write
	CrossMsgResolve(ctx context.Context, id address.SubnetID, c cid.Cid, from address.SubnetID) ([]types.Message, error)                                                                                                                          // perm:read
	LockState(ctx context.Context, wallet address.Address, actor address.Address, subnet address.SubnetID, method abi.MethodNum) (cid.Cid, error)                                                                                                 // perm:write
	UnlockState(ctx context.Context, wallet address.Address, actor address.Address, subnet address.SubnetID, method abi.MethodNum) error                                                                                                          // perm:write
	InitAtomicExec(ctx context.Context, wallet address.Address, inputs map[string]sca.LockedState, msgs []types.Message) (cid.Cid, error)                                                                                                         // perm:write
	ListAtomicExecs(ctx context.Context, id address.SubnetID, addr address.Address) ([]sca.AtomicExec, error)                                                                                                                                     // perm:read
	ComputeAndSubmitExec(ctx context.Context, wallet address.Address, id address.SubnetID, execID cid.Cid) (sca.ExecStatus, error)                                                                                                                // perm:write
	AbortAtomicExec(ctx context.Context, wallet address.Address, id address.SubnetID, execID cid.Cid) (sca.ExecStatus, error)                                                                                                                     // perm:write
	SubnetChainNotify(context.Context, address.SubnetID) (<-chan []*HeadChange, error)                                                                                                                                                            // perm:read
	SubnetChainHead(context.Context, address.SubnetID) (*types.TipSet, error)                                                                                                                                                                     // perm:read
	SubnetStateGetActor(ctx context.Context, id address.SubnetID, addr address.Address, tsk types.TipSetKey) (*types.Actor, error)                                                                                                                // perm:read
	SubnetStateWaitMsg(ctx context.Context, id address.SubnetID, cid cid.Cid, confidence uint64, limit abi.ChainEpoch, allowReplaced bool) (*MsgLookup, error)                                                                                    // perm:read
	SubnetStateGetValidators(ctx context.Context, id address.SubnetID) ([]hierarchical.Validator, error)                                                                                                                                          // perm:read
}
