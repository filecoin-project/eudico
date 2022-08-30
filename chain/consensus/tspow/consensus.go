package tspow

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.opencensus.io/stats"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	param "github.com/filecoin-project/lotus/chain/consensus/common/params"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"

	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const (
	MaxDiffLookback = 70

	// MaxHeightDrift is the epochs number to define blocks that will be rejected,
	// if there are more than MaxHeightDrift epochs above the theoretical max height
	// based on systime.
	MaxHeightDrift = 5
)

var (
	_   consensus.Consensus = &TSPoW{}
	log                     = logging.Logger("tspow-consensus")
)

type TSPoW struct {
	// The interface for accessing and putting tipsets into local storage
	store *store.ChainStore

	// handle to the random beacon for verification
	beacon beacon.Schedule

	// the state manager handles making state queries
	sm *stmgr.StateManager

	verifier storiface.Verifier

	genesis *types.TipSet

	subMgr subnet.Manager

	r *resolver.Resolver

	netName address.SubnetID
}

func NewTSPoWConsensus(
	ctx context.Context,
	sm *stmgr.StateManager,
	submgr subnet.Manager,
	beacon beacon.Schedule,
	r *resolver.Resolver,
	verifier storiface.Verifier,
	genesis chain.Genesis,
	netName dtypes.NetworkName,
) consensus.Consensus {
	sn, _ := address.SubnetIDFromString(string(netName))
	return &TSPoW{
		store:    sm.ChainStore(),
		beacon:   beacon,
		r:        r,
		sm:       sm,
		verifier: verifier,
		genesis:  genesis,
		subMgr:   submgr,
		netName:  sn,
	}
}

func (tsp *TSPoW) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	b, err := common.PrepareBlockForSignature(ctx, tsp.sm, bt)
	if err != nil {
		return nil, err
	}
	next := b.Header

	tgt := big.Zero()
	tgt.SetBytes(next.Ticket.VRFProof)

	bestH := *next
	for i := 0; i < 10000; i++ {
		next.ElectionProof = &types.ElectionProof{
			VRFProof: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		}
		rand.Read(next.ElectionProof.VRFProof) //nolint:errcheck
		if work(&bestH).LessThan(work(next)) {
			bestH = *next
			if work(next).GreaterThanEqual(tgt) {
				break
			}
		}
	}
	next = &bestH

	if work(next).LessThan(tgt) {
		return nil, nil
	}

	err = common.SignBlock(ctx, w, b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (tsp *TSPoW) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	if err := common.BlockSanityChecks(hierarchical.PoW, b.Header); err != nil {
		return fmt.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tsp.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
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

	// check work above threshold
	w := work(b.Header)
	thr := big.Zero()
	thr.SetBytes(b.Header.Ticket.VRFProof)
	if thr.GreaterThan(w) {
		return fmt.Errorf("block below work threshold")
	}

	// check work threshold
	if b.Header.Height < MaxDiffLookback {
		if !thr.Equals(param.GenesisWorkTarget) {
			return fmt.Errorf("wrong work target")
		}
	} else {
		//
		lbr := b.Header.Height - DiffLookback(baseTs.Height())
		lbts, err := tsp.store.GetTipsetByHeight(ctx, lbr, baseTs, false)
		if err != nil {
			return fmt.Errorf("failed to get lookback tipset+1: %w", err)
		}

		expDiff := Difficulty(baseTs, lbts)
		if !thr.Equals(expDiff) {
			return fmt.Errorf("expected adjusted difficulty %s, was %s (act-exp: %s)", expDiff, thr, big.Sub(thr, expDiff))
		}
	}

	msgsChecks := common.CheckMsgs(ctx, tsp.store, tsp.sm, tsp.subMgr, tsp.r, tsp.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := tsp.minerIsValid(h.Miner); err != nil {
			return fmt.Errorf("minerIsValid failed: %w", err)
		}
		return nil
	})

	pweight, err := Weight(context.TODO(), nil, baseTs)
	if err != nil {
		return fmt.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return fmt.Errorf("parrent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	stateRootCheck := common.CheckStateRoot(ctx, tsp.store, tsp.sm, b, baseTs)

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

func (tsp *TSPoW) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if tsp.genesis == nil {
		return false
	}

	now := uint64(build.Clock.Now().Unix())
	return epoch > (abi.ChainEpoch((now-tsp.genesis.MinTimestamp())/build.BlockDelaySecs) + MaxHeightDrift)
}

func (tsp *TSPoW) minerIsValid(maddr address.Address) error {
	switch maddr.Protocol() {
	case address.BLS:
		fallthrough
	case address.SECP256K1:
		return nil
	}

	return fmt.Errorf("miner address must be a key")
}

func (tsp *TSPoW) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
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

	reject, err := tsp.validateBlockHeader(ctx, blk.Header)
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

func (tsp *TSPoW) validateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	if err := tsp.minerIsValid(b.Miner); err != nil {
		return err.Error(), err
	}

	err = sigs.CheckBlockSignature(ctx, b, b.Miner)
	if err != nil {
		log.Errorf("block signature verification failed: %s", err)
		return "signature_verification_failed", err
	}

	return "", nil
}

func (tsp *TSPoW) Type() hierarchical.ConsensusType {
	return hierarchical.PoW
}
