// +build exclude

package checkpointing
import (
	"context"
	"fmt"
	"time"
	"os"

	"github.com/sa8/multi-party-sig/pkg/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
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
	defer timeTrack(time.Now(), "Signing-failure", num, file)
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
	defer timeTrack(time.Now(), "DKG-failure", num, file)
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

