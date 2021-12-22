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
	"sync"

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
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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
	// lock
	lk sync.Mutex

	// Generated public key
	pubkey []byte
	// taproot config
	config *keygen.TaprootConfig
	// miners
	minerSigners []peer.ID
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
	// minio client
	minioClient *minio.Client
	// Bitcoin latest checkpoint
	latestConfigCheckpoint types.TipSetKey
	// Is synced
	synced bool
	// height verified!
	height abi.ChainEpoch
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

	fmt.Println("EUDICO PATH :", os.Getenv("EUDICO_PATH"))

	var ccfg config.FullNode
	result, err := config.FromFile(os.Getenv("EUDICO_PATH")+"/config.toml", &ccfg)
	if err != nil {
		return nil, err
	}

	cpconfig := result.(*config.FullNode).Checkpoint

	// initiate miners signers array
	var minerSigners []peer.ID

	synced := false
	var config *keygen.TaprootConfig
	// Load configTaproot
	if _, err := os.Stat(os.Getenv("EUDICO_PATH") + "/share.toml"); errors.Is(err, os.ErrNotExist) {
		// path/to/whatever does not exist
		fmt.Println("No share file saved")
	} else {
		synced = true
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

		for key, vshare := range configTOML.VerificationShares {

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

		for id := range config.VerificationShares {
			minerSigners = append(minerSigners, peer.ID(id))
		}

		fmt.Println(minerSigners)
	}

	// Initialize minio client object.
	minioClient, err := minio.New(cpconfig.MinioHost, &minio.Options{
		Creds:  credentials.NewStaticV4(cpconfig.MinioAccessKeyID, cpconfig.MinioSecretAccessKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	return &CheckpointingSub{
		pubsub:       pubsub,
		topic:        nil,
		sub:          nil,
		host:         host,
		api:          &api,
		events:       e,
		init:         false,
		ptxid:        "",
		config:       config,
		minerSigners: minerSigners,
		newconfig:    nil,
		cpconfig:     &cpconfig,
		minioClient:  minioClient,
		synced:       synced,
	}, nil
}

func (c *CheckpointingSub) listenCheckpointEvents(ctx context.Context) {

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for power actor")

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		c.lk.Lock()
		defer c.lk.Unlock()

		// verify we are synced
		st, err := c.api.SyncState(ctx)
		if err != nil {
			panic(err)
		}
		//fmt.Println(st.ActiveSyncs)

		if !c.synced {
			// Are we synced ?
			if len(st.ActiveSyncs) > 0 &&
				st.ActiveSyncs[len(st.ActiveSyncs)-1].Height == newTs.Height() {

				fmt.Println("We are synced")
				// Yes then verify our checkpoint
				ts, err := c.api.ChainGetTipSet(ctx, c.latestConfigCheckpoint)
				if err != nil {
					panic(err)
				}
				fmt.Println("We have a checkpoint up to height : ", ts.Height())
				c.synced = true
				c.height = ts.Height()
			} else {
				return false, nil, nil
			}
		}

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

		// Init with the first checkpoint for the demo
		// Should be done outside of it ?
		if !c.init && c.config != nil {
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

		// Activate checkpointing every 30 blocks
		fmt.Println("Height:", newTs.Height())
		// NOTES: this will only work in delegated consensus
		// Wait for more tipset to valid the height and be sure it is valid
		if newTs.Height()%25 == 0 && (c.config != nil || c.newconfig != nil) {
			fmt.Println("Check point time")

			// Initiation and config should be happening at start
			if c.init {
				cp := oldTs.Key().Bytes()

				// If we don't have a config we don't sign but update our config with key
				if c.config == nil {
					fmt.Println("We dont have a config")
					pubkey := c.newconfig.PublicKey

					pubkeyShort := GenCheckpointPublicKeyTaproot(pubkey, cp)

					c.config = c.newconfig
					merkleRoot := HashMerkleRoot(pubkey, cp)
					c.tweakedValue = HashTweakedValue(pubkey, merkleRoot)
					c.pubkey = pubkeyShort
					c.newconfig = nil

				} else {
					var config string = hex.EncodeToString(cp) + "\n"
					for _, partyId := range c.orderParticipantsList() {
						config += partyId + "\n"
					}

					hash, err := CreateConfig([]byte(config))
					if err != nil {
						panic(err)
					}

					// Push config to S3
					err = StoreConfig(ctx, c.minioClient, c.cpconfig.MinioBucketName, hex.EncodeToString(hash))
					if err != nil {
						panic(err)
					}

					c.CreateCheckpoint(ctx, cp, hash)
				}

			}
		}

		// If Power Actors list has changed start DKG
		// Changes detected so generate new key
		if oldSt.MinerCount != newSt.MinerCount {
			fmt.Println("Generate new config")

			c.GenerateNewKeys(ctx)

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

func (c *CheckpointingSub) GenerateNewKeys(ctx context.Context) {

	idsStrings := c.newOrderParticipantsList()

	fmt.Println("Participants list :", idsStrings)

	ids := c.formIDSlice(idsStrings)

	id := party.ID(c.host.ID().String())

	threshold := (len(idsStrings) / 2) + 1
	n := NewNetwork(ids, c.sub, c.topic)
	f := frost.KeygenTaproot(id, ids, threshold)

	handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})
	if err != nil {
		panic(err)
	}
	LoopHandler(ctx, handler, n)
	r, err := handler.Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Result :", r)

	c.newconfig = r.(*keygen.TaprootConfig)
}

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context, cp, data []byte) {

	taprootAddress, err := PubkeyToTapprootAddress(c.pubkey)
	if err != nil {
		panic(err)
	}

	pubkey := c.config.PublicKey
	if c.newconfig != nil {
		pubkey = c.newconfig.PublicKey
	}

	pubkeyShort := GenCheckpointPublicKeyTaproot(pubkey, cp)
	newTaprootAddress, err := PubkeyToTapprootAddress(pubkeyShort)
	if err != nil {
		panic(err)
	}

	idsStrings := c.orderParticipantsList()
	fmt.Println("Participants list :", idsStrings)
	fmt.Println("Precedent tx", c.ptxid)
	ids := c.formIDSlice(idsStrings)

	if c.ptxid == "" {
		fmt.Println("Missing precedent txid")
		taprootScript := GetTaprootScript(c.pubkey)
		success := AddTaprootToWallet(c.cpconfig.BitcoinHost, taprootScript)
		if !success {
			panic("failed to add taproot address to wallet")
		}

		ptxid, err := WalletGetTxidFromAddress(c.cpconfig.BitcoinHost, taprootAddress)
		if err != nil {
			panic(err)
		}
		c.ptxid = ptxid
		fmt.Println("Found precedent txid:", c.ptxid)
	}

	index := 0
	value, scriptPubkeyBytes := GetTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)

	if scriptPubkeyBytes[0] != 0x51 {
		fmt.Println("Wrong txout")
		index = 1
		value, scriptPubkeyBytes = GetTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)
	}
	newValue := value - c.cpconfig.Fee

	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createrawtransaction\", \"params\": [[{\"txid\":\"" + c.ptxid + "\",\"vout\": " + strconv.Itoa(index) + ", \"sequence\": 4294967295}], [{\"" + newTaprootAddress + "\": \"" + fmt.Sprintf("%.2f", newValue) + "\"}, {\"data\": \"" + hex.EncodeToString(data) + "\"}]]}"
	result := jsonRPC(c.cpconfig.BitcoinHost, payload)
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

	fmt.Println("Starting signing")
	f := frost.SignTaprootWithTweak(c.config, ids, hashedTx[:], c.tweakedValue[:])
	n := NewNetwork(ids, c.sub, c.topic)
	handler, err := protocol.NewMultiHandler(f, hashedTx[:])
	if err != nil {
		panic(err)
	}
	LoopHandler(ctx, handler, n)
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
		result = jsonRPC(c.cpconfig.BitcoinHost, payload)
		if result["error"] != nil {
			fmt.Println(result)
			panic("failed to broadcast transaction")
		}

		/* Need to keep this to build next one */
		newtxid := result["result"].(string)
		fmt.Println("New Txid:", newtxid)
		c.ptxid = newtxid
	}
}

