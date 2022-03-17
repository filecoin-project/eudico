package tendermint

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
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
	tendermintRPCAddressEnv     = "EUDICO_TENDERMINT_RPC"
	defaultTendermintRPCAddress = "http://127.0.0.1:26657"

	MaxHeightDrift = 5
)

var (
	log                     = logging.Logger("tendermint-consensus")
	_   consensus.Consensus = &Tendermint{}
)

type Tendermint struct {
	store    *store.ChainStore
	beacon   beacon.Schedule
	sm       *stmgr.StateManager
	verifier ffiwrapper.Verifier
	genesis  *types.TipSet
	subMgr   subnet.SubnetMgr
	netName  address.SubnetID
	r        *resolver.Resolver

	// Used to access Tendermint RPC
	client *tmclient.HTTP
	// Offset in Tendermint blockchain
	offset int64
	// Tendermint validator secp256k1 address
	tendermintValidatorAddress string
	// Eudico client secp256k1 address
	eudicoClientAddress address.Address
	// Secp256k1 public key
	eudicoClientPubKey []byte
	// Mapping between Tendermint validator addresses and Eudico miner addresses
	tendermintEudicoAddresses map[string]address.Address
}

func NewConsensus(
	sm *stmgr.StateManager,
	submgr subnet.SubnetMgr,
	b beacon.Schedule,
	r *resolver.Resolver,
	v ffiwrapper.Verifier,
	g chain.Genesis,
	netName dtypes.NetworkName,
) consensus.Consensus {
	ctx := context.TODO()

	subnetID := address.SubnetID(netName)
	log.Infof("New Tendermint consensus for %s subnet", subnetID)

	c, err := tmclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint RPC client: %s", err)
	}

	log.Infof("Tendermint RPC address: %s", NodeAddr())

	valAddr, valPubKey, clientAddr, err := getValidatorsInfo(ctx, c)
	if err != nil {
		log.Fatalf("unable to get or handle Tendermint validators info: %s", err)
	}
	log.Info("Tendermint validator addr:", valAddr)
	log.Infof("Tendermint validator pub key: %x", valPubKey)
	log.Info("Eudico client addr: ", clientAddr)

	regSubnet, err := registerNetworkViaTxSync(ctx, c, subnetID)
	if err != nil {
		log.Fatalf("unable to registrate network: %s", err)
	}
	log.Info("subnet registered")
	log.Warnf("Tendermint offset for %s is %d", regSubnet.Name, regSubnet.Offset)

	return &Tendermint{
		store:                      sm.ChainStore(),
		beacon:                     b,
		sm:                         sm,
		verifier:                   v,
		genesis:                    g,
		subMgr:                     submgr,
		netName:                    subnetID,
		client:                     c,
		offset:                     regSubnet.Offset,
		tendermintValidatorAddress: valAddr,
		eudicoClientAddress:        clientAddr,
		eudicoClientPubKey:         valPubKey,
		tendermintEudicoAddresses:  make(map[string]address.Address),
	}
}

func (tm *Tendermint) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("starting block validation process at @%d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.Tendermint, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tm.store.LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
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
		return xerrors.Errorf("parent weight different: %s (header) != %s (computed)",
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

	// Perform Tendermint specific checks.
	if err := tm.validateTendermintData(ctx, b); err != nil {
		return err
	}

	log.Infof("block at @%d is valid", b.Header.Height)

	return nil
}

func (tm *Tendermint) validateTendermintData(ctx context.Context, b *types.FullBlock) error {
	height := int64(b.Header.Height) + tm.offset

	blockInfo, err := tm.client.Block(ctx, &height)
	if err != nil {
		return xerrors.Errorf("unable to get the Tendermint block info at height %d: %w", height, err)
	}

	valInfo, err := tm.client.Validators(ctx, &height, nil, nil)
	if err != nil {
		return xerrors.Errorf("unable to get the Tendermint validators info at height %d: %w", height, err)
	}

	var validMinerEudicoAddress address.Address
	var convErr error
	validMinerEudicoAddress, ok := tm.tendermintEudicoAddresses[blockInfo.Block.ProposerAddress.String()]
	if !ok {
		proposerPubKey := findValidatorPubKeyByAddress(valInfo.Validators, blockInfo.Block.ProposerAddress.Bytes())
		if proposerPubKey == nil {
			return xerrors.Errorf("unable to find pubKey for proposer %w", blockInfo.Block.ProposerAddress)
		}

		validMinerEudicoAddress, convErr = getFilecoinAddrFromTendermintPubKey(proposerPubKey)
		if convErr != nil {
			return xerrors.Errorf("unable to get proposer addr %w", err)
		}
		tm.tendermintEudicoAddresses[blockInfo.Block.ProposerAddress.String()] = validMinerEudicoAddress
	}
	if b.Header.Miner != validMinerEudicoAddress {
		return xerrors.Errorf("invalid miner address %w in the block header", b.Header.Miner)
	}

	if err := isBlockSealed(b, blockInfo.Block); err != nil {
		return xerrors.Errorf("block is not sealed: %w", err)
	}
	return nil
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

// IsEpochBeyondCurrMax is used in Filcns to detect delayed blocks.
// We are currently using defaults here and not worrying about it.
// We will consider potential changes of Consensus interface in https://github.com/filecoin-project/eudico/issues/143.
func (tm *Tendermint) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if tm.genesis == nil {
		return false
	}

	tendermintLastBlock, err := tm.client.Block(context.TODO(), nil)
	if err != nil {
		return false
	}
	return tendermintLastBlock.Block.Height+MaxHeightDrift < int64(epoch)
}

func (tm *Tendermint) Type() hierarchical.ConsensusType {
	return hierarchical.Tendermint
}

// Weight defines weight.
// We are just using a default weight for all subnet consensus algorithms.
func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}
