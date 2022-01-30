package tendermint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	httptendermintrpcclient "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tenderminttypes "github.com/tendermint/tendermint/types"
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
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const (
	Sidecar = "http://127.0.0.1:26657"
)

var (
	log = logging.Logger("tendermint-consensus")
	_ consensus.Consensus = &Tendermint{}
)

func NodeAddr() string {
	addr := os.Getenv("TENDERMINT_NODE_ADDR")
	if addr == "" {
		return Sidecar
	}
	return addr
}

func GetTendermintID() (address.Address, error){
	client, err := httptendermintrpcclient.New(Sidecar)
	if err != nil {
		// TODO: Tendermint: don't use panic
		panic("unable to access a tendermint client")
	}
	info, err := client.Status(context.TODO())
	if err != nil {
		// TODO: Tendermint: don't use panic
		panic(err)
	}
	id := string(info.NodeInfo.NodeID)
	addr, err := address.NewFromString(id)
	if err != nil {
		// TODO: Tendermint: don't use panic
		panic(err)
	}
	return addr, nil
}

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

	netName hierarchical.SubnetID

	client *httptendermintrpcclient.HTTP

	offset int64

	events <-chan coretypes.ResultEvent
}

func registrationMessage(name hierarchical.SubnetID) []byte {
	b := []byte(name.String())
	b = append(b, RegistrationMessageType)
	return b
}

func NewConsensus(sm *stmgr.StateManager, submgr subnet.SubnetMgr, beacon beacon.Schedule,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {

	nn := hierarchical.SubnetID(netName)

	tendermintClient, err := httptendermintrpcclient.New(NodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s", err)
	}
	info, err := tendermintClient.Status(context.TODO())
	if err != nil {
		log.Fatalf("unable to connect to the Tendermint node: %s", err)
	}
	log.Info(info)

	resp, err := tendermintClient.BroadcastTxCommit(context.TODO(), registrationMessage(nn))
	if err != nil {
		log.Fatalf("unable to register network: %s", err)
	}

	var offset int64
	buf := bytes.NewBuffer(resp.DeliverTx.Data)
	err = binary.Read(buf, binary.LittleEndian, &offset)
	if err != nil {
		log.Fatalf("unable to convert offset: %s", err.Error())
	}
	log.Warnf("!!!!! Tendermint offset for %s is %d", nn.String(), offset)

	err = tendermintClient.Start()
	if err != nil {
		log.Fatal(err)
	}
	//TODO: stop client on exit

	//TODO: do we need this? Now all requests to Tendermint are made using plain HTTP
	query := "tm.event = 'NewBlock'"
	events, err := tendermintClient.Subscribe(context.TODO(), "test-client", query)
	if err != nil {
		log.Fatalf("unable to subscribe to the Tendermint events: %s",err)
	}

	return &Tendermint{
		store:    sm.ChainStore(),
		beacon:   beacon,
		sm:       sm,
		verifier: verifier,
		genesis:  genesis,
		subMgr:   submgr,
		netName:  nn,
		client: tendermintClient,
		events: events,
		offset: offset,
	}
}

// Weight defines weight. TODO: Tendermint: define wieght for tendermint
func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}

