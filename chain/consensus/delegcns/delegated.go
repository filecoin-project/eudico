package delegcns

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"

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
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

var _ consensus.Consensus = &Delegated{}

var log = logging.Logger("delegated-consensus")

type Delegated struct {
	// The interface for accessing and putting tipsets into local storage
	store *store.ChainStore

	// handle to the random beacon for verification
	beacon beacon.Schedule

	// the state manager handles making state queries
	sm *stmgr.StateManager

	verifier ffiwrapper.Verifier

	genesis *types.TipSet

	subMgr subnet.SubnetMgr

	r *resolver.Resolver

	// We could get network name from state manager, but with this
	// we avoid having fetch it for every block validation.
	netName address.SubnetID
}

var producer = func() address.Address {
	a, err := address.NewFromString("t0100")
	if err != nil {
		panic(err)
	}
	return a
}()

// Blocks that are more than MaxHeightDrift epochs above
// the theoretical max height based on systime are quickly rejected
const MaxHeightDrift = 5

func NewDelegatedConsensus(sm *stmgr.StateManager, submgr subnet.SubnetMgr, beacon beacon.Schedule,
	r *resolver.Resolver, verifier ffiwrapper.Verifier,
	genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return &Delegated{
		store:    sm.ChainStore(),
		beacon:   beacon,
		r:        r,
		sm:       sm,
		verifier: verifier,
		genesis:  genesis,
		subMgr:   submgr,
		netName:  address.SubnetID(netName),
	}
}

func (deleg *Delegated) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	if err := common.BlockSanityChecks(hierarchical.Delegated, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header
	baseTs, err := deleg.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
	if err != nil {
		return xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	// fast checks first
	if h.Height <= baseTs.Height() {
		return xerrors.Errorf("block height not greater than parent height: %d != %d", h.Height, baseTs.Height())
	}

	nulls := h.Height - (baseTs.Height() + 1)
	if tgtTs := baseTs.MinTimestamp() + build.BlockDelaySecs*uint64(nulls+1); h.Timestamp != tgtTs {
		return xerrors.Errorf("block has wrong timestamp: %d != %d", h.Timestamp, tgtTs)
	}

	now := uint64(build.Clock.Now().Unix())
	if h.Timestamp > now+build.AllowableClockDriftSecs {
		return xerrors.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	}
	if h.Timestamp > now {
		log.Warn("Got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	}

	msgsChecks := common.CheckMsgs(ctx, deleg.store, deleg.sm, deleg.subMgr, deleg.r, deleg.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := deleg.minerIsValid(ctx, h.Miner, baseTs); err != nil {
			return xerrors.Errorf("minerIsValid failed: %w", err)
		}
		return nil
	})

	pweight, err := Weight(context.TODO(), nil, baseTs)
	if err != nil {
		return xerrors.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return xerrors.Errorf("parrent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	stateRootCheck := common.CheckStateRoot(ctx, deleg.store, deleg.sm, b, baseTs)

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

	return nil
}

func (deleg *Delegated) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if deleg.genesis == nil {
		return false
	}

	now := uint64(build.Clock.Now().Unix())
	return epoch > (abi.ChainEpoch((now-deleg.genesis.MinTimestamp())/build.BlockDelaySecs) + MaxHeightDrift)
}

func (deleg *Delegated) minerIsValid(ctx context.Context, maddr address.Address, baseTs *types.TipSet) error {
	ida, err := deleg.sm.LookupID(ctx, maddr, baseTs)
	if err != nil {
		return xerrors.Errorf("failed to load power actor: %w", err)
	}

	if ida != producer {
		return xerrors.Errorf("bad miner")
	}

	return nil
}

func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}

func (deleg *Delegated) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
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

	reject, err := deleg.validateBlockHeader(ctx, blk.Header)
	if err != nil {
		if reject == "" {
			log.Warn("ignoring block msg: ", err)
			return pubsub.ValidationIgnore, reject
		}
		recordFailureFlagPeer(reject)
		return pubsub.ValidationReject, reject
	}

	// all good, accept the block
	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func (deleg *Delegated) validateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	baseTs := deleg.store.GetHeaviestTipSet()

	if err := deleg.minerIsValid(ctx, b.Miner, baseTs); err != nil {
		return err.Error(), err
	}

	err = sigs.CheckBlockSignature(ctx, b, b.Miner)
	if err != nil {
		log.Errorf("block signature verification failed: %s", err)
		return "signature_verification_failed", err
	}

	return "", nil
}
