package tspow

import (
	"context"
	"fmt"
	big2 "math/big"
	"sort"
	"strings"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/multiformats/go-multihash"
	"go.opencensus.io/stats"

	"github.com/Gurpartap/async"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"golang.org/x/xerrors"

	bstore "github.com/filecoin-project/lotus/blockstore"
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
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

var log = logging.Logger("tspow-consensus")

var _ consensus.Consensus = &TSPoW{}

const MaxDiffLookback = 70

func DiffLookback(baseH abi.ChainEpoch) abi.ChainEpoch {
	lb := ((baseH + 11) * 217) % (MaxDiffLookback - 40)
	return lb + 40
}

func Difficulty(baseTs, lbts *types.TipSet) big.Int {
	expLbTime := 100000 * uint64(DiffLookback(baseTs.Height())) * build.BlockDelaySecs
	actTime := (baseTs.Blocks()[0].Timestamp - lbts.Blocks()[0].Timestamp) * 100000

	actTime = expLbTime - uint64(int64(expLbTime-actTime)/100)

	// clamp max adjustment
	if actTime < expLbTime*99/100 {
		actTime = expLbTime * 99 / 100
	}
	if actTime > expLbTime*101/100 {
		actTime = expLbTime * 101 / 100
	}

	prevdiff := big.Zero()
	prevdiff.SetBytes(baseTs.Blocks()[0].Ticket.VRFProof)
	diff := big.Div(types.BigMul(prevdiff, big.NewInt(int64(expLbTime))), big.NewInt(int64(actTime)))

	pgen, _ := big2.NewFloat(0).SetInt(param.GenesisWorkTarget.Int).Float64()
	fdiff, _ := big2.NewFloat(0).SetInt(diff.Int).Float64()
	pgen = fdiff * 100 / pgen
	// Difficulty adjustement print. Really helpful for debugging purposes.
	log.Debugf("adjust %.4f%%, p%s lb%d (%.4f%% gen)\n", 100*float64(expLbTime)/float64(actTime), prevdiff, DiffLookback(baseTs.Height()), pgen)

	return diff
}

type TSPoW struct {
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

	netName address.SubnetID
}

// Blocks that are more than MaxHeightDrift epochs above
// the theoretical max height based on systime are quickly rejected
const MaxHeightDrift = 5

func NewTSPoWConsensus(sm *stmgr.StateManager, submgr subnet.SubnetMgr, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	return &TSPoW{
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

func (tsp *TSPoW) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	if err := common.BlockSanityChecks(hierarchical.PoW, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tsp.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
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

	// check work above threshold
	w := work(b.Header)
	thr := big.Zero()
	thr.SetBytes(b.Header.Ticket.VRFProof)
	if thr.GreaterThan(w) {
		return xerrors.Errorf("block below work threshold")
	}

	// check work threshold
	if b.Header.Height < MaxDiffLookback {
		if !thr.Equals(param.GenesisWorkTarget) {
			return xerrors.Errorf("wrong work target")
		}
	} else {
		//
		lbr := b.Header.Height - DiffLookback(baseTs.Height())
		lbts, err := tsp.store.GetTipsetByHeight(ctx, lbr, baseTs, false)
		if err != nil {
			return xerrors.Errorf("failed to get lookback tipset+1: %w", err)
		}

		expDiff := Difficulty(baseTs, lbts)
		if !thr.Equals(expDiff) {
			return xerrors.Errorf("expected adjusted difficulty %s, was %s (act-exp: %s)", expDiff, thr, big.Sub(thr, expDiff))
		}
	}

	msgsChecks := common.CheckMsgs(ctx, tsp.store, tsp.sm, tsp.subMgr, tsp.r, tsp.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := tsp.minerIsValid(h.Miner); err != nil {
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

	return xerrors.Errorf("miner address must be a key")
}

func work(bh *types.BlockHeader) big.Int {
	w := big.NewInt(0)
	w.SetBytes([]byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	})

	bhc := *bh
	bhc.BlockSig = nil

	dmh, err := multihash.Decode(bhc.Cid().Hash())
	if err != nil {
		panic(err) // todo probably definitely not a good idea
	}
	s := big.NewInt(0)
	s.SetBytes(dmh.Digest)
	return big.Div(w, s)
}

func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	w := ts.ParentWeight()
	for _, header := range ts.Blocks() {
		w = big.Add(w, work(header))
	}

	return w, nil
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

func BestWorkBlock(ts *types.TipSet) *types.BlockHeader {
	blks := ts.Blocks()
	sort.Slice(blks, func(i, j int) bool {
		return work(blks[i]).GreaterThan(work(blks[j]))
	})
	return blks[0]
}
