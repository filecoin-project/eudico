package tendermint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	httptendermintrpcclient "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tenderminttypes "github.com/tendermint/tendermint/types"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
)

const (
	TendermintSidecar = "http://127.0.0.1:26657"
)

func TendermintNodeAddr() string {
	addr := os.Getenv("TENDERMINT_NODE_ADDR")
	if addr == "" {
		return TendermintSidecar
	}
	return addr
}

var (
	log = logging.Logger("tendermint-consensus")
	_ consensus.Consensus = &Tendermint{}
)

func GetTendermintID() (address.Address, error){
	client, err := httptendermintrpcclient.New(TendermintSidecar)
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

	events <-chan coretypes.ResultEvent
}

func NewConsensus(sm *stmgr.StateManager, submgr subnet.SubnetMgr, beacon beacon.Schedule,
	verifier ffiwrapper.Verifier, genesis chain.Genesis, netName dtypes.NetworkName) consensus.Consensus {
	tendermintClient, err := httptendermintrpcclient.New(TendermintNodeAddr())
	if err != nil {
		log.Fatalf("unable to create a Tendermint client: %s",err)
	}
	info, err := tendermintClient.Status(context.TODO())
	if err != nil {
		log.Fatalf("unable to connect to the Tendermint node: %s",err)
	}
	log.Info(info)

	err = tendermintClient.Start()
	if err != nil {
		log.Fatal(err)
	}
	//TODO: stop client on exit

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
		netName:  hierarchical.SubnetID(netName),
		client: tendermintClient,
		events: events,
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

	msgsChecks := common.CheckMsgs(ctx, tendermint.store, tendermint.sm, tendermint.subMgr, tendermint.netName, b, baseTs)

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


	height := int64(h.Height)+1
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

	msgsChecks := common.CheckMsgs(ctx, tendermint.store, tendermint.sm, tendermint.subMgr, tendermint.netName, b, baseTs)

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


	height := int64(h.Height)+1
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

func getTendermintTransactionHash(block *tenderminttypes.Block) ([32]byte, error) {
	lenTx := len(block.Txs)
	if lenTx !=1 {
		return [32]byte{}, xerrors.Errorf("Tx len is not 1 but %d: %s, %s", block.Txs[0].String(), block.Txs[1].String() )

	}
	tx := block.Txs[0].String()
	txo := tx[3:len(tx)-1]
	txoData, err := hex.DecodeString(txo)
	if err != nil {
		panic(err)
	}
	receivedTxHash := sha256.Sum256(txoData)
	return receivedTxHash, nil

}

func getMessageMapFromTendermintBlock(tb *tenderminttypes.Block) (map[[32]byte]bool, error) {
	msgs := make(map[[32]byte]bool)
	for _, msg := range tb.Txs {
		tx := msg.String()
		txo := tx[3:len(tx)-1]
		txoData, err := hex.DecodeString(txo)
		if err != nil {
			return nil, err
		}
		id := sha256.Sum256(txoData)
		msgs[id] = true
	}
	return msgs, nil
}

// isBlockSealed checks that all messages from Filecoin block are contained in the Tendermint block.
// Checking messages from the blocks are same doesn't work because some messages might be filtered.
func isBlockSealed(fb *types.FullBlock, tb *tenderminttypes.Block) (bool, error) {
	fbMsgs := fb.BlsMessages
	tbMsgs, err := getMessageMapFromTendermintBlock(tb)
	if err != nil {
		return false, err
	}
	for _, msg := range fbMsgs {
		bs, err := msg.Serialize()
		if err != nil {
			return false, err
		}
		id := sha256.Sum256(bs)
		_, found := tbMsgs[id]
		if !found {
			return false, nil
		}
	}
	return true, nil
}

// TODO: We should extract this somewhere else and make the message pool and miner use the same logic
func (tendermint *Tendermint) checkBlockMessages(ctx context.Context, b *types.FullBlock, baseTs *types.TipSet) error {
	{
		var sigCids []cid.Cid // this is what we get for people not wanting the marshalcbor method on the cid type
		var pubks [][]byte

		for _, m := range b.BlsMessages {
			sigCids = append(sigCids, m.Cid())

			pubk, err := tendermint.sm.GetBlsPublicKey(ctx, m.From, baseTs)
			if err != nil {
				return xerrors.Errorf("failed to load bls public to validate block: %w", err)
			}

			pubks = append(pubks, pubk)
		}

		if err := consensus.VerifyBlsAggregate(ctx, b.Header.BLSAggregate, sigCids, pubks); err != nil {
			return xerrors.Errorf("bls aggregate signature was invalid: %w", err)
		}
	}

	nonces := make(map[address.Address]uint64)

	stateroot, _, err := tendermint.sm.TipSetState(ctx, baseTs)
	if err != nil {
		return err
	}

	st, err := state.LoadStateTree(tendermint.store.ActorStore(ctx), stateroot)
	if err != nil {
		return xerrors.Errorf("failed to load base state tree: %w", err)
	}

	nv := tendermint.sm.GetNtwkVersion(ctx, b.Header.Height)
	pl := vm.PricelistByEpoch(baseTs.Height())
	var sumGasLimit int64
	checkMsg := func(msg types.ChainMsg) error {
		m := msg.VMMessage()

		// Phase 1: syntactic validation, as defined in the spec
		minGas := pl.OnChainMessage(msg.ChainLength())
		if err := m.ValidForBlockInclusion(minGas.Total(), nv); err != nil {
			return err
		}

		// ValidForBlockInclusion checks if any single message does not exceed BlockGasLimit
		// So below is overflow safe
		sumGasLimit += m.GasLimit
		if sumGasLimit > build.BlockGasLimit {
			return xerrors.Errorf("block gas limit exceeded")
		}

		// Phase 2: (Partial) semantic validation:
		// the sender exists and is an account actor, and the nonces make sense
		var sender address.Address
		if tendermint.sm.GetNtwkVersion(ctx, b.Header.Height) >= network.Version13 {
			sender, err = st.LookupID(m.From)
			if err != nil {
				return err
			}
		} else {
			sender = m.From
		}

		if _, ok := nonces[sender]; !ok {
			// `GetActor` does not validate that this is an account actor.
			act, err := st.GetActor(sender)
			if err != nil {
				return xerrors.Errorf("failed to get actor: %w", err)
			}

			if !builtin.IsAccountActor(act.Code) {
				return xerrors.New("Sender must be an account actor")
			}
			nonces[sender] = act.Nonce
		}

		if nonces[sender] != m.Nonce {
			return xerrors.Errorf("wrong nonce (exp: %d, got: %d)", nonces[sender], m.Nonce)
		}
		nonces[sender]++

		return nil
	}

	// Validate message arrays in a temporary blockstore.
	tmpbs := bstore.NewMemory()
	tmpstore := blockadt.WrapStore(ctx, cbor.NewCborStore(tmpbs))

	bmArr := blockadt.MakeEmptyArray(tmpstore)
	for i, m := range b.BlsMessages {
		if err := checkMsg(m); err != nil {
			return xerrors.Errorf("block had invalid bls message at index %d: %w", i, err)
		}

		c, err := store.PutMessage(tmpbs, m)
		if err != nil {
			return xerrors.Errorf("failed to store message %s: %w", m.Cid(), err)
		}

		k := cbg.CborCid(c)
		if err := bmArr.Set(uint64(i), &k); err != nil {
			return xerrors.Errorf("failed to put bls message at index %d: %w", i, err)
		}
	}

	smArr := blockadt.MakeEmptyArray(tmpstore)
	for i, m := range b.SecpkMessages {
		if err := checkMsg(m); err != nil {
			return xerrors.Errorf("block had invalid secpk message at index %d: %w", i, err)
		}

		// `From` being an account actor is only validated inside the `vm.ResolveToKeyAddr` call
		// in `StateManager.ResolveToKeyAddress` here (and not in `checkMsg`).
		kaddr, err := tendermint.sm.ResolveToKeyAddress(ctx, m.Message.From, baseTs)
		if err != nil {
			return xerrors.Errorf("failed to resolve key addr: %w", err)
		}

		if err := sigs.Verify(&m.Signature, kaddr, m.Message.Cid().Bytes()); err != nil {
			return xerrors.Errorf("secpk message %s has invalid signature: %w", m.Cid(), err)
		}

		c, err := store.PutMessage(tmpbs, m)
		if err != nil {
			return xerrors.Errorf("failed to store message %s: %w", m.Cid(), err)
		}
		k := cbg.CborCid(c)
		if err := smArr.Set(uint64(i), &k); err != nil {
			return xerrors.Errorf("failed to put secpk message at index %d: %w", i, err)
		}
	}

	bmroot, err := bmArr.Root()
	if err != nil {
		return err
	}

	smroot, err := smArr.Root()
	if err != nil {
		return err
	}

	mrcid, err := tmpstore.Put(ctx, &types.MsgMeta{
		BlsMessages:   bmroot,
		SecpkMessages: smroot,
	})
	if err != nil {
		return err
	}

	if b.Header.Messages != mrcid {
		return fmt.Errorf("messages didnt match message root in header")
	}

	// Finally, flush.
	return vm.Copy(ctx, tmpbs, tendermint.store.ChainBlockstore(), mrcid)
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
		return tendermint.validateLocalBlock(ctx, msg)
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

	blk, what, err := tendermint.decodeAndCheckBlock(msg)
	if err != nil {
		log.Error("got invalid block over pubsub: ", err)
		recordFailureFlagPeer(what)
		return pubsub.ValidationReject, what
	}

	// validate the block meta: the Message CID in the header must match the included messages
	err = tendermint.validateMsgMeta(ctx, blk)
	if err != nil {
		log.Warnf("error validating message metadata: %s", err)
		recordFailureFlagPeer("invalid_block_meta")
		return pubsub.ValidationReject, "invalid_block_meta"
	}

	reject, err := tendermint.validateBlockHeader(ctx, blk.Header)
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

func (tendermint *Tendermint) validateLocalBlock(ctx context.Context, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	stats.Record(ctx, metrics.BlockPublished.M(1))

	if size := msg.Size(); size > 1<<20-1<<15 {
		log.Errorf("ignoring oversize block (%dB)", size)
		return pubsub.ValidationIgnore, "oversize_block"
	}

	blk, what, err := tendermint.decodeAndCheckBlock(msg)
	if err != nil {
		log.Errorf("got invalid local block: %s", err)
		return pubsub.ValidationIgnore, what
	}

	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func (tendermint *Tendermint) decodeAndCheckBlock(msg *pubsub.Message) (*types.BlockMsg, string, error) {
	blk, err := types.DecodeBlockMsg(msg.GetData())
	if err != nil {
		return nil, "invalid", xerrors.Errorf("error decoding block: %w", err)
	}

	if count := len(blk.BlsMessages) + len(blk.SecpkMessages); count > build.BlockMessageLimit {
		return nil, "too_many_messages", fmt.Errorf("block contains too many messages (%d)", count)
	}

	// make sure we have a signature
	if blk.Header.BlockSig == nil {
		return nil, "missing_signature", fmt.Errorf("block without a signature")
	}

	return blk, "", nil
}

func (tendermint *Tendermint) validateMsgMeta(ctx context.Context, msg *types.BlockMsg) error {
	// TODO there has to be a simpler way to do this without the blockstore dance
	// block headers use adt0
	store := blockadt.WrapStore(ctx, cbor.NewCborStore(bstore.NewMemory()))
	bmArr := blockadt.MakeEmptyArray(store)
	smArr := blockadt.MakeEmptyArray(store)

	for i, m := range msg.BlsMessages {
		c := cbg.CborCid(m)
		if err := bmArr.Set(uint64(i), &c); err != nil {
			return err
		}
	}

	for i, m := range msg.SecpkMessages {
		c := cbg.CborCid(m)
		if err := smArr.Set(uint64(i), &c); err != nil {
			return err
		}
	}

	bmroot, err := bmArr.Root()
	if err != nil {
		return err
	}

	smroot, err := smArr.Root()
	if err != nil {
		return err
	}

	mrcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   bmroot,
		SecpkMessages: smroot,
	})

	if err != nil {
		return err
	}

	if msg.Header.Messages != mrcid {
		return fmt.Errorf("messages didn't match root cid in header")
	}

	return nil
}

func (tendermint *Tendermint) validateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	if err := tendermint.minerIsValid(b.Miner); err != nil {
		return err.Error(), err
	}

	err = sigs.CheckBlockSignature(ctx, b, b.Miner)
	if err != nil {
		log.Errorf("block signature verification failed: %s", err)
		return "signature_verification_failed", err
	}

	return "", nil
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

// Blocks that are more than MaxHeightDrift epochs above
// the theoretical max height based on systime are quickly rejected
// TODO: Tendermint: is that correct?
const MaxHeightDrift = 5
