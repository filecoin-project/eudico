package checkpointing

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/BurntSushi/toml"
	"github.com/Zondax/multi-party-sig/pkg/math/curve"
	"github.com/Zondax/multi-party-sig/pkg/party"
	"github.com/Zondax/multi-party-sig/pkg/protocol"
	"github.com/Zondax/multi-party-sig/pkg/taproot"
	"github.com/Zondax/multi-party-sig/protocols/frost"
	"github.com/Zondax/multi-party-sig/protocols/frost/keygen"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/actors/mpower"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"github.com/filecoin-project/lotus/node/config"
	cbor "github.com/ipfs/go-ipld-cbor"
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
	// new config generated
	newconfig *keygen.TaprootConfig
	// Initiated
	init bool
	// Previous tx
	ptxid string
	// Tweaked value
	tweakedValue []byte
	// minio config
	cpconfig *config.Checkpoint
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

	var ccfg config.FullNode
	result, err := config.FromFile(os.Getenv("EUDICO_PATH")+"/config.toml", &ccfg)
	if err != nil {
		return nil, err
	}

	cpconfig := result.(*config.FullNode).Checkpoint

	var config *keygen.TaprootConfig
	// Load configTaproot
	if _, err := os.Stat(os.Getenv("EUDICO_PATH") + "/share.toml"); errors.Is(err, os.ErrNotExist) {
		// path/to/whatever does not exist
		fmt.Println("No share file saved")
	} else {
		content, err := os.ReadFile(os.Getenv("EUDICO_PATH") + "/share.toml")
		if err != nil {
			return nil, err
		}

		var configTOML TaprootConfigTOML
		_, err = toml.Decode(string(content), &configTOML)
		if err != nil {
			return nil, err
		}

		privateSharePath, err := hex.DecodeString(configTOML.PrivateShare)
		if err != nil {
			return nil, err
		}

		publickey, err := hex.DecodeString(configTOML.PublicKey)
		if err != nil {
			return nil, err
		}

		var privateShare curve.Secp256k1Scalar
		err = privateShare.UnmarshalBinary(privateSharePath)
		if err != nil {
			return nil, err
		}

		verificationShares := make(map[party.ID]*curve.Secp256k1Point)

		fmt.Println(configTOML.VerificationShares)

		for key, vshare := range configTOML.VerificationShares {

			fmt.Println(key)
			fmt.Println(vshare)

			var p curve.Secp256k1Point
			pByte, err := hex.DecodeString(vshare.Share)
			if err != nil {
				return nil, err
			}
			err = p.UnmarshalBinary(pByte)
			if err != nil {
				return nil, err
			}
			verificationShares[party.ID(key)] = &p
		}

		config = &keygen.TaprootConfig{
			ID:                 party.ID(host.ID().String()),
			Threshold:          configTOML.Thershold,
			PrivateShare:       &privateShare,
			PublicKey:          publickey,
			VerificationShares: verificationShares,
		}
	}

	return &CheckpointingSub{
		pubsub:    pubsub,
		topic:     nil,
		sub:       nil,
		host:      host,
		api:       &api,
		events:    e,
		init:      false,
		ptxid:     "",
		config:    config,
		newconfig: nil,
		cpconfig:  &cpconfig,
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
		if err != nil {
			fmt.Println(err)
			panic(err)
		}
		c.LoopHandler(ctx, handler, n)
		r, err := handler.Result()
		if err != nil {
			fmt.Println(err)
			panic(err)
		}
		fmt.Println("Result :", r)

		c.newconfig = r.(*keygen.TaprootConfig)

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		newAct, err := c.api.StateGetActor(ctx, mpower.PowerActorAddr, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		oldAct, err := c.api.StateGetActor(ctx, mpower.PowerActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}

		var oldSt, newSt mpower.State

		bs := blockstore.NewAPIBlockstore(c.api)
		cst := cbor.NewCborStore(bs)
		if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
			return false, nil, err
		}
		if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
			return false, nil, err
		}

		// If Power Actors list has changed start DKG
		if !c.init {
			ts, err := c.api.ChainGetTipSetByHeight(ctx, 0, oldTs.Key())
			if err != nil {
				panic(err)
			}
			data := ts.Cids()[0]
			err = c.initiate(data.Bytes())
			if err != nil {
				panic(err)
			}
			return false, nil, nil
		}

		// ZONDAX TODO
		// Activate checkpointing every 20 blocks
		fmt.Println("Height:", newTs.Height())
		if newTs.Height()%20 == 0 {
			fmt.Println("Check point time")

			// Initiation and config should be happening at start
			if c.init && c.config != nil {

				data := oldTs.Cids()[0]

				var partyList string = ""
				for _, partyId := range c.orderParticipantsList() {
					partyList += partyId + "\n"
				}
				hash, err := CreateConfig([]byte(partyList))
				if err != nil {
					panic(err)
				}

				// Push config to S3
				err = StoreConfig(ctx, c.cpconfig.MinioHost, c.cpconfig.MinioAccessKeyID, c.cpconfig.MinioSecretAccessKey, c.cpconfig.MinioBucketName ,hex.EncodeToString(hash))
				if err != nil {
					panic(err)
				}

				c.CreateCheckpoint(ctx, data.Bytes(), hash)
			}
		}

		// Changes detected so generate new key
		if oldSt.MinerCount != newSt.MinerCount {
			fmt.Println("Generate new config")

			return true, nil, nil
		}

		return false, nil, nil
	}

	err := c.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (c *CheckpointingSub) Start(ctx context.Context) {
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

	c.listenCheckpointEvents(ctx)
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

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context, cp, data []byte) {
	idsStrings := c.orderParticipantsList()
	fmt.Println("Participants list :", idsStrings)
	fmt.Println("Precedent tx", c.ptxid)
	ids := c.formIDSlice(idsStrings)
	taprootAddress := PubkeyToTapprootAddress(c.pubkey)

	pubkey := c.config.PublicKey
	if c.newconfig != nil {
		pubkey = c.newconfig.PublicKey
	}

	pubkeyShort := GenCheckpointPublicKeyTaproot(pubkey, cp)
	newTaprootAddress := PubkeyToTapprootAddress(pubkeyShort)

	if c.ptxid == "" {
		fmt.Println("Missing precedent txid")
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

	index := 0
	value, scriptPubkeyBytes := GetTxOut(c.ptxid, index)

	if scriptPubkeyBytes[0] != 0x51 {
		fmt.Println("Wrong txout")
		index = 1
		value, scriptPubkeyBytes = GetTxOut(c.ptxid, index)
	}
	newValue := value - c.cpconfig.Fee

	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createrawtransaction\", \"params\": [[{\"txid\":\"" + c.ptxid + "\",\"vout\": " + strconv.Itoa(index) + ", \"sequence\": 4294967295}], [{\"" + newTaprootAddress + "\": \"" + fmt.Sprintf("%.2f", newValue) + "\"}, {\"data\": \"" + hex.EncodeToString(data) + "\"}]]}"
	result := jsonRPC(payload)
	fmt.Println(result)
	if result == nil {
		panic("cant create new transaction")
	}

	rawTransaction := result["result"].(string)

	tx, err := hex.DecodeString(rawTransaction)
	if err != nil {
		panic(err)
	}

	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(value*100000000))
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
		panic(err)
	}
	fmt.Println("Result :", r)

	// if signing is a success we register the new value
	merkleRoot := HashMerkleRoot(pubkey, cp)
	c.tweakedValue = HashTweakedValue(pubkey, merkleRoot)
	c.pubkey = pubkeyShort
	// If new config used
	if c.newconfig != nil {
		c.config = c.newconfig
		c.newconfig = nil
	}

	c.ptxid = ""

	if idsStrings[0] == c.host.ID().String() {
		// Only first one broadcast the transaction ?
		// Actually all participants can broadcast the transcation. It will be the same everywhere.
		rawtx := PrepareWitnessRawTransaction(rawTransaction, r.(taproot.Signature))

		payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendrawtransaction\", \"params\": [\"" + rawtx + "\"]}"
		result = jsonRPC(payload)
		if result["error"] != nil {
			fmt.Println(result)
			panic("failed to broadcast transaction")
		}

		fmt.Println(result)

		/* Need to keep this to build next one */
		newtxid := result["result"].(string)
		fmt.Println("New Txid:", newtxid)
		c.ptxid = newtxid
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

func (c *CheckpointingSub) prefundTaproot() error {
	taprootAddress := PubkeyToTapprootAddress(c.pubkey)

	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendtoaddress\", \"params\": [\"" + taprootAddress + "\", 50]}"
	result := jsonRPC(payload)
	fmt.Println(result)
	if result == nil {
		// Should probably not panic here
		return errors.New("couldn't create first transaction")
	}
	c.ptxid = result["result"].(string)

	return nil
}

func (c *CheckpointingSub) initiate(cp []byte) error {
	pubkeyShort := GenCheckpointPublicKeyTaproot(c.config.PublicKey, cp)
	c.pubkey = pubkeyShort

	idsStrings := c.orderParticipantsList()

	if idsStrings[0] == c.host.ID().String() {
		err := c.prefundTaproot()
		if err != nil {
			return err
		}
	}

	// Save tweaked value
	merkleRoot := HashMerkleRoot(c.config.PublicKey, cp)
	c.tweakedValue = HashTweakedValue(c.config.PublicKey, merkleRoot)

	c.init = true

	return nil
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
