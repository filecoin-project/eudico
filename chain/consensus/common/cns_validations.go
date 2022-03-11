package common

import (
	"context"
	"fmt"

	"github.com/Gurpartap/async"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"
)

var log = logging.Logger("consensus-common")

func CheckStateRoot(ctx context.Context, store *store.ChainStore, sm *stmgr.StateManager, b *types.FullBlock, baseTs *types.TipSet) async.ErrorFuture {
	h := b.Header
	return async.Err(func() error {
		stateroot, precp, err := sm.TipSetState(ctx, baseTs)
		if err != nil {
			return xerrors.Errorf("get tipsetstate(%d, %s) failed: %w", h.Height, h.Parents, err)
		}

		if stateroot != h.ParentStateRoot {
			msgs, err := store.MessagesForTipset(ctx, baseTs)
			if err != nil {
				log.Error("failed to load messages for tipset during tipset state mismatch error: ", err)
			} else {
				log.Warn("Messages for tipset with mismatching state:")
				for i, m := range msgs {
					mm := m.VMMessage()
					log.Warnf("Message[%d]: from=%s to=%s method=%d params=%x", i, mm.From, mm.To, mm.Method, mm.Params)
				}
			}

			return xerrors.Errorf("parent state root did not match computed state (%s != %s)", stateroot, h.ParentStateRoot)
		}

		if precp != h.ParentMessageReceipts {
			return xerrors.Errorf("parent receipts root did not match computed value (%s != %s)", precp, h.ParentMessageReceipts)
		}

		return nil
	})
}

func CheckMsgs(ctx context.Context, store *store.ChainStore, sm *stmgr.StateManager, submgr subnet.SubnetMgr, r *resolver.Resolver, netName address.SubnetID, b *types.FullBlock, baseTs *types.TipSet) []async.ErrorFuture {
	h := b.Header
	msgsCheck := async.Err(func() error {
		if b.Cid() == build.WhitelistedBlock {
			return nil
		}

		if err := checkBlockMessages(ctx, store, sm, submgr, r, netName, b, baseTs); err != nil {
			return xerrors.Errorf("block had invalid messages: %w", err)
		}
		return nil
	})

	baseFeeCheck := async.Err(func() error {
		baseFee, err := store.ComputeBaseFee(ctx, baseTs)
		if err != nil {
			return xerrors.Errorf("computing base fee: %w", err)
		}
		if types.BigCmp(baseFee, b.Header.ParentBaseFee) != 0 {
			return xerrors.Errorf("base fee doesn't match: %s (header) != %s (computed)",
				b.Header.ParentBaseFee, baseFee)
		}
		return nil
	})
	blockSigCheck := async.Err(func() error {
		if err := sigs.CheckBlockSignature(ctx, h, b.Header.Miner); err != nil {
			return xerrors.Errorf("check block signature failed: %w", err)
		}
		return nil
	})

	return []async.ErrorFuture{msgsCheck, baseFeeCheck, blockSigCheck}

}

func BlockSanityChecks(ctype hierarchical.ConsensusType, h *types.BlockHeader) error {
	// Delegated consensus has no election proof.
	switch ctype {
	case hierarchical.Delegated:
		if h.ElectionProof != nil {
			return xerrors.Errorf("block must have nil election proof")
		}
		if h.Ticket != nil {
			return xerrors.Errorf("block must have nil ticket")
		}
	default:
		// FIXME: We currently support PoW and delegated, thus the
		// default instead of specifying other consensus. This needs
		// to change.
		if h.Ticket == nil {
			return xerrors.Errorf("block must not have nil ticket")
		}
	}

	if h.BlockSig == nil {
		return xerrors.Errorf("block had nil signature")
	}

	if h.BLSAggregate == nil {
		return xerrors.Errorf("block had nil bls aggregate signature")
	}

	if h.Miner.Protocol() != address.SECP256K1 {
		return xerrors.Errorf("block had non-secp miner address")
	}

	if len(h.Parents) != 1 {
		return xerrors.Errorf("must have 1 parent")
	}

	return nil
}

