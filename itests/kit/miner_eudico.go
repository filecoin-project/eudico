package kit

import (
	"context"
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
)

const (
	finalityTimeout  = 600
	balanceSleepTime = 3
)

func WaitActorBalance(ctx context.Context, addr addr.Address, balance big.Int, limit int, api api.FullNode) (int, error) {
	heads, err := api.ChainNotify(ctx)
	if err != nil {
		return 0, err
	}

	n := 0
	timer := time.After(finalityTimeout * time.Second)

	for n < limit {
		select {
		case <-ctx.Done():
			return 0, xerrors.New("closed channel")
		case <-heads:
			a, err := api.StateGetActor(ctx, addr, types.EmptyTSK)
			switch {
			case err != nil && !strings.Contains(err.Error(), types.ErrActorNotFound.Error()):
				return 0, err
			case err != nil && strings.Contains(err.Error(), types.ErrActorNotFound.Error()):
				n++
			case err == nil:
				if big.Cmp(balance, a.Balance) == 0 {
					return n, nil
				}
				n++
			}
		case <-timer:
			return 0, xerrors.New("finality timer exceeded")
		}
	}
	return 0, xerrors.New("unable to wait the target amount")
}

func WaitSubnetActorBalance(ctx context.Context, subnetAddr addr.SubnetID, addr addr.Address, balance big.Int, limit int, api api.FullNode) (int, error) {
	heads, err := api.SubnetChainNotify(ctx, subnetAddr)
	if err != nil {
		return 0, err
	}
	n := 0
	timer := time.After(finalityTimeout * time.Second)

	for n < limit {
		select {
		case <-ctx.Done():
			return 0, xerrors.New("closed channel")
		case <-heads:
			a, err := api.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
			switch {
			case err != nil && !strings.Contains(err.Error(), types.ErrActorNotFound.Error()):
				return 0, err
			case err != nil && strings.Contains(err.Error(), types.ErrActorNotFound.Error()):
				n++
			case err == nil:
				if big.Cmp(balance, a.Balance) == 0 {
					return n, nil
				}
				n++
			}
		case <-timer:
			return 0, xerrors.New("finality timer exceeded")
		}
	}
	return 0, xerrors.New("unable to wait the target amount")
}

func WaitForBalance(ctx context.Context, addr addr.Address, balance uint64, api api.FullNode) error {
	currentBalance, err := api.WalletBalance(ctx, addr)
	if err != nil {
		return err
	}
	targetBalance := types.FromFil(balance)
	ticker := time.NewTicker(balanceSleepTime * time.Second)
	defer ticker.Stop()

	timer := time.After(finalityTimeout * time.Second)

	for big.Cmp(currentBalance, targetBalance) != 1 {
		select {
		case <-ctx.Done():
			return xerrors.New("closed channel")
		case <-ticker.C:
			currentBalance, err = api.WalletBalance(ctx, addr)
			if err != nil {
				return err
			}
		case <-timer:
			return xerrors.New("balance timer exceeded")
		}
	}

	return nil
}

func SubnetPerformHeightCheckForBlocks(ctx context.Context, validatedBlocksNumber int, subnetAddr addr.SubnetID, api api.FullNode) error {
	subnetHeads, err := api.SubnetChainNotify(ctx, subnetAddr)
	if err != nil {
		return err
	}
	if validatedBlocksNumber < 2 || validatedBlocksNumber > 100 {
		return xerrors.New("wrong validated blocks number")
	}
	initHead := (<-subnetHeads)[0]
	prevHeight := initHead.Val.Height()

	i := 1
	for i < validatedBlocksNumber {
		select {
		case <-ctx.Done():
			return xerrors.New("closed channel")
		case <-subnetHeads:
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
