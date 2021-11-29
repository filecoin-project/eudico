package checkpointing

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/Zondax/multi-party-sig/pkg/party"
	"github.com/Zondax/multi-party-sig/pkg/protocol"
	"github.com/Zondax/multi-party-sig/protocols/frost"
	"github.com/Zondax/multi-party-sig/protocols/frost/keygen"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
)

var log = logging.Logger("checkpointing")

type CheckpointingSub struct {
	host   host.Host
	pubsub *pubsub.PubSub
	// Topic for keygen
	topic *pubsub.Topic
	// Sub for keygen
	sub *pubsub.Subscription
	// This is the API for the fullNode in the root chain.
	api *impl.FullNodeAPI
	// Listener for events of the root chain.
	events *events.Events
	// Generated public key
	pubkey []byte
	// taproot config
	config *keygen.TaprootConfig
	// If we have started the key generation
	genkey bool
	// Initiated
	init bool
	// Previous tx
	ptxid string
	// Tweaked value
	tweakedValue []byte
	//checkpoint value
	cp []byte
}

func NewCheckpointSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	host host.Host,
	pubsub *pubsub.PubSub,
	api impl.FullNodeAPI,
) (*CheckpointingSub, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)
	// Starting shardSub to listen to events in the root chain.
	e, err := events.NewEvents(ctx, &api)
	if err != nil {
		return nil, err
	}

	// Load configTaproot
	fmt.Println(os.Getenv("EUDICO_PATH"))
	// Read file

	return &CheckpointingSub{
		pubsub: pubsub,
		topic:  nil,
		sub:    nil,
		host:   host,
		api:    &api,
		events: e,
		genkey: false,
		init:   false,
		ptxid:  "",
	}, nil
}

func (c *CheckpointingSub) listenCheckpointEvents(ctx context.Context) {

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for power actor")

		idsStrings := c.orderParticipantsList()

		fmt.Println("Participants list :", idsStrings)

		ids := c.formIDSlice(idsStrings)

		id := party.ID(c.host.ID().String())

		threshold := 2
		n := NewNetwork(ids, c.sub, c.topic)
		f := frost.KeygenTaproot(id, ids, threshold)

		handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})

		fmt.Println(handler)

		if err != nil {
			fmt.Println(err)
			log.Fatal("Not working")
		}
		c.LoopHandler(ctx, handler, n)
		r, err := handler.Result()
		if err != nil {
			fmt.Println(err)
			log.Fatal("Not working neither")
		}
		fmt.Println("Result :", r)

		c.config = r.(*keygen.TaprootConfig)

		if !c.init {
			cp := []byte("random data")[:]
			pubkeyShort := GenCheckpointPublicKeyTaproot(c.config.PublicKey, cp)
			c.pubkey = pubkeyShort

			if idsStrings[0] == c.host.ID().String() {
				taprootAddress := PubkeyToTapprootAddress(c.pubkey)

				payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendtoaddress\", \"params\": [\"" + taprootAddress + "\", 50]}"
				result := jsonRPC(payload)
				fmt.Println(result)
				if result == nil {
					// Should probably not panic here
					panic("Couldn't create first transaction")
				}

				c.ptxid = result["result"].(string)
			}

			// Save tweaked value
			merkleRoot := HashMerkleRoot(c.config.PublicKey, cp)
			c.tweakedValue = HashTweakedValue(c.config.PublicKey, merkleRoot)

			c.init = true
		}

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		/*
				NOT WORKING WITHOUT THE MOCKED POWER ACTOR

			oldAct, err := c.api.StateGetActor(ctx, mpoweractor.MpowerActorAddr, oldTs.Key())
			if err != nil {
				return false, nil, err
			}
			newAct, err := c.api.StateGetActor(ctx, mpoweractor.MpowerActorAddr, newTs.Key())
			if err != nil {
				return false, nil, err
			}
		*/

		// This is not actually what we want. Just here to check.
		oldTipset, err := c.api.ChainGetTipSet(ctx, oldTs.Key())
		if err != nil {
			return false, nil, err
		}

		// ZONDAX TODO:
		// If Power Actors list has changed start DKG

		// Only start when we have 3 peers
		if len(c.topic.ListPeers()) < 2 {
			return false, nil, nil
		}

		if c.genkey {
			fmt.Println(oldTipset.Height())
			// ZONDAX TODO
			// Activate checkpointing every 5 blocks
			if oldTipset.Height()%5 == 0 {
				fmt.Println("Check point time")

				if c.init && c.config != nil {
					fmt.Println("We have a taproot config")

					data := oldTipset.Cids()[0]

					c.CreateCheckpoint(ctx, data.Bytes())
				}

			}
			return false, nil, nil
		}

		c.genkey = true

		return true, nil, nil
	}

	err := c.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (c *CheckpointingSub) Start(ctx context.Context) {
	c.listenCheckpointEvents(ctx)

	topic, err := c.pubsub.Join("keygen")
	if err != nil {
		panic(err)
	}
	c.topic = topic

	// and subscribe to it
	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	c.sub = sub
}