func checkBlockMessages(ctx context.Context, str *store.ChainStore, sm *stmgr.StateManager, submgr subnet.SubnetMgr, r *resolver.Resolver, netName address.SubnetID, b *types.FullBlock, baseTs *types.TipSet) error {
	{
		var sigCids []cid.Cid // this is what we get for people not wanting the marshalcbor method on the cid type
		var pubks [][]byte

		for _, m := range b.BlsMessages {
			sigCids = append(sigCids, m.Cid())

			pubk, err := sm.GetBlsPublicKey(ctx, m.From, baseTs)
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

	stateroot, _, err := sm.TipSetState(ctx, baseTs)
	if err != nil {
		return err
	}

	st, err := state.LoadStateTree(str.ActorStore(ctx), stateroot)
	if err != nil {
		return xerrors.Errorf("failed to load base state tree: %w", err)
	}

	nv := sm.GetNetworkVersion(ctx, b.Header.Height)
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
		if sm.GetNetworkVersion(ctx, b.Header.Height) >= network.Version13 {
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

		c, err := store.PutMessage(ctx, tmpbs, m)
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
		kaddr, err := sm.ResolveToKeyAddress(ctx, m.Message.From, baseTs)
		if err != nil {
			return xerrors.Errorf("failed to resolve key addr: %w", err)
		}

		if err := sigs.Verify(&m.Signature, kaddr, m.Message.Cid().Bytes()); err != nil {
			return xerrors.Errorf("secpk message %s has invalid signature: %w", m.Cid(), err)
		}

		c, err := store.PutMessage(ctx, tmpbs, m)
		if err != nil {
			return xerrors.Errorf("failed to store message %s: %w", m.Cid(), err)
		}
		k := cbg.CborCid(c)
		if err := smArr.Set(uint64(i), &k); err != nil {
			return xerrors.Errorf("failed to put secpk message at index %d: %w", i, err)
		}
	}

	crossArr := blockadt.MakeEmptyArray(tmpstore)
	// Preamble to get states required for cross-msg checks.
	var (
		parentSCA *sca.SCAState
		snSCA     *sca.SCAState
		pstore    blockadt.Store
		snstore   blockadt.Store
	)
	// If subnet manager is not set we are in the root chain and we don't need to get parentSCA
	// state
	if submgr != nil {
		parentSCA, pstore, err = getSCAState(ctx, sm, submgr, netName.Parent(), baseTs)
		if err != nil {
			return err
		}
	}
	// Get SCA state in subnet.
	snSCA, snstore, err = getSCAState(ctx, sm, submgr, netName, baseTs)
	if err != nil {
		return err
	}
	// Check cross messages
	for i, m := range b.CrossMessages {
		if err := checkCrossMsg(ctx, r, pstore, snstore, parentSCA, snSCA, m); err != nil {
			return xerrors.Errorf("failed to check message %s: %w", m.Cid(), err)
		}

		// FIXME: Should we try to apply the message before accepting the block?
		// Check if the message can be applied before accepting it for proposal.
		// if err := canApplyMsg(ctx, submgr, sm, netName, m); err != nil {
		//         return xerrors.Errorf("failed testing the application of cross-msg %s: %w", m.Cid(), err)
		// }
		// // NOTE: We don't check mesage against VM for cross shard messages. They are
		// // checked in some other way.
		// if err := checkMsg(m); err != nil {
		//         return xerrors.Errorf("block had invalid bls message at index %d: %w", i, err)
		// }

		c, err := store.PutMessage(ctx, tmpbs, m)
		if err != nil {
			return xerrors.Errorf("failed to store message %s: %w", m.Cid(), err)
		}

		k := cbg.CborCid(c)
		if err := crossArr.Set(uint64(i), &k); err != nil {
			return xerrors.Errorf("failed to put cross message at index %d: %w", i, err)
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

	crossroot, err := crossArr.Root()
	if err != nil {
		return err
	}

	mrcid, err := tmpstore.Put(ctx, &types.MsgMeta{
		BlsMessages:   bmroot,
		SecpkMessages: smroot,
		CrossMessages: crossroot,
	})
	if err != nil {
		return err
	}

	if b.Header.Messages != mrcid {
		return fmt.Errorf("messages didnt match message root in header")
	}

	// Finally, flush.
	return vm.Copy(ctx, tmpbs, str.ChainBlockstore(), mrcid)
}

func ValidateLocalBlock(ctx context.Context, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	stats.Record(ctx, metrics.BlockPublished.M(1))

	if size := msg.Size(); size > 1<<20-1<<15 {
		log.Errorf("ignoring oversize block (%dB)", size)
		return pubsub.ValidationIgnore, "oversize_block"
	}

	blk, what, err := DecodeAndCheckBlock(msg)
	if err != nil {
		log.Errorf("got invalid local block: %s", err)
		return pubsub.ValidationIgnore, what
	}

	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func DecodeAndCheckBlock(msg *pubsub.Message) (*types.BlockMsg, string, error) {
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

func ValidateMsgMeta(ctx context.Context, msg *types.BlockMsg) error {
	// TODO there has to be a simpler way to do this without the blockstore dance
	// block headers use adt0
	store := blockadt.WrapStore(ctx, cbor.NewCborStore(bstore.NewMemory()))
	bmArr := blockadt.MakeEmptyArray(store)
	smArr := blockadt.MakeEmptyArray(store)
	crossArr := blockadt.MakeEmptyArray(store)

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

	for i, m := range msg.CrossMessages {
		c := cbg.CborCid(m)
		if err := crossArr.Set(uint64(i), &c); err != nil {
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

	crossroot, err := crossArr.Root()
	if err != nil {
		return err
	}

	mrcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   bmroot,
		SecpkMessages: smroot,
		CrossMessages: crossroot,
	})

	if err != nil {
		return err
	}

	if msg.Header.Messages != mrcid {
		return fmt.Errorf("messages didn't match root cid in header")
	}

	return nil
}
