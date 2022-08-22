package kit

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/xerrors"

	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	napi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
)

const (
	finalityTimeout  = 1200
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

func WaitSubnetActorBalance(ctx context.Context, subnetAddr addr.SubnetID, addr addr.Address, balance big.Int, api napi.FullNode) (int, error) {
	heads, err := getSubnetChainHead(ctx, subnetAddr, api)
	if err != nil {
		return 0, err
	}

	n := 0
	timer := time.After(finalityTimeout * time.Second)

	for {
		select {
		case <-ctx.Done():
			return 0, xerrors.New("context closed")
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

// SubnetMinerMinesBlocks checks that the specified miner has mined `targetBlockNumber` blocks of `blocks` in the subnet.
func SubnetMinerMinesBlocks(ctx context.Context, targetBlockNumber, blocks int, subnetAddr addr.SubnetID, miner addr.Address, api napi.FullNode) error {
	subnetHeads, err := getSubnetChainHead(ctx, subnetAddr, api)
	if err != nil {
		return err
	}
	if blocks < 2 || blocks > 100 || targetBlockNumber > blocks {
		return xerrors.New("wrong blocks number")
	}

	// ChainNotify returns channel with chain head updates.
	// First message is guaranteed to be of len == 1, and type == 'current'.
	// Without forks we can expect that its len always to be 1.
	initHead := <-subnetHeads
	if len(initHead) < 1 {
		return xerrors.New("empty chain head")
	}
	currHeight := initHead[0].Val.Height()

	head, err := api.SubnetChainHead(ctx, subnetAddr)
	if err != nil {
		return err
	}
	height := head.Height()
	if height != currHeight {
		return xerrors.New("wrong initial block height")
	}

	minerAddrs := make(map[addr.Address]int)
	i := 0
	minerMinedBlocks := 0
	for i < blocks {
		select {
		case <-ctx.Done():
			return xerrors.New("closed channel")
		case heads := <-subnetHeads:
			if len(heads) != 1 {
				return xerrors.New("chain head length is not one")
			}

			if targetBlockNumber == minerMinedBlocks {
				return nil
			}
			height := heads[0].Val.Height()

			if height <= currHeight {
				return xerrors.Errorf("wrong %d block height: prev block height - %d, current head height - %d",
					i, currHeight, height)
			}
			currHeight = height

			minerAddrs[heads[0].Val.Blocks()[0].Miner]++

			if heads[0].Val.Blocks()[0].Miner == miner {
				minerMinedBlocks++
			}

			i++
		}
	}

	if targetBlockNumber == minerMinedBlocks {
		return nil
	}

	fmt.Println(minerAddrs)

	return fmt.Errorf("failed to mine %d blocks", targetBlockNumber)

}

// SubnetHeightCheckForBlocks checks that there will be mined `blockNumber` blocks in the specified  subnet.
func SubnetHeightCheckForBlocks(ctx context.Context, blockNumber int, subnetAddr addr.SubnetID, api napi.FullNode) error {
	subnetHeads, err := getSubnetChainHead(ctx, subnetAddr, api)
	if err != nil {
		return err
	}
	if blockNumber < 2 || blockNumber > 100 {
		return xerrors.New("wrong validated blocks number")
	}

	// ChainNotify returns channel with chain head updates.
	// First message is guaranteed to be of len == 1, and type == 'current'.
	// Without forks we can expect that its len always to be 1.
	initHead := <-subnetHeads
	if len(initHead) < 1 {
		return xerrors.New("empty chain head")
	}
	currHeight := initHead[0].Val.Height()

	head, err := api.SubnetChainHead(ctx, subnetAddr)
	if err != nil {
		return err
	}
	height := head.Height()
	if height != currHeight {
		return xerrors.New("wrong initial block height")
	}

	i := 1
	for i < blockNumber {
		select {
		case <-ctx.Done():
			return xerrors.New("closed channel")
		case heads := <-subnetHeads:
			if len(heads) != 1 {
				return xerrors.New("chain head length is not one")
			}

			if i > blockNumber {
				return nil
			}
			height := heads[0].Val.Height()

			if height <= currHeight {
				return xerrors.Errorf("wrong %d block height: prev block height - %d, current head height - %d",
					i, currHeight, height)
			}
			currHeight = height
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

func GetFreeLocalAddr() (addr string, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close() // nolint
			return fmt.Sprintf("127.0.0.1:%d", l.Addr().(*net.TCPAddr).Port), nil
		}
	}
	return
}

func GetFreeLibp2pLocalAddr() (m multiaddr.Multiaddr, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close() // nolint
			return multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", l.Addr().(*net.TCPAddr).Port))
		}
	}
	return
}

func GetLibp2pAddr(privKey []byte) (m multiaddr.Multiaddr, err error) {
	saddr, err := GetFreeLibp2pLocalAddr()
	if err != nil {
		return nil, err
	}

	priv, err := crypto.UnmarshalPrivateKey(privKey)
	if err != nil {
		return nil, err
	}

	peerID, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		panic(err)
	}

	peerInfo := peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{saddr},
	}

	addrs, err := peer.AddrInfoToP2pAddrs(&peerInfo)
	if err != nil {
		return nil, err
	}

	return addrs[0], nil
}

func NodeLibp2pAddr(node *TestFullNode) (m multiaddr.Multiaddr, err error) {
	privKey, err := node.PrivKey(context.Background())
	if err != nil {
		return nil, err
	}

	a, err := GetLibp2pAddr(privKey)
	if err != nil {
		return nil, err
	}

	return a, nil
}
