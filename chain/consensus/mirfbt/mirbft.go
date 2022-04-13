// Package mirbft implements integration with MirBFT library,
// available at https://github.com/hyperledger-labs/mirbft.
package mirbft

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const (
	MaxHeightDrift = 5
)

var (
	log                     = logging.Logger("mir-consensus")
	_   consensus.Consensus = &MirBFT{}
)

type MirBFT struct {
	store    *store.ChainStore
	beacon   beacon.Schedule
	sm       *stmgr.StateManager
	verifier ffiwrapper.Verifier
	genesis  *types.TipSet
	subMgr   subnet.SubnetMgr
	netName  address.SubnetID
	resolver *resolver.Resolver
	node     *Node
}

func NewConsensus(
	ctx context.Context,
	sm *stmgr.StateManager,
	submgr subnet.SubnetMgr,
	b beacon.Schedule,
	r *resolver.Resolver,
	v ffiwrapper.Verifier,
	g chain.Genesis,
	netName dtypes.NetworkName,
) (consensus.Consensus, error) {
	subnetID := address.SubnetID(netName)
	log.Infof("New MirBFT consensus for %s subnet", subnetID)

	n, err := NewNode(uint64(0))
	if err != nil {
		return nil, err
	}

	go n.Serve(ctx)

	return &MirBFT{
		store:    sm.ChainStore(),
		beacon:   b,
		sm:       sm,
		verifier: v,
		genesis:  g,
		subMgr:   submgr,
		netName:  subnetID,
		resolver: r,
		node:     n,
	}, nil
}

func (m *MirBFT) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("starting block validation process at @%d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.MirBFT, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := m.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
	if err != nil {
		return xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	// fast checks first
	if h.Height != baseTs.Height()+1 {
		return xerrors.Errorf("block height not parent height+1: %d != %d", h.Height, baseTs.Height()+1)
	}

	now := uint64(build.Clock.Now().Unix())
	if h.Timestamp > now+build.AllowableClockDriftSecs {
		return xerrors.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	}
	if h.Timestamp > now {
		log.Warn("Got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	}

	msgsChecks := common.CheckMsgs(ctx, m.store, m.sm, m.subMgr, m.resolver, m.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := m.minerIsValid(b.Header.Miner); err != nil {
			return xerrors.Errorf("minerIsValid failed: %w", err)
		}
		return nil
	})

	pweight, err := Weight(context.TODO(), nil, baseTs)
	if err != nil {
		return xerrors.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return xerrors.Errorf("parent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	stateRootCheck := common.CheckStateRoot(ctx, m.store, m.sm, b, baseTs)

	await := []async.ErrorFuture{
		minerCheck,
		stateRootCheck,
	}

	await = append(await, msgsChecks...)

	var merr error
	for _, fut := range await {
		if err := fut.AwaitContext(ctx); err != nil {
			merr = multierror.Append(merr, err)
		}
	}
	if merr != nil {
		mulErr := merr.(*multierror.Error)
		mulErr.ErrorFormat = func(es []error) string {
			if len(es) == 1 {
				return fmt.Sprintf("1 error occurred:\n\t* %+v\n\n", es[0])
			}

			points := make([]string, len(es))
			for i, err := range es {
				points[i] = fmt.Sprintf("* %+v", err)
			}

			return fmt.Sprintf(
				"%d errors occurred:\n\t%s\n\n",
				len(es), strings.Join(points, "\n\t"))
		}
		return mulErr
	}

	log.Infof("block at @%d is valid", b.Header.Height)

	return nil
}

func (m *MirBFT) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	if self {
		return common.ValidateLocalBlock(ctx, msg)
	}

	// track validation time
	begin := build.Clock.Now()
	defer func() {
		log.Debugf("block validation time: %s", build.Clock.Since(begin))
	}()

	stats.Record(ctx, metrics.BlockReceived.M(1))

	recordFailureFlagPeer := func(what string) {
		// bv.Validate will flag the peer in that case
		panic(what)
	}

	blk, what, err := common.DecodeAndCheckBlock(msg)
	if err != nil {
		log.Error("got invalid block over pubsub: ", err)
		recordFailureFlagPeer(what)
		return pubsub.ValidationReject, what
	}

	// validate the block meta: the Message CID in the header must match the included messages
	err = common.ValidateMsgMeta(ctx, blk)
	if err != nil {
		log.Warnf("error validating message metadata: %s", err)
		recordFailureFlagPeer("invalid_block_meta")
		return pubsub.ValidationReject, "invalid_block_meta"
	}

	// all good, accept the block
	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func (m *MirBFT) minerIsValid(maddr address.Address) error {
	switch maddr.Protocol() {
	case address.BLS:
		fallthrough
	case address.SECP256K1:
		return nil
	}

	return xerrors.Errorf("miner address must be a key")
}

// IsEpochBeyondCurrMax is used in Filcns to detect delayed blocks.
// We are currently using defaults here and not worrying about it.
// We will consider potential changes of Consensus interface in https://github.com/filecoin-project/eudico/issues/143.
func (m *MirBFT) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if m.genesis == nil {
		return false
	}

	now := uint64(build.Clock.Now().Unix())
	return epoch > (abi.ChainEpoch((now-m.genesis.MinTimestamp())/build.BlockDelaySecs) + MaxHeightDrift)
}

func (m *MirBFT) Type() hierarchical.ConsensusType {
	return hierarchical.MirBFT
}

// Weight defines weight.
// We are just using a default weight for all subnet consensus algorithms.
func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}