func (c *CheckpointingSub) newOrderParticipantsList() []string {
	id := c.host.ID().String()
	var ids []string

	ids = append(ids, id)

	for _, peerID := range c.topic.ListPeers() {
		ids = append(ids, peerID.String())
	}

	sort.Strings(ids)

	return ids
}

func (c *CheckpointingSub) orderParticipantsList() []string {
	var ids []string
	for id := range c.config.VerificationShares {
		ids = append(ids, string(id))
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

// Temporary
func (c *CheckpointingSub) prefundTaproot() error {
	taprootAddress, err := PubkeyToTapprootAddress(c.pubkey)
	if err != nil {
		return err
	}

	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendtoaddress\", \"params\": [\"" + taprootAddress + "\", 50]}"
	result := jsonRPC(c.cpconfig.BitcoinHost, payload)
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
	success := BitcoindPing(c.cpconfig.BitcoinHost)
	if !success {
		// Should probably not panic here
		panic("Bitcoin node not available")
	}

	fmt.Println("Successfully pinged bitcoind")

	LoadWallet(c.cpconfig.BitcoinHost)

	// Get first checkpoint from block 0
	ts, err := c.api.ChainGetGenesis(ctx)
	if err != nil {
		panic(err)
	}
	cidBytes := ts.Key().Bytes()
	publickey, err := hex.DecodeString(c.cpconfig.PublicKey)
	if err != nil {
		panic(err)
	}

	btccp, err := GetLatestCheckpoint(c.cpconfig.BitcoinHost, publickey, cidBytes)
	if err != nil {
		panic(err)
	}

	cp, err := GetConfig(ctx, c.minioClient, c.cpconfig.MinioBucketName, btccp.cid)

	fmt.Println(cp)
	if cp != "" {
		cpBytes, err := hex.DecodeString(cp)
		if err != nil {
			panic(err)
		}
		c.latestConfigCheckpoint, err = types.TipSetKeyFromBytes(cpBytes)
		if err != nil {
			panic(err)
		}

		// The first tx is in Bitcoin
		c.init = true
	}

	c.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?
			return nil
		},
	})

}
