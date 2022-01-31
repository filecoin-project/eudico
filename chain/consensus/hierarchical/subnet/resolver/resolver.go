package resolver

//go:generate go run ./gen/gen.go

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	lru "github.com/hashicorp/golang-lru"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"
	logging "github.com/ipfs/go-log/v2"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
	xerrors "golang.org/x/xerrors"
)

const retryTimeout = 10 * time.Second

var log = logging.Logger("subnet-resolver")

func SubnetResolverTopic(id address.SubnetID) string {
	return "/fil/subnet/resolver" + id.String()
}

func resolverNamespace(id address.SubnetID) datastore.Key {
	return datastore.NewKey("/resolver/" + id.String())
}

type Resolver struct {
	netName address.SubnetID
	self    peer.ID
	ds      datastore.Datastore
	pubsub  *pubsub.PubSub

	// Caches to track duplicate and frequent msgs
	pushCache *msgReceiptCache
	pullCache *msgReceiptCache
	// NOTE: We don't track number of response
	// messages sent for now. We accept any number.
	// We will need to address this to prevent potential
	// spamming.
	// responseCache *msgReceiptCache

	lk          sync.Mutex
	ongoingPull map[cid.Cid]time.Time
}

type MsgType uint64

const (
	// Push content to other subnet
	Push MsgType = iota
	// PullMeta requests CrossMsgs behind a CID
	PullMeta
	// Response is used to answer to pull requests.
	Response

	// NOTE: For now we don't expect subnets needing to
	// pull checkpoints from other subnets (although this
	// has been discussed for verification purposes)
	// PullCheck requests Checkpoint form a CID
	// PullCheck
)

type ResolveMsg struct {
	// From subnet
	From address.SubnetID
	// Message type being propagated
	Type MsgType
	// Cid of the content
	Cid cid.Cid
	// MsgMeta being propagated (if any)
	CrossMsgs sca.CrossMsgs
	// Checkpoint being propagated (if any)
	// Checkpoint schema.Checkpoint
}

type msgReceiptCache struct {
	msgs *lru.TwoQueueCache
}

func newMsgReceiptCache() *msgReceiptCache {
	c, _ := lru.New2Q(8192)

	return &msgReceiptCache{
		msgs: c,
	}
}

func (mrc *msgReceiptCache) add(bcid string) int {
	val, ok := mrc.msgs.Get(bcid)
	if !ok {
		mrc.msgs.Add(bcid, int(1))
		return 0
	}

	mrc.msgs.Add(bcid, val.(int)+1)
	return val.(int)
}

func (r *Resolver) addMsgReceipt(t MsgType, bcid cid.Cid, from peer.ID) int {
	if t == Push {
		// All push messages are considered equal independent of
		// the source.
		return r.pushCache.add(bcid.String())
	}
	// We allow each peer.ID in a subnet to send a pull request
	// for each CID without being rejected.
	// FIXME: Additional checks may be required to prevent malicious
	// peers from spamming the topic with infinite requests.
	// Deferring the design of a smarter logic here.
	return r.pullCache.add(bcid.String() + from.String())
}

func NewRootResolver(self peer.ID, ds dtypes.MetadataDS, pubsub *pubsub.PubSub, nn dtypes.NetworkName) *Resolver {
	return NewResolver(self, ds, pubsub, address.SubnetID(nn))
}
func NewResolver(self peer.ID, ds dtypes.MetadataDS, pubsub *pubsub.PubSub, netName address.SubnetID) *Resolver {
	return &Resolver{
		netName:     netName,
		self:        self,
		ds:          nsds.Wrap(ds, resolverNamespace(netName)),
		pubsub:      pubsub,
		pushCache:   newMsgReceiptCache(),
		pullCache:   newMsgReceiptCache(),
		ongoingPull: make(map[cid.Cid]time.Time),
	}
}

func HandleMsgs(mctx helpers.MetricsCtx, lc fx.Lifecycle, r *Resolver, submgr subnet.SubnetMgr) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	if err := r.HandleMsgs(ctx, submgr); err != nil {
		panic(err)
	}
}

func (r *Resolver) HandleMsgs(ctx context.Context, submgr subnet.SubnetMgr) error {
	// Register new message validator for resolver msgs.
	v := NewValidator(submgr, r)
	if err := r.pubsub.RegisterTopicValidator(SubnetResolverTopic(r.netName), v.Validate); err != nil {
		return err
	}

	log.Infof("subscribing to subnet content resolver topic %s", SubnetResolverTopic(r.netName))

	// Subscribe to subnet resolver topic.
	msgSub, err := r.pubsub.Subscribe(SubnetResolverTopic(r.netName)) //nolint
	if err != nil {
		return err
	}

	// Start handle incoming resolver msg.
	go r.HandleIncomingResolveMsg(ctx, msgSub)
	return nil
}

func (r *Resolver) shouldPull(c cid.Cid) bool {
	r.lk.Lock()
	defer r.lk.Unlock()
	if time.Since(r.ongoingPull[c]) > retryTimeout {
		r.ongoingPull[c] = time.Now()
		return true
	}
	return false
}