func (tendermint *Tendermint) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("STARTED VALIDATE BLOCK %d", b.Header.Height)
	defer log.Infof("FINISHED VALIDATE BLOCK %d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.Tendermint, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tendermint.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
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

	msgsChecks := common.CheckMsgsWithoutBlockSig(ctx, tendermint.store, tendermint.sm, tendermint.subMgr, tendermint.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := tendermint.minerIsValid(b.Header.Miner); err != nil {
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

	stateRootCheck := common.CheckStateRoot(ctx, tendermint.store, tendermint.sm, b, baseTs)

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
	height := int64(h.Height)+tendermint.offset
	log.Infof("Try to access Tendermint RPC from ValidateBlock")
	tendermintBlock, err := tendermint.client.Block(ctx, &height)
	if err != nil {
		return xerrors.Errorf("unable to get the Tendermint block at height %d", height)
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

func (tendermint *Tendermint) validateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("STARTED ADDITIONAL VALIDATION FOR BLOCK %d", b.Header.Height)
	defer log.Infof("FINISHED ADDITIONAL VALIDATION FOR  %d", b.Header.Height)

	if err := common.BlockSanityChecks(hierarchical.Tendermint, b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tendermint.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
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

	msgsChecks := common.CheckMsgsWithoutBlockSig(ctx, tendermint.store, tendermint.sm, tendermint.subMgr, tendermint.netName, b, baseTs)

	minerCheck := async.Err(func() error {
		if err := tendermint.minerIsValid(b.Header.Miner); err != nil {
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

	stateRootCheck := common.CheckStateRoot(ctx, tendermint.store, tendermint.sm, b, baseTs)

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

	height := int64(h.Height)+tendermint.offset
	tendermintBlock, err := tendermint.client.Block(ctx, &height)
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

func getMessageMapFromTendermintBlock(tb *tenderminttypes.Block) (map[[32]byte]bool, error) {
	msgs := make(map[[32]byte]bool)
	for _, msg := range tb.Txs {
		tx := msg.String()
		// Transactions from Tendermint are in the Tx{} format. So we have to remove T,x, { and } characters.
		// Then we have to remove last two characters that are message type.
		txo := tx[3:len(tx)-3]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			return nil, err
		}
		id := sha256.Sum256(txoData)
		msgs[id] = true
	}
	return msgs, nil
}

// isBlockSealed checks that the following conditions hold:
//     - all messages from the Filecoin block are contained in the Tendermint block.
//     - Tendermint block hash is equal to Filecoin BlockSig field.
func isBlockSealed(fb *types.FullBlock, tb *tenderminttypes.Block) (bool, error) {
	tendermintMessagesHashes, err := getMessageMapFromTendermintBlock(tb)
	if err != nil {
		return false, err
	}

	for _, msg := range fb.BlsMessages {
		bs, err := msg.Serialize()
		if err != nil {
			return false, err
		}
		id := sha256.Sum256(bs)
		_, found := tendermintMessagesHashes[id]
		if !found {
			log.Info("bls messages are not sealed")
			return false, nil
		}
	}

	for _, msg := range fb.SecpkMessages {
		bs, err := msg.Serialize()
		if err != nil {
			return false, err
		}
		id := sha256.Sum256(bs)
		_, found := tendermintMessagesHashes[id]
		if !found {
			log.Info("secpk messages are not sealed")
			return false, nil
		}
	}

	for _, msg := range fb.CrossMessages {
		bs, err := msg.Serialize()
		if err != nil {
			return false, err
		}
		id := sha256.Sum256(bs)
		_, found := tendermintMessagesHashes[id]
		if !found {
			log.Info("cross messages are not sealed")
			return false, nil
		}
	}

	if !bytes.Equal(fb.Header.BlockSig.Data, tb.Hash().Bytes()) {
		log.Infof("block tendermint hash is invalid %x", fb.Header.BlockSig.Data)
		return false, xerrors.New("block tendermint hash is invalid")
	}
	return true, nil
}

func (tendermint *Tendermint) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if tendermint.genesis == nil {
		return false
	}

	tendermintLastBlock, err := tendermint.client.Block(context.TODO(), nil)
	if err != nil {
		//TODO: Tendermint: Discuss what we should return here.
		return false
	}
	//TODO: Tendermint: Discuss what we should return here.
	return tendermintLastBlock.Block.Height+MaxHeightDrift < int64(epoch)
}

func (tendermint *Tendermint) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
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

func (tendermint *Tendermint) minerIsValid(maddr address.Address) error {
	switch maddr.Protocol() {
	case address.BLS:
		fallthrough
	case address.SECP256K1:
		return nil
	}

	return xerrors.Errorf("miner address must be a key")
}

// TODO: is that correct or should be adapted?
// Blocks that are more than MaxHeightDrift epochs above
// the theoretical max height based on systime are quickly rejected
const MaxHeightDrift = 5
