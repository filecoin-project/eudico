package checkpointing

import (
	"context"
	"fmt"

	"github.com/Zondax/multi-party-sig/pkg/party"
	"github.com/Zondax/multi-party-sig/pkg/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

type Network struct {
	sub     *pubsub.Subscription
	topic   *pubsub.Topic
	parties party.IDSlice
}

func NewNetwork(parties party.IDSlice, sub *pubsub.Subscription, topic *pubsub.Topic) *Network {
	c := &Network{
		sub:     sub,
		topic:   topic,
		parties: parties,
	}
	return c
}

func (n *Network) Next(ctx context.Context) *protocol.Message {
	msg, err := n.sub.Next(ctx)
	if err == context.Canceled {
		// We are actually done and don't want to wait for messages anymore
		return nil
	}

	if err != nil {
		panic(err)
	}

	// Converting a pubsub.Message into a protocol.Message
	// see https://pkg.go.dev/github.com/libp2p/go-libp2p-pubsub@v0.5.3/pb#Message
	// https://pkg.go.dev/github.com/taurusgroup/multi-party-sig@v0.6.0-alpha-2021-09-21/pkg/protocol?utm_source=gopls#Message
	var pmessage protocol.Message
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

func (n *Network) Done() {
	fmt.Println("Done")

	/* Might need to do something here ? */
}

func (n *Network) Parties() party.IDSlice {
	return n.parties
}

/*
	Handling incoming and outgoing messages
*/

func broadcastingMessage(ctx context.Context, h protocol.Handler, network *Network, over chan bool) {
	for {
		msg, ok := <-h.Listen()
		fmt.Println("Outgoing message:", msg)
		if !ok {
			network.Done()
			// the channel was closed, indicating that the protocol is done executing.
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
			if h.CanAccept(msg) {
				// This message is ours
				fmt.Println("Incoming message:", msg)
			}
			h.Accept(msg)
		}

	}
}

func LoopHandler(ctx context.Context, h protocol.Handler, network *Network) {
	over := make(chan bool)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go broadcastingMessage(ctx, h, network, over)
	go waitingMessages(ctx, h, network, over)

	<-over
}