func (r *Resolver) pullSuccess(c cid.Cid) {
	r.lk.Lock()
	defer r.lk.Unlock()
	delete(r.ongoingPull, c)
}

func DecodeResolveMsg(b []byte) (*ResolveMsg, error) {
	var bm ResolveMsg
	if err := bm.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, err
	}

	return &bm, nil
}

func EncodeResolveMsg(m *ResolveMsg) ([]byte, error) {
	w := new(bytes.Buffer)
	if err := m.MarshalCBOR(w); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

type Validator struct {
	r      *Resolver
	submgr subnet.SubnetMgr
}

func NewValidator(submgr subnet.SubnetMgr, r *Resolver) *Validator {
	return &Validator{r, submgr}
}

func (v *Validator) Validate(ctx context.Context, pid peer.ID, msg *pubsub.Message) (res pubsub.ValidationResult) {
	// Decode resolve msg
	rmsg, err := DecodeResolveMsg(msg.GetData())
	if err != nil {
		log.Errorf("error decoding resolve msg cid: %s", err)
		return pubsub.ValidationReject
	}

	log.Infof("Received cross-msg resolution message of type: %v from subnet %v", rmsg.Type, rmsg.From)
	// Check the CID and messages sent are correct for push messages
	if rmsg.Type == Push {
		msgs := rmsg.CrossMsgs
		c, err := msgs.Cid()
		if err != nil {
			log.Errorf("error computing cross-msgs cid: %s", err)
			return pubsub.ValidationIgnore
		}
		if rmsg.Cid != c {
			log.Errorf("cid computed for crossMsgs not equal to the one requested: %s", err)
			return pubsub.ValidationReject
		}
	}

	// it's a correct message! make sure we've only seen it once
	if count := v.r.addMsgReceipt(rmsg.Type, rmsg.Cid, msg.GetFrom()); count > 0 {
		if pid == v.r.self {
			log.Warnf("local block has been seen %d times; ignoring", count)
		}

		return pubsub.ValidationIgnore
	}

	// Process the resolveMsg, record error, and return gossipsub validation status.
	sub, err := v.r.processResolveMsg(v.submgr, rmsg)
	if err != nil {
		log.Errorf("error processing resolve message: %s", err)
		return sub
	}

	// TODO: Any additional check?

	// Pass validated request.
	// msg.ValidatorData = rmsg

	return pubsub.ValidationAccept
}

func (r *Resolver) HandleIncomingResolveMsg(ctx context.Context, sub *pubsub.Subscription) {
	for {
		_, err := sub.Next(ctx)
		if err != nil {
			log.Warn("error from message subscription: ", err)
			if ctx.Err() != nil {
				log.Warn("quitting HandleResolveMessages loop")
				return
			}
			log.Error("error from resolve-msg subscription: ", err)
			continue
		}

		// Do nothing... everything happens in validate
		// Including message handling.
	}
}

func (r *Resolver) processResolveMsg(submgr subnet.SubnetMgr, rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	switch rmsg.Type {
	case Push:
		return r.processPush(rmsg)
	case PullMeta:
		return r.processPull(submgr, rmsg)
	case Response:
		return r.processResponse(rmsg)
	}
	return pubsub.ValidationReject, xerrors.Errorf("Resolve message type is not valid")

}

func (r *Resolver) processPush(rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	// Check if we are already storing the CrossMsgs CID locally.
	_, found, err := r.getLocal(rmsg.Cid)
	if err != nil {
		return pubsub.ValidationIgnore, xerrors.Errorf("Error getting cross-msg locally: %w", err)
	}
	if found {
		// Ignoring message, we already have these cross-msgs
		return pubsub.ValidationIgnore, nil
	}
	// If not stored locally, store it in the datastore for future access.
	if err := r.setLocal(rmsg.Cid, &rmsg.CrossMsgs); err != nil {
		return pubsub.ValidationIgnore, err
	}

	// TODO: Introduce checks here to ensure that push messages come from the right
	// source?
	return pubsub.ValidationAccept, nil
}

func (r *Resolver) processPull(submgr subnet.SubnetMgr, rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	// Inspect the state of the SCA to get crossMsgs behind the CID.
	st, store, err := submgr.GetSCAState(context.TODO(), r.netName)
	if err != nil {
		return pubsub.ValidationIgnore, err
	}
	msgs, found, err := st.GetCrossMsgs(store, rmsg.Cid)
	if err != nil {
		return pubsub.ValidationIgnore, err
	}
	if !found {
		// Reject instead of ignore. Someone may be trying to spam us with
		// random unvalid CIDs.
		return pubsub.ValidationReject, xerrors.Errorf("couldn't find crossmsgs for msgMeta with cid: %s", rmsg.Cid)
	}
	// Send response
	if err := r.PushCrossMsgs(*msgs, rmsg.From, true); err != nil {
		return pubsub.ValidationIgnore, err
	}
	// Publish a Response message to the source subnet if the CID is found.
	return pubsub.ValidationAccept, nil
}

func (r *Resolver) processResponse(rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	// Response messages are processed in the same way as push messages
	// (at least for now). Is the validation what differs between them.
	if sub, err := r.processPush(rmsg); err != nil {
		return sub, err
	}
	// If received successfully we can delete ongoingPull
	r.pullSuccess(rmsg.Cid)
	return pubsub.ValidationAccept, nil
}

func (r *Resolver) getLocal(c cid.Cid) (*sca.CrossMsgs, bool, error) {
	b, err := r.ds.Get(datastore.NewKey(c.String()))
	if err != nil {
		if err == datastore.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	out := &sca.CrossMsgs{}
	if err := out.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (r *Resolver) setLocal(c cid.Cid, msgs *sca.CrossMsgs) error {
	w := new(bytes.Buffer)
	if err := msgs.MarshalCBOR(w); err != nil {
		return err
	}
	return r.ds.Put(datastore.NewKey(c.String()), w.Bytes())
}

func (r *Resolver) publishMsg(m *ResolveMsg, id address.SubnetID) error {
	b, err := EncodeResolveMsg(m)
	if err != nil {
		return xerrors.Errorf("error serializing resolveMsg: %v", err)
	}
	return r.pubsub.Publish(SubnetResolverTopic(id), b)
}

// WaitCrossMsgsResolved waits until crossMsgs for meta have been fully resolved
func (r *Resolver) WaitCrossMsgsResolved(ctx context.Context, c cid.Cid, from address.SubnetID) chan error {
	out := make(chan error)
	resolved := false
	go func() {
		var err error
		for !resolved {
			select {
			case <-ctx.Done():
				out <- xerrors.Errorf("context timeout")
				return
			default:
				// Check if crossMsg fully resolved.
				_, resolved, err = r.ResolveCrossMsgs(c, address.SubnetID(from))
				if err != nil {
					out <- err
				}
				// If not resolved wait two seconds to poll again and see if it has been resolved
				// FIXME: This is not the best approach, but good enough for now.
				if !resolved {
					time.Sleep(2 * time.Second)
				}
			}
		}
		close(out)
	}()
	return out
}

func (r *Resolver) ResolveCrossMsgs(c cid.Cid, from address.SubnetID) ([]types.Message, bool, error) {
	// FIXME: This function should keep track of the retries that have been done,
	// and fallback to a 1:1 exchange if this fails.
	cross, found, err := r.getLocal(c)
	if err != nil {
		return []types.Message{}, false, err
	}
	// If found, inspect messages and keep resolving metas
	if found {
		msgs := cross.Msgs
		foundAll := true
		// If there is some msgMeta to resolve, resolve it
		for _, mt := range cross.Metas {
			c, err := mt.Cid()
			if err != nil {
				return []types.Message{}, false, nil
			}
			// Recursively resolve crossMsg for meta
			cross, found, err := r.ResolveCrossMsgs(c, address.SubnetID(mt.From))
			if err != nil {
				return []types.Message{}, false, nil
			}
			// Append messages found
			msgs = append(msgs, cross...)
			foundAll = foundAll && found
		}
		if foundAll {
			// Hurray! We resolved everything, ready to return.
			return msgs, true, nil
		}

		// We haven't resolved everything, wait for the next round to finish
		// pulling everything.
		// NOTE: We could consider still sending partial results here.
		return []types.Message{}, true, nil
	}

	// If not try to pull message
	if r.shouldPull(c) {
		return []types.Message{}, false, r.PullCrossMsgs(c, from)
	}

	// If we shouldn't pull yet because we pulled recently
	// do nothing for now, and notify that is wasn't resolved yet.
	return []types.Message{}, false, nil

}

func (r *Resolver) PushCrossMsgs(msgs sca.CrossMsgs, id address.SubnetID, isResponse bool) error {
	c, err := msgs.Cid()
	if err != nil {
		return err
	}
	m := &ResolveMsg{
		Type:      Push,
		From:      r.netName,
		Cid:       c,
		CrossMsgs: msgs,
	}
	if isResponse {
		m.Type = Response
	}
	return r.publishMsg(m, id)
}

func (r *Resolver) PushMsgFromCheckpoint(ch *schema.Checkpoint, st *sca.SCAState, store adt.Store) error {
	// For each crossMsgMeta
	for _, meta := range ch.CrossMsgs() {
		// Get the crossMsgs behind Cid from SCA state and push it.
		c, err := meta.Cid()
		if err != nil {
			return err
		}
		msgs, found, err := st.GetCrossMsgs(store, c)
		if err != nil {
			return err
		}
		if !found {
			return xerrors.Errorf("couldn't found crossmsgs for msgMeta with cid: %s", c)
		}
		// Push cross-msgs to subnet
		if err = r.PushCrossMsgs(*msgs, address.SubnetID(meta.To), false); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) PullCrossMsgs(c cid.Cid, id address.SubnetID) error {
	m := &ResolveMsg{
		Type: PullMeta,
		From: r.netName,
		Cid:  c,
	}
	return r.publishMsg(m, id)
}
