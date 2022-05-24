// Package mir implements Eudico consensus in Mir framework.
package mir

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.opencensus.io/stats"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	lapi "github.com/filecoin-project/lotus/api"
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
	SubmitInterval = 5000 * time.Millisecond
	ValidatorsEnv  = "EUDICO_MIR_VALIDATORS"
)

var (
	log                     = logging.Logger("mir-consensus")
	_   consensus.Consensus = &Mir{}
)

type Mir struct {
	store    *store.ChainStore
	beacon   beacon.Schedule
	sm       *stmgr.StateManager
	verifier ffiwrapper.Verifier
	genesis  *types.TipSet
	subMgr   subnet.SubnetMgr
	netName  address.SubnetID
	resolver *resolver.Resolver
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
	log.Infof("New Mir consensus for %s subnet", subnetID)

	return &Mir{
		store:    sm.ChainStore(),
		beacon:   b,
		sm:       sm,
		verifier: v,
		genesis:  g,
		subMgr:   submgr,
		netName:  subnetID,
		resolver: r,
	}, nil
}

// CreateBlock creates a Filecoin block from the input block template.
func (bft *Mir) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	b, err := common.PrepareBlockForSignature(ctx, bft.sm, bt)
	if err != nil {
		return nil, err
	}

	err = common.SignBlock(ctx, w, b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (bft *Mir) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("starting block validation process at @%d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.Mir, b.Header); err != nil {
		return fmt.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := bft.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
	if err != nil {
		return fmt.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	// fast checks first
	if h.Height != baseTs.Height()+1 {
		return fmt.Errorf("block height not parent height+1: %d != %d", h.Height, baseTs.Height()+1)
	}

	now := uint64(build.Clock.Now().Unix())
	if h.Timestamp > now+build.AllowableClockDriftSecs {
		return fmt.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	}
	if h.Timestamp > now {
		log.Warn("Got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	}

	msgsChecks := common.CheckMsgs(ctx, bft.store, bft.sm, bft.subMgr, bft.resolver, bft.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := bft.minerIsValid(b.Header.Miner); err != nil {
			return fmt.Errorf("minerIsValid failed: %w", err)
		}
		return nil
	})

	pweight, err := Weight(ctx, nil, baseTs)
	if err != nil {
		return fmt.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return fmt.Errorf("parent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	stateRootCheck := common.CheckStateRoot(ctx, bft.store, bft.sm, b, baseTs)

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

			return fmt.Sprintf("%d errors occurred:\n\t%s\n\n", len(es), strings.Join(points, "\n\t"))
		}
		return mulErr
	}

	log.Infof("block at @%d is valid", b.Header.Height)

	return nil
}

func (bft *Mir) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
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

func (bft *Mir) minerIsValid(maddr address.Address) error {
	switch maddr.Protocol() {
	case address.BLS:
		fallthrough
	case address.SECP256K1:
		return nil
	}
	return fmt.Errorf("miner address must be a key")
}

// IsEpochBeyondCurrMax is used in Filcns to detect delayed blocks.
// We are currently using defaults here and not worrying about it.
// We will consider potential changes of Consensus interface in https://github.com/filecoin-project/eudico/issues/143.
func (bft *Mir) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if bft.genesis == nil {
		return false
	}

	now := uint64(build.Clock.Now().Unix())
	return epoch > (abi.ChainEpoch((now-bft.genesis.MinTimestamp())/build.BlockDelaySecs) + MaxHeightDrift)
}

func (bft *Mir) Type() hierarchical.ConsensusType {
	return hierarchical.Mir
}

// Weight defines weight.
// We are just using a default weight for all subnet consensus algorithms.
func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}
