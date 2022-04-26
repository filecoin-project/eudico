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
	"os"

	// "github.com/filecoin-project/go-address"
	// "github.com/filecoin-project/lotus/chain/actors/adt"
	// "github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	// "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	//"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	//"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	//"github.com/ipfs/go-cid"
	"github.com/sa8/multi-party-sig/pkg/protocol"
	//logging "github.com/ipfs/go-log/v2"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
	xerrors "golang.org/x/xerrors"
)

type MessageValidator struct {
	//netName address.SubnetID
	self   peer.ID
	pubsub *pubsub.PubSub

	// Caches to track duplicate and frequent msgs
	messageCache *msgReceiptCache
	// NOTE: We don't track number of response
	// messages sent for now. We accept any number.
	// We will need to address this to prevent potential
	// spamming.
	// responseCache *msgReceiptCache

	lk          sync.Mutex

	h protocol.Handler

}

type SigningMsg struct {
	// From subnet -> not needed for checkpointing
	//From address.SubnetID
	// Message type being propagated
	// Cid of the content
	Cid string
	// MsgMeta being propagated (if any)-> change this to be string?
	//CrossMsgs sca.CrossMsgs
	// Checkpoint being propagated (if any)
	// Checkpoint schema.Checkpoint

	//for checkpointing, we use []byte
	Content MsgData
}





func (r *MessageValidator) addMsgReceipt( bcid string, from peer.ID) int {

	return r.messageCache.add(bcid)

}

func NewMessageValidator(self peer.ID, pubsub *pubsub.PubSub, h protocol.Handler) *MessageValidator {
	return &MessageValidator{
		self:        self,
		pubsub:      pubsub,
		messageCache:   newMsgReceiptCache(),
		h: h,
	}
}

func HandleSigningMsgs(mctx helpers.MetricsCtx, lc fx.Lifecycle, r *MessageValidator) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	if err := r.HandleSigningMsgs(ctx); err != nil {
		panic(err)
	}
}

func (r *MessageValidator) HandleSigningMsgs(ctx context.Context) error {
	// Register new message validator for resolver msgs.
	fmt.Println("Inside handle signing msg")
	v := NewSigningValidator(r)
	if err := r.pubsub.RegisterTopicValidator("signing", v.Validate); err != nil {
		return err
	}
	fmt.Println("topic registred")

	log.Infof("subscribing to signing topic")

	// Subscribe to signing topic.
	topic, err := r.pubsub.Join("signing")
	if err != nil {
		return err
	}
	fmt.Println("topic joined")
	// msgSub, err := r.pubsub.Subscribe("pikachu") //nolint
	// if err != nil {
	// 	return err
	// }
	msgSub, err := topic.Subscribe(pubsub.WithBufferSize(10000))
	if err != nil {
		return err
	}
	fmt.Println("topic suscribed")

	//time.Sleep(6 * time.Second)

	// Start handle incoming resolver msg.
	go r.HandleIncomingSigningMsg(ctx, msgSub)
	return nil
}
func (r *MessageValidator) Close() error {
	// Unregister topic validator when resolver is closed. If not, when
	// initializing it again registering the validator will fail.
	return r.pubsub.UnregisterTopicValidator("signing")
}

func DecodeSigningMsg(b []byte) (*SigningMsg, error) {
	var bm SigningMsg
	if err := bm.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, err
	}

	return &bm, nil
}

