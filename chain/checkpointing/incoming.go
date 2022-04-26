// +build exclude

package checkpointing

import (
	"context"
	"fmt"
	"time"
	"os"
	"crypto/sha256"
	"errors"


	peer "github.com/libp2p/go-libp2p-core/peer"
	lru "github.com/hashicorp/golang-lru"
	"github.com/sa8/multi-party-sig/pkg/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"golang.org/x/xerrors"
)

type Network struct {
	sub   *pubsub.Subscription
	topic *pubsub.Topic
}
// this define new means of communication, not a network
// we reuse the same network
// in the taurus group, they called it new network
func NewNetwork(sub *pubsub.Subscription, topic *pubsub.Topic) *Network {
	c := &Network{
		sub:   sub,
		topic: topic,
	}
	return c
}


type messageReceiptCache struct {
	messages *lru.TwoQueueCache
}

func (mrc *messageReceiptCache) add(mcid string) int {
	val, ok := brc.blocks.Get(bcid)
	if !ok {
		brc.blocks.Add(bcid, int(1))
		return 0
	}

	brc.blocks.Add(bcid, val.(int)+1)
	return val.(int)
}

func newMessageReceiptCache() *messageReceiptCache {
	c, _ := lru.New2Q(8192)

	return &messageReceiptCache{
		messages: c,
	}
}
type MessageValidator struct {
	self peer.ID

	peers *lru.TwoQueueCache

	killThresh int

	recvMessages *messageReceiptCache

	//blacklist func(peer.ID)

}

func NewMessageValidator(self peer.ID,  blacklist func(peer.ID)) *MessageValidator {
	p, _ := lru.New2Q(4096)
	return &MessageValidator{
		self:       self,
		peers:      p,
		killThresh: 10,
		blacklist:  blacklist,
		recvMessages: newMessageReceiptCache(),
	}
}


func (mv *MessageValidator) flagPeer(p peer.ID) {
	v, ok := mv.peers.Get(p)
	if !ok {
		mv.peers.Add(p, int(1))
		return
	}

	val := v.(int)

	if val >= mv.killThresh {
		log.Warnf("blacklisting peer %s", p)
		mv.blacklist(p)
		return
	}

	mv.peers.Add(p, v.(int)+1)
}
func HashedCid(cm *protocol.Message) (string, error) {
	// to do
	if len((*cm).Data) == 0 {
		return "", errors.New("Message data is empty.")
	}
	sha256 := sha256.Sum256((*cm).Data)

	return hex.EncodeToString(sha256[:]), nil
	//return hex.EncodeToString((*cm).content), nil

}

func (mv *MessageValidator) Validate(ctx context.Context, pid peer.ID, msg *protocol.Message, h protocol.Handler) (res pubsub.ValidationResult) {
	defer func() {
		if rerr := recover(); rerr != nil {
			err := xerrors.Errorf("validate message: %s", rerr)
			// I don't think we need the following line in our case
			//recordFailure(ctx, metrics.BlockValidationFailure, err.Error())
			mv.flagPeer(pid)
			res = pubsub.ValidationReject
			return
		}
	}()

	var what string
	//res, what = mv.consensus.ValidateBlockPubsub(ctx, pid == bv.self, msg)
	//verify with h.can accept?
	if h.CanAccept(msg){
 	// it's a good message! make sure we've only seen it once
		if count := mv.recvMessages.add(HashedCID(msg)); count > 0 {
			if pid == mv.self {
				log.Warnf("local block has been seen %d times; ignoring", count)
			}

			// TODO: once these changes propagate to the network, we can consider
			// dropping peers who send us the same block multiple times
			return pubsub.ValidationIgnore
		}
	} else {
		//recordFailure(ctx, metrics.MessageValidationFailure, nil)
		return pubsub.ValidationReject
	}

	return pubsub.ValidationAccept
}



// we could potentially re-use next.sub.net
// protocol message est la structure 
func (n *Network) Next(ctx context.Context) *protocol.Message {
	msg, err := n.sub.Next(ctx)
	if err == context.Canceled {
		// We are actually done and don't want to wait for messages anymore
		return nil
	}

	if err != nil {
		panic(err)
	}

	// Unwrapping protocol message from PubSub message
	// see https://pkg.go.dev/github.com/libp2p/go-libp2p-pubsub@v0.5.3/pb#Message
	// https://pkg.go.dev/github.com/taurusgroup/multi-party-sig@v0.6.0-alpha-2021-09-21/pkg/protocol?utm_source=gopls#Message
	var pmessage protocol.Message
	// this part is very important
	err = pmessage.UnmarshalBinary(msg.Data)
	if err != nil {
		panic(err)
	}

	return &pmessage
}

