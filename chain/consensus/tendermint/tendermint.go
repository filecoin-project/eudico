package tendermint

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	tmsecp "github.com/tendermint/tendermint/crypto/secp256k1"
	tmclient "github.com/tendermint/tendermint/rpc/client/http"
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
	Sidecar = "http://127.0.0.1:26657"

	// TODO: is that correct or should be adapted?
	// Blocks that are more than MaxHeightDrift epochs above
	// the theoretical max height based on systime are quickly rejected
	MaxHeightDrift = 5
)

var (
	log                     = logging.Logger("tendermint-consensus")
	_   consensus.Consensus = &Tendermint{}
)

type Tendermint struct {
	// The interface for accessing and putting tipsets into local storage
	store *store.ChainStore

	// handle to the random beacon for verification
	beacon beacon.Schedule

	// the state manager handles making state queries
	sm *stmgr.StateManager

	verifier ffiwrapper.Verifier

	genesis *types.TipSet

	subMgr subnet.SubnetMgr

	netName address.SubnetID

	r *resolver.Resolver

	client *tmclient.HTTP

	offset int64

	tag []byte

	// Tendermint validator secp256k1 address
	validatorAddress []byte

	// Eudico client secp256k1 address
	clientAddress address.Address

	// Secp256k1 public key
	clientPubKey []byte
}

func NewConsensus(sm *stmgr.StateManager, submgr subnet.SubnetMgr, beacon beacon.Schedule, r *resolver.Resolver,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {

	subnetID := address.SubnetID(netName)
	log.Infof("New Tendermint consensus for %s subnet", subnetID )

	tag := sha256.Sum256([]byte(subnetID))

	tmClient, err := tmclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s", err)
	}
	resp, err := tmClient.Status(context.TODO())
	if err != nil {
		log.Fatalf("unable to connect to the Tendermint node: %s", err)
	}
	log.Info("Tendermint validator:", resp.ValidatorInfo.Address)

	validatorAddress := resp.ValidatorInfo.Address.Bytes()

	keyType := resp.ValidatorInfo.PubKey.Type()
	if keyType != tmsecp.KeyType {
		log.Fatalf("Tendermint validator uses unsupported key type: %s", keyType)
	}
	validatorPubKey := resp.ValidatorInfo.PubKey.Bytes()

	clientAddress, err := address.NewSecp256k1Address(validatorPubKey)
	if err != nil {
		log.Fatalf("unable to calculate client address: %s", err.Error())
	}

	regMsg, err := NewRegistrationMessageBytes(subnetID, tag[:4])
	if err != nil {
		log.Fatalf("unable to create a registration message: %s", err)
	}
	regResp, err := tmClient.BroadcastTxCommit(context.TODO(), regMsg)
	if err != nil {
		log.Fatalf("unable to register network: %s", err)
	}
	log.Info("subnet registered")

	regSubnet, err := DecodeRegistrationMessage(regResp.DeliverTx.Data)
	if err != nil {
		log.Fatalf("unable to decode registration response: %s", err.Error())
	}

	log.Warnf("!!!!! Tendermint offset for %s is %d", regSubnet.Name, regSubnet.Offset)

	return &Tendermint{
		store:    sm.ChainStore(),
		beacon:   beacon,
		sm:       sm,
		verifier: verifier,
		genesis:  genesis,
		subMgr:   submgr,
		netName:  subnetID,
		client:   tmClient,
		offset:   regSubnet.Offset,
		tag: tag[:4],
		validatorAddress: validatorAddress,
		clientAddress: clientAddress,
		clientPubKey: validatorPubKey,
	}
}