func EncodeSigningMsg(m *SigningMsg) ([]byte, error) {
	w := new(bytes.Buffer)
	if err := m.MarshalCBOR(w); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

type SigningValidator struct {
	r      *MessageValidator
}

func NewSigningValidator( r *MessageValidator) *SigningValidator {
	return &SigningValidator{r}
}


func (v *SigningValidator) Validate(ctx context.Context, pid peer.ID, msg *pubsub.Message) (res pubsub.ValidationResult) {
	rmsg, err := DecodeSigningMsg(msg.GetData())
	if err != nil {
		fmt.Println("Message decode failed")
		panic(err)
	}
	var pmessage protocol.Message
	// this part is very important
	err = pmessage.UnmarshalBinary(rmsg.Content.Content)
	if err != nil {
		panic(err)
	}
	if v.r.h.CanAcceptLibp2p(&pmessage) {
		fmt.Println("Accept message ", &pmessage)
		v.r.h.Accept(&pmessage)
		return pubsub.ValidationAccept
	}
	fmt.Println("Ignore message: ", &pmessage)
	return pubsub.ValidationIgnore
}


func (r *MessageValidator) HandleIncomingSigningMsg(ctx context.Context, sub *pubsub.Subscription) {
	for {
		_, err := sub.Next(ctx)
		fmt.Println("Incoming message received")
		if err != nil {
			log.Warn("error from message subscription: ", err)
			if ctx.Err() != nil {
				log.Warn("quitting HandleSigningMessages loop")
				return
			}
			log.Error("error from signing-msg subscription: ", err)
			continue
		}



		// Do nothing... everything happens in validate
		// Including message handling.
	}
}


func LoopHandlerSign(ctx context.Context, h protocol.Handler,c *CheckpointingSub, num int, file *os.File) {
	defer timeTrack(time.Now(), "Signing", num, file)
	over := make(chan bool)
	fmt.Println("starting loop")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := NewMessageValidator(c.host.ID(), c.pubsub, h)
	
	fmt.Println("Inside handle signing msg")
	v := NewSigningValidator(r)
	if err := r.pubsub.RegisterTopicValidator("signing", v.Validate); err != nil {
		fmt.Println(err)
	}
	fmt.Println("topic registred")

	log.Infof("subscribing to signing topic")

	// Subscribe to signing topic.
	topic, err := r.pubsub.Join("signing")
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("topic joined")
	// msgSub, err := r.pubsub.Subscribe("pikachu") //nolint
	// if err != nil {
	// 	return err
	// }
	msgSub, err := topic.Subscribe(pubsub.WithBufferSize(10000))
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("topic suscribed")

	//time.Sleep(6 * time.Second)

	// Start handle incoming resolver msg.
	go r.HandleIncomingSigningMsg(ctx, msgSub)
	go r.broadcastingMessage(ctx, h, over)
	//go waitingMessages(ctx, h, network, over)
	go r.waitTimeOut(ctx, h, over)

	<-over

	fmt.Println("We are done")
	
	//file.Close()
}
func (r *MessageValidator) waitTimeOut(ctx context.Context, h protocol.Handler, over chan bool) {
	for {
		select {
		case <-over:
			return
		default:
			h.TimeOutExpired()
		}
	}
}
func (r *MessageValidator) broadcastingMessage(ctx context.Context, h protocol.Handler,  over chan bool) {
	for {
		msg, ok := <-h.Listen()
		fmt.Println("Outgoing message:", msg)
		if !ok {
			fmt.Println("message not ok closing chan")
			// the channel was closed, indicating that the protocol is done executing.
			// we sleep two seconds to be sure we received all messages before closing
			//time.Sleep(2*time.Second)
			close(over)
			return
		}
		data, err := msg.MarshalBinary()
		if err != nil {
			panic(err)
		}
		msgData := &MsgData{Content: data}
		cid,_ := HashedCid(msgData)
		m := &SigningMsg{
			Cid:  cid,
			Content: *msgData,
		}

		err = r.publishMsg(m)
		if err != nil {
			fmt.Println("Published message panicked", err)
			//panic(err)
		}
	}
}


func (r *MessageValidator) publishMsg(m *SigningMsg) error {
	b, err := EncodeSigningMsg(m)
	if err != nil {
		fmt.Println("error serializing signingMsg")
		return xerrors.Errorf("error serializing signingMsg: %v", err)
	}
	return r.pubsub.Publish("signing", b)
}
func HashedCid(cm *MsgData) (string, error) {
	// to do
	if len((*cm).Content) == 0 {
		return "", errors.New("Message data is empty.")
	}
	sha256 := sha256.Sum256((*cm).Content)

	return hex.EncodeToString(sha256[:]), nil
	//return hex.EncodeToString((*cm).content), nil

}


