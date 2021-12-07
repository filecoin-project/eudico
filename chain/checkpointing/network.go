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
	n.topic.Publish(ctx, data)
}

func (n *Network) Done() {
	fmt.Println("Done")

	/* Might need to do something here ? */
}

func (n *Network) Parties() party.IDSlice {
	return n.parties
}
