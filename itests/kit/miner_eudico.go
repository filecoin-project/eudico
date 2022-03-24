package kit

import (
	"context"
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
)

const (
	finalityTimeout  = 120
	balanceSleepTime = 3
)

func WaitForFinality(ctx context.Context, finalityBlockNumber int, newHeads <-chan []*api.HeadChange) error {
	blocks := 0
	timer := time.After(finalityTimeout * time.Second)
	for blocks < finalityBlockNumber {
		select {
		case <-ctx.Done():
			return xerrors.New("closed channel")
		case <-newHeads:
			blocks++
		case <-timer:
			return xerrors.New("finality timer exceeded")
		}
	}
	return nil
}

func WaitForBalance(ctx context.Context, addr addr.Address, balance uint64, api api.FullNode) error {
	wait := true
	targetBalance := types.FromFil(balance)
	for wait {
		currentBalance, err := api.WalletBalance(ctx, addr)
		if err != nil {
			return err
		}
		time.Sleep(balanceSleepTime * time.Second)
		if big.Cmp(currentBalance, targetBalance) == 1 {
			wait = false
		}
	}
	return nil
}

func WaitForBalanceDiff(ctx context.Context, addr addr.Address, diff uint64, api api.FullNode) error {
	wait := true
	diffBalance := types.FromFil(diff)
	baseBalance, err := api.WalletBalance(ctx, addr)
	if err != nil {
		return err
	}
	targetBalance := big.Add(baseBalance, diffBalance)
	for wait {
		currentBalance, err := api.WalletBalance(ctx, addr)
		if err != nil {
			return err
		}
		time.Sleep(balanceSleepTime * time.Second)
		if big.Cmp(currentBalance, targetBalance) == 1 {
			wait = false
		}
	}
	return nil
}

func PerformBasicCheckForSubnetBlocks(ctx context.Context, validatedBlocksNumber int, subnetAddr addr.SubnetID, newHeads <-chan []*api.HeadChange, api api.FullNode) error {
	if validatedBlocksNumber < 2 || validatedBlocksNumber > 100 {
		return xerrors.New("wrong validated blocks number")
	}
	initHead := (<-newHeads)[0]
	prevHeight := initHead.Val.Height()

	i := 1
	for i < validatedBlocksNumber {
		select {
		case <-ctx.Done():
			return xerrors.New("closed channel")
		case <-newHeads:
			if i > validatedBlocksNumber {
				return nil
			}
			newHead, err := api.SubnetChainHead(ctx, subnetAddr)
			if err != nil {
				return err
			}
			if newHead.Height() <= prevHeight {
				return xerrors.New("new block height is not greater than the previous one")
			}
			prevHeight = newHead.Height()
			i++
		}
	}

	return nil
}
