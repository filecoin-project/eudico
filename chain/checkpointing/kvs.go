package checkpointing

//go:generate go run ./gen/gen.go

import (
	"bytes"
	"context"
	"sync"
	"time"
	"crypto/sha256"
	"errors"
	"encoding/hex"
	"fmt"

	// "github.com/filecoin-project/go-address"
	// "github.com/filecoin-project/lotus/chain/actors/adt"
	// "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	// "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	//"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	//"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	lru "github.com/hashicorp/golang-lru"
	//"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	nsds "github.com/ipfs/go-datastore/namespace"

	//logging "github.com/ipfs/go-log/v2"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
	xerrors "golang.org/x/xerrors"
)

const retryTimeout = 10 * time.Second

//var log = logging.Logger("checkpointing-kvs")

// in the checkpointing case we always use the same topic
// we will use "pikachu"
// func SubnetResolverTopic(id address.SubnetID) string {
// 	return "/fil/subnet/resolver" + id.String()
// }

// same for namespace, we will use "pikachu"
// func resolverNamespace(id address.SubnetID) datastore.Key {
// 	return datastore.NewKey("/resolver/" + id.String())
// }

type Resolver struct {
	//netName address.SubnetID
	self   peer.ID
	ds     datastore.Datastore
	pubsub *pubsub.PubSub

	// Caches to track duplicate and frequent msgs
	pushCache *msgReceiptCache
	pullCache *msgReceiptCache
	// NOTE: We don't track number of response
	// messages sent for now. We accept any number.
	// We will need to address this to prevent potential
	// spamming.
	// responseCache *msgReceiptCache

	lk          sync.Mutex
	ongoingPull map[string]time.Time
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

type MsgData struct{
	Content []byte
}

type ResolveMsg struct {
	// From subnet -> not needed for checkpointing
	//From address.SubnetID
	// Message type being propagated
	Type MsgType
	// Cid of the content
	Cid string
	// MsgMeta being propagated (if any)-> change this to be string?
	//CrossMsgs sca.CrossMsgs
	// Checkpoint being propagated (if any)
	// Checkpoint schema.Checkpoint

	//for checkpointing, we use []byte
	Content MsgData
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

func (r *Resolver) addMsgReceipt(t MsgType, bcid string, from peer.ID) int {
	if t == Push {
		// All push messages are considered equal independent of
		// the source.
		return r.pushCache.add(bcid)
	}
	// We allow each peer.ID in a subnet to send a pull request
	// for each CID without being rejected.
	// FIXME: Additional checks may be required to prevent malicious
	// peers from spamming the topic with infinite requests.
	// Deferring the design of a smarter logic here.
	return r.pullCache.add(bcid + from.String())
}

func NewRootResolver(self peer.ID, ds dtypes.MetadataDS, pubsub *pubsub.PubSub) *Resolver {
	return NewResolver(self, ds, pubsub)
}
func NewResolver(self peer.ID, ds datastore.Datastore, pubsub *pubsub.PubSub) *Resolver {
	return &Resolver{
		self:        self,
		ds:          nsds.Wrap(ds, datastore.NewKey("pikachu")),
		//ds: 		 ds,
		pubsub:      pubsub,
		pushCache:   newMsgReceiptCache(),
		pullCache:   newMsgReceiptCache(),
		ongoingPull: make(map[string]time.Time),
	}
}

func HandleMsgs(mctx helpers.MetricsCtx, lc fx.Lifecycle, r *Resolver) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	if err := r.HandleMsgs(ctx); err != nil {
		panic(err)
	}
}

func (r *Resolver) HandleMsgs(ctx context.Context) error {
	// Register new message validator for resolver msgs.
	v := NewValidator(r)
	if err := r.pubsub.RegisterTopicValidator("pikachu", v.Validate); err != nil {
		return err
	}

	log.Infof("subscribing to content resolver topic pikachu")

	// Subscribe to subnet resolver topic.
	msgSub, err := r.pubsub.Subscribe("pikachu") //nolint
	if err != nil {
		return err
	}
	fmt.Println("suscribed to message sub", msgSub)
	//time.Sleep(6 * time.Second)

	// Start handle incoming resolver msg.
	go r.HandleIncomingResolveMsg(ctx, msgSub)
	return nil
}

func (r *Resolver) Close() error {
	// Unregister topic validator when resolver is closed. If not, when
	// initializing it again registering the validator will fail.
	return r.pubsub.UnregisterTopicValidator("pikachu")
}
func (r *Resolver) shouldPull(c string) bool {
	r.lk.Lock()
	defer r.lk.Unlock()
	if time.Since(r.ongoingPull[c]) > retryTimeout {
		r.ongoingPull[c] = time.Now()
		return true
	}
	return false
}

func (r *Resolver) pullSuccess(c string) {
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
}

func NewValidator( r *Resolver) *Validator {
	return &Validator{r}
}

func (v *Validator) Validate(ctx context.Context, pid peer.ID, msg *pubsub.Message) (res pubsub.ValidationResult) {
	// Decode resolve msg
	fmt.Println("Calling Validate ")
	rmsg, err := DecodeResolveMsg(msg.GetData())
	fmt.Println("decoded message cid: ", rmsg.Cid)
	fmt.Println("Message is coming from: ", pid.String())
	if err != nil {
		fmt.Println("errod decoding message cid")
		log.Errorf("error decoding resolve msg cid: %s", err)
		return pubsub.ValidationReject
	}
	fmt.Println(rmsg)
	fmt.Println("we are here! hello")
	log.Infof("Received kvs resolution message of type: %v, from %v", rmsg.Type, pid.String())
	log.Warnf("trying to warn you")
	//fmt.Println("Message id: ", msg.Cid)
	// Check the CID and messages sent are correct for push messages
	if rmsg.Type == Push {
		fmt.Println("message to validate is of type push")
		msgs := rmsg.Content
		c, err := msgs.Cid() //
		if err != nil {
			log.Errorf("error computing msgs cid: %s", err)
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
	sub, err := v.r.processResolveMsg(ctx, rmsg)
	if err != nil {
		log.Errorf("error processing resolve message: %s", err)
		return sub
	}

	// TODO: Any additional check?

	// Pass validated request.
	// msg.ValidatorData = rmsg
	fmt.Println("end of validate")

	return pubsub.ValidationAccept
}

func (cm *MsgData) Cid() (string, error) {
	// to do
	if len((*cm).Content) == 0 {
		return "", errors.New("Message data is empty.")
	}
	sha256 := sha256.Sum256((*cm).Content)

	return hex.EncodeToString(sha256[:]), nil
	//return hex.EncodeToString((*cm).content), nil

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

func (r *Resolver) processResolveMsg(ctx context.Context, rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	switch rmsg.Type {
	case Push:
		return r.processPush(ctx, rmsg)
	case PullMeta:
		return r.processPull( rmsg)
	case Response:
		return r.processResponse(ctx, rmsg)
	}
	return pubsub.ValidationReject, xerrors.Errorf("Resolve message type is not valid")

}

func (r *Resolver) processPush(ctx context.Context, rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	// Check if we are already storing the CrossMsgs CID locally.
	fmt.Println("Processing push for message with cid: ", rmsg.Cid)
	_, found, err := r.getLocal(ctx, rmsg.Cid)
	if err != nil {
		return pubsub.ValidationIgnore, xerrors.Errorf("Error getting msg locally: %w", err)
	}
	if found {
		// Ignoring message, we already have these cross-msgs
		return pubsub.ValidationIgnore, nil
	}
	// If not stored locally, store it in the datastore for future access.
	if err := r.setLocal(ctx, rmsg.Cid, &rmsg.Content); err != nil {
		return pubsub.ValidationIgnore, err
	}
	fmt.Println("Message added! yay")
	
	// TODO: Introduce checks here to ensure that push messages come from the right
	// source?
	return pubsub.ValidationAccept, nil
}

func (r *Resolver) processPull(rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	// Inspect the state of the SCA to get crossMsgs behind the CID.
	// st, store, err := submgr.GetSCAState(context.TODO(), r.netName)
	// if err != nil {
	// 	return pubsub.ValidationIgnore, err
	// }
	//msgs, found, err := st.GetCrossMsgs(store, rmsg.Cid)
	// if err != nil {
	// 	return pubsub.ValidationIgnore, err
	// }
	fmt.Println("Processing a pull request for message with cid: ",rmsg.Cid)
	msg, found, err := r.getLocal(context.TODO(), rmsg.Cid)
	if err != nil {
		return pubsub.ValidationIgnore, err
	}
	if !found {
		// Reject instead of ignore. Someone may be trying to spam us with
		// random unvalid CIDs.
		//return pubsub.ValidationIgnore, xerrors.Errorf("couldn't find data for msgMeta with cid: %s", rmsg.Cid)
		return pubsub.ValidationAccept, nil
		//return pubsub.ValidationReject, xerrors.Errorf("couldn't find crossmsgs for msgMeta with cid: %s", rmsg.Cid)
	}
	// Send response
	if err := r.PushCheckpointMsgs(*msg,true); err != nil {
		return pubsub.ValidationIgnore, err
	}
	// Publish a Response message to the source subnet if the CID is found.
	return pubsub.ValidationAccept, nil
}

// GetCrossMsgs returns the crossmsgs from a CID in the registry.
// func (st *SCAState) GetCrossMsgs(store adt.Store, c string) (*MsgData, bool, error) {
// 	msgMetas, err := adt.AsMap(store, st.CheckMsgsRegistry, builtin.DefaultHamtBitwidth)
// 	if err != nil {
// 		return nil, false, err
// 	}
// 	var out CrossMsgs
// 	found, err := msgMetas.Get(abi.CidKey(c), &out)
// 	if err != nil {
// 		return nil, false, xerrors.Errorf("failed to get crossMsgMeta from registry with cid %v: %w", c, err)
// 	}
// 	if !found {
// 		return nil, false, nil
// 	}
// 	return &out, true, nil
// }

func (r *Resolver) processResponse(ctx context.Context, rmsg *ResolveMsg) (pubsub.ValidationResult, error) {
	// Response messages are processed in the same way as push messages
	// (at least for now). Is the validation what differs between them.
	if sub, err := r.processPush(ctx, rmsg); err != nil {
		return sub, err
	}
	// If received successfully we can delete ongoingPull
	r.pullSuccess(rmsg.Cid)
	return pubsub.ValidationAccept, nil
}

func (r *Resolver) getLocal(ctx context.Context, c string) (*MsgData, bool, error) {
	b, err := r.ds.Get(ctx, datastore.NewKey(c))
	if err != nil {
		if err == datastore.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	out := &MsgData{}
	if err := out.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (r *Resolver) setLocal(ctx context.Context, c string, msgs *MsgData) error {
	w := new(bytes.Buffer)
	if err := msgs.MarshalCBOR(w); err != nil {
		return err
	}
	fmt.Println("We are currently adding message %v to KVS.", msgs)
	return r.ds.Put(ctx, datastore.NewKey(c), w.Bytes())
}

func (r *Resolver) publishMsg(m *ResolveMsg) error {
	b, err := EncodeResolveMsg(m)
	if err != nil {
		return xerrors.Errorf("error serializing resolveMsg: %v", err)
	}
	fmt.Println("publishing message ",m)
	return r.pubsub.Publish("pikachu", b)
}

//WaitCrossMsgsResolved waits until crossMsgs for meta have been fully resolved
func (r *Resolver) WaitCheckpointResolved(ctx context.Context, c string) chan error {
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
				_, resolved, err = r.ResolveCheckpointMsgs(ctx, c)
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
	fmt.Println("done with WaitCheckpointResolved")
	return out
}

func (r *Resolver) ResolveCheckpointMsgs(ctx context.Context, c string) ([]byte, bool, error) {
	// FIXME: This function should keep track of the retries that have been done,
	// and fallback to a 1:1 exchange if this fails.
	cross, found, err := r.getLocal(ctx, c)
	if err != nil {
		return []byte{}, false, err
	}
	if found {
		// Hurray! We resolved everything, ready to return.
		return cross.Content, true, nil
	}
	// If not try to pull message
	if r.shouldPull(c) {
		return []byte{}, false, r.PullCheckpointMsgs(c)
	}

	// If we shouldn't pull yet because we pulled recently
	// do nothing for now, and notify that is wasn't resolved yet.
	return []byte{}, false, nil

}

func (r *Resolver) PushCheckpointMsgs(msgs MsgData, isResponse bool) error {
	c, err := msgs.Cid()
	if err != nil {
		return err
	}
	m := &ResolveMsg{
		Type:      Push,
		Cid:       c,
		Content: msgs,
	}
	if isResponse {
		m.Type = Response
	}
	return r.publishMsg(m)
}

// func (r *Resolver) PushMsgFromCheckpoint(ch *schema.Checkpoint, st *sca.SCAState, store adt.Store) error {
// 	// For each crossMsgMeta
// 	for _, meta := range ch.CrossMsgs() {
// 		// Get the crossMsgs behind Cid from SCA state and push it.
// 		c, err := meta.Cid()
// 		if err != nil {
// 			return err
// 		}
// 		msgs, found, err := st.GetCrossMsgs(store, c)
// 		if err != nil {
// 			return err
// 		}
// 		if !found {
// 			return xerrors.Errorf("couldn't found crossmsgs for msgMeta with cid: %s", c)
// 		}
// 		// Push cross-msgs to subnet
// 		if err = r.PushCrossMsgs(*msgs, false); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }

func (r *Resolver) PullCheckpointMsgs(ci string) error {
	m := &ResolveMsg{
		Type: PullMeta,
		Cid:  ci,
	}
	return r.publishMsg(m)
}