func (n *Network) Send(ctx context.Context, msg *protocol.Message) {
	data, err := msg.MarshalBinary()
	if err != nil {
		panic(err)
	}
	err = n.topic.Publish(ctx, data)
	if err != nil {
		panic(err)
	}
}

/*
	Handling incoming and outgoing messages
*/

func broadcastingMessage(ctx context.Context, h protocol.Handler, network *Network, over chan bool) {
	for {
		msg, ok := <-h.Listen()
		fmt.Println("Outgoing message:", msg)
		if !ok {
			// the channel was closed, indicating that the protocol is done executing.
			// we sleep two seconds to be sure we received all messages before closing
			//time.Sleep(2*time.Second)
			close(over)
			return
		}
		network.Send(ctx, msg)
	}
}

func waitingMessages(ctx context.Context, h protocol.Handler, network *Network, over chan bool) {
	for {
		select {
		case <-over:
			return
		default:
			msg := network.Next(ctx)
			fmt.Println("Incoming message:", msg, h.CanAccept(msg))
			/*if h.CanAccept(msg) {
				// This message is ours
				fmt.Println("Incoming message:", msg)
			}*/
			h.Accept(msg)
		}
	}
}

// This could be simplified
// next and send very similar to next and publish in libp2p
// the code could be simplified to use next and publish.s
func LoopHandlerSign(ctx context.Context, h protocol.Handler, network *Network, num int, file *os.File) {
	defer timeTrack(time.Now(), "Signing", num, file)
	over := make(chan bool)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go broadcastingMessage(ctx, h, network, over)
	go waitingMessages(ctx, h, network, over)
	go waitTimeOut(ctx, h, network, over)

	<-over

	fmt.Println("We are done")
	
	//file.Close()
}
func waitTimeOut(ctx context.Context, h protocol.Handler, network *Network, over chan bool) {
	for {
		select {
		case <-over:
			return
		default:
			h.TimeOutExpired()
		}
	}
}
func LoopHandlerDKG(ctx context.Context, h protocol.Handler, network *Network, num int, file *os.File) {
	defer timeTrack(time.Now(), "DKG", num, file)
	over := make(chan bool)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go broadcastingMessage(ctx, h, network, over)
	go waitingMessages(ctx, h, network, over)
	go waitTimeOut(ctx, h, network, over)

	<-over

	fmt.Println("We are done")
	
}

// func LoopHandlerDKG(ctx context.Context, h protocol.Handler, network *Network) {
// 	over := make(chan bool)
// 	ctx, cancel := context.WithCancel(ctx)
// 	defer cancel()
// 	for {
// 		select {

// 		// outgoing messages
// 		case msg, ok := <-h.Listen():
// 			fmt.Println("Outgoing message:", msg)
// 			if !ok {
// 			// the channel was closed, indicating that the protocol is done executing.
// 				close(over)
// 				return
// 			}
// 			go network.Send(ctx, msg)

// 		// incoming messages
// 		case msg := <-network.Next(ctx):
// 			fmt.Println("Incoming message:", msg, h.CanAccept(msg))
// 			/*if h.CanAccept(msg) {
// 				// This message is ours
// 				fmt.Println("Incoming message:", msg)
// 			}*/
// 			h.Accept(msg)

// 		// timeout case
// 		default: //timeout done
// 			h.TimeOutExpired()
// 		}
// 	}
// }


// func LoopHandlerDKG(ctx context.Context, h protocol.Handler, network *Network) {
// 	over := make(chan bool)
// 	for {
// 		select {
// 		// outgoing messages
// 		case msg, ok := <-h.Listen():
// 			if !ok {
// 				close(over)
// 				// the channel was closed, indicating that the protocol is done executing.
// 				return
// 			}
// 			go network.Send(ctx,msg)

// 		// incoming messages
// 		case msg := network.Next(ctx):
// 			h.Accept(msg)

// 		// timeout case
// 		default: //timeout done
// 			h.TimeOutExpired()
// 		}
// 	}

// 	fmt.Println("We are done")
// }

// // HandlerLoop blocks until the handler has finished. The result of the execution is given by Handler.Result().
// func LoopHandler(ctx context.Context, h protocol.Handler, network *Network) {
// 	over := make(chan bool)
// 	for {
// 		select {

// 		// outgoing messages
// 		case msg, ok := <-h.Listen():
// 			if !ok {
// 				close(over)
// 				// the channel was closed, indicating that the protocol is done executing.
// 				return
// 			}
// 			go network.Send(ctx,msg)

// 		// incoming messages
// 		case msg1 := <- network.Next(ctx):
// 			h.Accept(msg1)
// 		}
// 	}

// 	fmt.Println("We are done")
// }