func (tm *Tendermint) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("STARTED VALIDATE BLOCK %d", b.Header.Height)
	defer log.Infof("FINISHED VALIDATE BLOCK %d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.Tendermint, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tm.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
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

	msgsChecks := common.CheckMsgsWithoutBlockSig(ctx, tm.store, tm.sm, tm.subMgr, tm.r, tm.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := tm.minerIsValid(b.Header.Miner); err != nil {
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

	stateRootCheck := common.CheckStateRoot(ctx, tm.store, tm.sm, b, baseTs)

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

	// Tendermint specific checks.
	height := int64(h.Height) + tm.offset
	log.Infof("Try to access Tendermint RPC from ValidateBlock")
	resp, err := tm.client.Block(ctx, &height)
	if err != nil {
		return xerrors.Errorf("unable to get the Tendermint block at height %d", height)
	}
	val, err := tm.client.Validators(ctx, &height, nil, nil)
	if err != nil {
		return xerrors.Errorf("unable to get the Tendermint block validators at height %d", height)
	}

	proposerPubKey := findValidatorPubKeyByAddress(val.Validators, resp.Block.ProposerAddress.Bytes())
	if proposerPubKey == nil {
		return xerrors.Errorf("unable to find pubKey for proposer %w", resp.Block.ProposerAddress)
	}

	addr, err := address.NewSecp256k1Address(proposerPubKey)
	if err != nil {
		return xerrors.Errorf("unable to get proposer addr %w", err)
	}

	if b.Header.Miner != addr {
		return xerrors.Errorf("invalid miner address %w in the block header", b.Header.Miner)
	}

	sealed, err := isBlockSealed(b, resp.Block)
	if err != nil {
		log.Infof("block sealed err: %s", err.Error())
		return err
	}
	if !sealed {
		log.Infof("block is not sealed %d", b.Header.Height)
		return xerrors.New("block is not sealed")
	}

	return nil
}

func (tm *Tendermint) validateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("STARTED ADDITIONAL VALIDATION FOR BLOCK %d", b.Header.Height)
	defer log.Infof("FINISHED ADDITIONAL VALIDATION FOR  %d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.Tendermint, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tm.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
	if err != nil {
		return xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	// fast checks first
	if h.Height != baseTs.Height() {
		return xerrors.Errorf("block height not parent height+1: %d != %d", h.Height, baseTs.Height()+1)
	}

	now := uint64(build.Clock.Now().Unix())
	if h.Timestamp > now+build.AllowableClockDriftSecs {
		return xerrors.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	}
	if h.Timestamp > now {
		log.Warn("Got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	}

	msgsChecks := common.CheckMsgsWithoutBlockSig(ctx, tm.store, tm.sm, tm.subMgr, tm.r, tm.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := tm.minerIsValid(b.Header.Miner); err != nil {
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

	stateRootCheck := common.CheckStateRoot(ctx, tm.store, tm.sm, b, baseTs)

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

	height := int64(h.Height) + tm.offset
	tendermintBlock, err := tm.client.Block(ctx, &height)
	if err != nil {
		return xerrors.Errorf("unable to get the Tendermint block by height %d", height)
	}

	sealed, err := isBlockSealed(b, tendermintBlock.Block)
	if err != nil {
		log.Infof("block sealed err: %s", err.Error())
		return err
	}
	if !sealed {
		log.Infof("block is not sealed %d", b.Header.Height)
		return xerrors.New("block is not sealed")
	}
	return nil
}

func (tm *Tendermint) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if tm.genesis == nil {
		return false
	}

	tendermintLastBlock, err := tm.client.Block(context.TODO(), nil)
	if err != nil {
		//TODO: Tendermint: Discuss what we should return here.
		return false
	}
	//TODO: Tendermint: Discuss what we should return here.
	return tendermintLastBlock.Block.Height+MaxHeightDrift < int64(epoch)
}

func (tm *Tendermint) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	if self {
		return validateLocalBlock(ctx, msg)
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

	blk, what, err := decodeAndCheckBlock(msg)
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

func (tm *Tendermint) minerIsValid(maddr address.Address) error {
	switch maddr.Protocol() {
	case address.BLS:
		fallthrough
	case address.SECP256K1:
		return nil
	}

	return xerrors.Errorf("miner address must be a key")
}

// Weight defines weight.
// TODO: should we adopt weight for tendermint?
func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}