func (c *CheckpointingSub) LoopHandler(ctx context.Context, h protocol.Handler, network *Network) {
	for {
		msg, ok := <-h.Listen()
		if !ok {
			network.Done()
			// the channel was closed, indicating that the protocol is done executing.
			fmt.Println("Should be good")
			return
		}
		network.Send(ctx, msg)

		for _, _ = range network.Parties() {
			msg = network.Next(ctx)
			h.Accept(msg)
		}
	}
}

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context, data []byte) {
	idsStrings := c.orderParticipantsList()
	fmt.Println("Participants list :", idsStrings)
	ids := c.formIDSlice(idsStrings)
	taprootAddress := PubkeyToTapprootAddress(c.pubkey)

	if c.ptxid == "" {
		taprootScript := GetTaprootScript(c.pubkey)
		success := AddTaprootScriptToWallet(taprootScript)
		if !success {
			panic("failed to add taproot address to wallet")
		}

		ptxid, err := WalletGetTxidFromAddress(taprootAddress)
		fmt.Println(taprootAddress)
		if err != nil {
			panic(err)
		}
		c.ptxid = ptxid
		fmt.Println("Found precedent txid:", c.ptxid)
	}

	//if idsStrings[0] == c.host.ID().String() {
	{
		// The first participant create the message to sign
		payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createrawtransaction\", \"params\": [[{\"txid\":\"" + c.ptxid + "\",\"vout\": 0, \"sequence\": 4294967295}], [{\"" + taprootAddress + "\": \"50\"}, {\"data\": \"636964\"}]]}"
		result := jsonRPC(payload)
		if result == nil {
			panic("cant create new transaction")
		}

		rawTransaction := result["result"].(string)

		payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"gettxout\", \"params\": [\"" + c.ptxid + "\", 0]}"

		result = jsonRPC(payload)
		if result == nil {
			panic("cant retrieve previous transaction")
		}
		taprootTxOut := result["result"].(map[string]interface{})
		scriptPubkey := taprootTxOut["scriptPubKey"].(map[string]interface{})
		scriptPubkeyBytes, _ := hex.DecodeString(scriptPubkey["hex"].(string))

		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(taprootTxOut["value"].(float64)*100000000))

		tx, err := hex.DecodeString(rawTransaction)
		if err != nil {
			panic(err)
		}
		utxo := append(buf[:], []byte{34}...)
		utxo = append(utxo, scriptPubkeyBytes...)

		hashedTx, err := TaprootSignatureHash(tx, utxo, 0x00)
		if err != nil {
			panic(err)
		}

		/*
		 * Orchestrate the signing message
		 */

		f := frost.SignTaprootWithTweak(c.config, ids, hashedTx[:], c.tweakedValue[:])
		n := NewNetwork(ids, c.sub, c.topic)
		handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})
		if err != nil {
			panic(err)
		}
		c.LoopHandler(ctx, handler, n)
		r, err := handler.Result()
		if err != nil {
			fmt.Println(err)
			log.Fatal("Not working neither")
		}
		fmt.Println("Result :", r)
	}

}

func (c *CheckpointingSub) orderParticipantsList() []string {
	id := c.host.ID().String()
	var ids []string

	ids = append(ids, id)

	for _, p := range c.topic.ListPeers() {
		ids = append(ids, p.String())
	}

	sort.Strings(ids)

	return ids
}

func (c *CheckpointingSub) formIDSlice(ids []string) party.IDSlice {
	var _ids []party.ID
	for _, p := range ids {
		_ids = append(_ids, party.ID(p))
	}

	idsSlice := party.NewIDSlice(_ids)

	return idsSlice
}

func BuildCheckpointingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, c *CheckpointingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)

	// Ping to see if bitcoind is available
	success := BitcoindPing()
	if !success {
		// Should probably not panic here
		panic("Bitcoin node not available")
	}

	fmt.Println("Successfully pinged bitcoind")

	c.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?
			return nil
		},
	})

}
