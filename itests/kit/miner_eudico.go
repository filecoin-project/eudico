package kit

import (
	"context"
	"strings"
	"time"

	"golang.org/x/xerrors"

	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	napi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
)

const (
	finalityTimeout  = 600
	balanceSleepTime = 3
)

func getSubnetChainHead(ctx context.Context, subnetAddr addr.SubnetID, api napi.FullNode) (heads <-chan []*napi.HeadChange, err error) {
	switch subnetAddr.String() {
	case "root":
		heads, err = api.ChainNotify(ctx)
	default:
		heads, err = api.SubnetChainNotify(ctx, subnetAddr)
	}
	return
}

func getSubnetActor(ctx context.Context, subnetAddr addr.SubnetID, addr addr.Address, api napi.FullNode) (a *types.Actor, err error) {
	switch subnetAddr.String() {
	case "root":
		a, err = api.StateGetActor(ctx, addr, types.EmptyTSK)
	default:
		a, err = api.SubnetStateGetActor(ctx, subnetAddr, addr, types.EmptyTSK)
	}
	return
}

func WaitSubnetActorBalance(ctx context.Context, subnetAddr addr.SubnetID, addr addr.Address, balance big.Int, limit int, api napi.FullNode) (int, error) {
	heads, err := getSubnetChainHead(ctx, subnetAddr, api)
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
			a, err := getSubnetActor(ctx, subnetAddr, addr, api)
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

func WaitForBalance(ctx context.Context, addr addr.Address, balance uint64, api napi.FullNode) error {
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

func SubnetPerformHeightCheckForBlocks(ctx context.Context, validatedBlocksNumber int, subnetAddr addr.SubnetID, api napi.FullNode) error {
	subnetHeads, err := getSubnetChainHead(ctx, subnetAddr, api)
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

// MessageForSend send the message
// TODO: use MessageForSend from cli package.
func MessageForSend(ctx context.Context, s api.FullNode, params lcli.SendParams) (*api.MessagePrototype, error) {
	if params.From == addr.Undef {
		defaddr, err := s.WalletDefaultAddress(ctx)
		if err != nil {
			return nil, err
		}
		params.From = defaddr
	}

	msg := types.Message{
		From:  params.From,
		To:    params.To,
		Value: params.Val,

		Method: params.Method,
		Params: params.Params,
	}

	if params.GasPremium != nil {
		msg.GasPremium = *params.GasPremium
	} else {
		msg.GasPremium = types.NewInt(0)
	}
	if params.GasFeeCap != nil {
		msg.GasFeeCap = *params.GasFeeCap
	} else {
		msg.GasFeeCap = types.NewInt(0)
	}
	if params.GasLimit != nil {
		msg.GasLimit = *params.GasLimit
	} else {
		msg.GasLimit = 0
	}
	validNonce := false
	if params.Nonce != nil {
		msg.Nonce = *params.Nonce
		validNonce = true
	}

	prototype := &api.MessagePrototype{
		Message:    msg,
		ValidNonce: validNonce,
	}
	return prototype, nil
}
