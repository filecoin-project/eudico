package checkpointing

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

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
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
)

var log = logging.Logger("checkpointing")

/*
	Main file for the checkpointing module. Handle all the core logic.
*/

type CheckpointingSub struct {

	/*
		Eudico passed value (network, eudico api for state, events)
	*/

	// libp2p host value
	host host.Host
	// libp2p pubsub instance
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

	/*
		Checkpointing module state
	*/
	// Generated public key
	pubkey []byte
	// Participants list identified with their libp2p cid
	participants []string
	// taproot config
	config *keygen.TaprootConfig
	// new config generated
	newconfig *keygen.TaprootConfig
	// Previous tx
	ptxid string
	// Tweaked value
	tweakedValue []byte
	// Checkpoint section in config.toml
	cpconfig *config.Checkpoint
	// minio client
	minioClient *minio.Client
	// Bitcoin latest checkpoint used when syncing
	latestConfigCheckpoint types.TipSetKey
	// Is Eudico synced (do we have all the blocks)
	synced bool
	// height verified! (the height of the latest checkpoint)
	height abi.ChainEpoch
}

/*
	Initiate checkpoint module
	It will load config and inititate CheckpointingSub struct
*/
func NewCheckpointSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	host host.Host,
	pubsub *pubsub.PubSub,
	api impl.FullNodeAPI,
) (*CheckpointingSub, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)
	// Starting checkpoint listener
	e, err := events.NewEvents(ctx, &api)
	if err != nil {
		return nil, err
	}

	// Load config from EUDICO_PATH environnement
	var ccfg config.FullNode
	result, err := config.FromFile(os.Getenv("EUDICO_PATH")+"/config.toml", &ccfg)
	if err != nil {
		return nil, err
	}

	cpconfig := result.(*config.FullNode).Checkpoint

	// initiate miners signers array
	var minerSigners []string

	synced := false

	// Load taproot verification shares from EUDICO_PATH environnement if file exist
	var config *keygen.TaprootConfig
	_, err = os.Stat(os.Getenv("EUDICO_PATH") + "/share.toml")
	if err == nil {
		// If we have a share.toml containing the distributed key we load them
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

		// Decode the hex value to byte
		privateSharePath, err := hex.DecodeString(configTOML.PrivateShare)
		if err != nil {
			return nil, err
		}

		// Decode the hex value to byte
		publickey, err := hex.DecodeString(configTOML.PublicKey)
		if err != nil {
			return nil, err
		}

		// Unmarshall to Secp256k1Scalar
		var privateShare curve.Secp256k1Scalar
		err = privateShare.UnmarshalBinary(privateSharePath)
		if err != nil {
			return nil, err
		}

		verificationShares := make(map[party.ID]*curve.Secp256k1Point)

		for key, vshare := range configTOML.VerificationShares {
			// Decode each verification share to byte for each participants

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
			minerSigners = append(minerSigners, string(id))
		}
	}

	// Initialize minio client object
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
		ptxid:        "",
		config:       config,
		participants: minerSigners,
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
			log.Errorf("unable to sync: %v", err)
			return false, nil, err
		}

		if !c.synced {
			// Are we synced ?
			if len(st.ActiveSyncs) > 0 &&
				st.ActiveSyncs[len(st.ActiveSyncs)-1].Height == newTs.Height() {

				log.Infow("we are synced")
				// Yes then verify our checkpoint
				ts, err := c.api.ChainGetTipSet(ctx, c.latestConfigCheckpoint)
				if err != nil {
					log.Errorf("couldnt get tipset: %v", err)
					return false, nil, err

				}
				log.Infow("We have a checkpoint up to height : ", ts.Height())
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

		// Activate checkpointing every 30 blocks
		log.Infow("Height:", newTs.Height())
		// NOTES: this will only work in delegated consensus
		// Wait for more tipset to valid the height and be sure it is valid
		if newTs.Height()%25 == 0 && (c.config != nil || c.newconfig != nil) {
			log.Infow("Check point time")

			// Initiation and config should be happening at start
			cp := oldTs.Key().Bytes()

			// If we don't have a config we don't sign but update our config with key
			if c.config == nil {
				log.Infow("We dont have a config")
				pubkey := c.newconfig.PublicKey

				pubkeyShort := genCheckpointPublicKeyTaproot(pubkey, cp)

				c.config = c.newconfig
				merkleRoot := hashMerkleRoot(pubkey, cp)
				c.tweakedValue = hashTweakedValue(pubkey, merkleRoot)
				c.pubkey = pubkeyShort
				c.newconfig = nil

			} else {
				var config string = hex.EncodeToString(cp) + "\n"
				for _, partyId := range c.orderParticipantsList() {
					config += partyId + "\n"
				}

				hash, err := CreateConfig([]byte(config))
				if err != nil {
					log.Errorf("couldnt create config: %v", err)
					return false, nil, err
				}

				// Push config to S3
				err = StoreConfig(ctx, c.minioClient, c.cpconfig.MinioBucketName, hex.EncodeToString(hash))
				if err != nil {
					log.Errorf("couldnt push config: %v", err)
					return false, nil, err
				}

				err = c.CreateCheckpoint(ctx, cp, hash)
				if err != nil {
					log.Errorf("couldnt create checkpoint: %v", err)
					return false, nil, err
				}
			}
		}

		// If Power Actors list has changed start DKG
		// Changes detected so generate new key
		if oldSt.MinerCount != newSt.MinerCount {
			log.Infow("generate new config")
			err := c.GenerateNewKeys(ctx, newSt.Miners)
			if err != nil {
				log.Errorf("error while generating new key: %v", err)
				// If generating new key failed, checkpointing should not be possible
			}

			return true, nil, nil
		}

		return false, nil, nil
	}

	err := c.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (c *CheckpointingSub) Start(ctx context.Context) error {

	/*
		Join libp2p pubsub topic dedicated to key generation or checkpoint generation
	*/

	topic, err := c.pubsub.Join("keygen")
	if err != nil {
		return err
	}
	c.topic = topic

	// and subscribe to it
	// INCREASE THE BUFFER SIZE BECAUSE IT IS ONLY 32 ! AND IT IS DROPPING MESSAGES WHEN FULL
	// https://github.com/libp2p/go-libp2p-pubsub/blob/v0.5.4/pubsub.go#L1222
	// NOTES: 1000 has been choosen arbitraly there is no reason for this number beside it just work
	sub, err := topic.Subscribe(pubsub.WithBufferSize(1000))
	if err != nil {
		return err
	}
	c.sub = sub

	c.listenCheckpointEvents(ctx)

	return nil
}

func (c *CheckpointingSub) GenerateNewKeys(ctx context.Context, participants []string) error {

	//idsStrings := c.newOrderParticipantsList()
	idsStrings := participants
	sort.Strings(idsStrings)

	log.Infow("participants list :", idsStrings)

	ids := c.formIDSlice(idsStrings)

	id := party.ID(c.host.ID().String())

	threshold := (len(idsStrings) / 2) + 1
	n := NewNetwork(c.sub, c.topic)
	f := frost.KeygenTaproot(id, ids, threshold)

	handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})
	if err != nil {
		return err
	}
	LoopHandler(ctx, handler, n)
	r, err := handler.Result()
	if err != nil {
		return err
	}
	log.Infow("result :", r)

	var ok bool
	c.newconfig, ok = r.(*keygen.TaprootConfig)
	if !ok {
		return xerrors.Errorf("state change propagated is the wrong type")
	}

	c.participants = participants

	return nil
}

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context, cp, data []byte) error {
	taprootAddress, err := pubkeyToTapprootAddress(c.pubkey)
	if err != nil {
		return err
	}

	pubkey := c.config.PublicKey
	if c.newconfig != nil {
		pubkey = c.newconfig.PublicKey
	}

	pubkeyShort := genCheckpointPublicKeyTaproot(pubkey, cp)
	newTaprootAddress, err := pubkeyToTapprootAddress(pubkeyShort)
	if err != nil {
		return err
	}

	idsStrings := c.orderParticipantsList()
	log.Infow("participants list :", idsStrings)
	log.Infow("precedent tx", c.ptxid)
	ids := c.formIDSlice(idsStrings)

	if c.ptxid == "" {
		log.Infow("missing precedent txid")
		taprootScript := getTaprootScript(c.pubkey)
		success := addTaprootToWallet(c.cpconfig.BitcoinHost, taprootScript)
		if !success {
			return xerrors.Errorf("failed to add taproot address to wallet")
		}

		// sleep an arbitrary long time to be sure it has been scanned
		time.Sleep(6 * time.Second)

		ptxid, err := walletGetTxidFromAddress(c.cpconfig.BitcoinHost, taprootAddress)
		if err != nil {
			return err
		}
		c.ptxid = ptxid
		log.Infow("found precedent txid:", c.ptxid)
	}

	index := 0
	value, scriptPubkeyBytes := getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)

	if scriptPubkeyBytes[0] != 0x51 {
		log.Infow("wrong txout")
		index = 1
		value, scriptPubkeyBytes = getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)
	}
	newValue := value - c.cpconfig.Fee

	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createrawtransaction\", \"params\": [[{\"txid\":\"" + c.ptxid + "\",\"vout\": " + strconv.Itoa(index) + ", \"sequence\": 4294967295}], [{\"" + newTaprootAddress + "\": \"" + fmt.Sprintf("%.2f", newValue) + "\"}, {\"data\": \"" + hex.EncodeToString(data) + "\"}]]}"
	result := jsonRPC(c.cpconfig.BitcoinHost, payload)
	if result == nil {
		return xerrors.Errorf("cant create new transaction")
	}

	rawTransaction := result["result"].(string)

	tx, err := hex.DecodeString(rawTransaction)
	if err != nil {
		return err
	}

	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(value*100000000))
	utxo := append(buf[:], []byte{34}...)
	utxo = append(utxo, scriptPubkeyBytes...)

	hashedTx, err := TaprootSignatureHash(tx, utxo, 0x00)
	if err != nil {
		return err
	}

	/*
	 * Orchestrate the signing message
	 */

	log.Infow("starting signing")
	f := frost.SignTaprootWithTweak(c.config, ids, hashedTx[:], c.tweakedValue[:])
	n := NewNetwork(c.sub, c.topic)
	handler, err := protocol.NewMultiHandler(f, hashedTx[:])
	if err != nil {
		return err
	}
	LoopHandler(ctx, handler, n)
	r, err := handler.Result()
	if err != nil {
		return err
	}
	log.Infow("result :", r)

	// if signing is a success we register the new value
	merkleRoot := hashMerkleRoot(pubkey, cp)
	c.tweakedValue = hashTweakedValue(pubkey, merkleRoot)
	c.pubkey = pubkeyShort
	// If new config used
	if c.newconfig != nil {
		c.config = c.newconfig
		c.newconfig = nil
	}

	c.ptxid = ""

	// Only first one broadcast the transaction ?
	// Actually all participants can broadcast the transcation. It will be the same everywhere.
	rawtx := prepareWitnessRawTransaction(rawTransaction, r.(taproot.Signature))

	payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendrawtransaction\", \"params\": [\"" + rawtx + "\"]}"
	result = jsonRPC(c.cpconfig.BitcoinHost, payload)
	if result["error"] != nil {
		return xerrors.Errorf("failed to broadcast transaction")
	}

	/* Need to keep this to build next one */
	newtxid := result["result"].(string)
	log.Infow("new Txid:", newtxid)
	c.ptxid = newtxid

	return nil
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

/*
	BuildCheckpointingSub is called after creating the checkpointing instance
	It verify connectivity with the Bitcoin node and look for the first checkpoint
	and if the node is a **participant** will pre-compute some values used in signing
*/
func BuildCheckpointingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, c *CheckpointingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)

	// Ping to see if bitcoind is available
	success := bitcoindPing(c.cpconfig.BitcoinHost)
	if !success {
		log.Errorf("bitcoin node not available")
		return
	}

	log.Infow("successfully pinged bitcoind")

	// Get first checkpoint from block 0
	ts, err := c.api.ChainGetGenesis(ctx)
	if err != nil {
		log.Errorf("couldnt get genesis tipset: %v", err)
		return
	}
	cidBytes := ts.Key().Bytes()
	publickey, err := hex.DecodeString(c.cpconfig.PublicKey)
	if err != nil {
		log.Errorf("couldnt decode public key: %v", err)
		return
	}

	// Get the last checkpoint from the bitcoin node
	btccp, err := GetLatestCheckpoint(c.cpconfig.BitcoinHost, publickey, cidBytes)
	if err != nil {
		log.Errorf("couldnt decode public key: %v", err)
		return
	}

	// Get the config in minio using the last checkpoint found through bitcoin
	// NOTES: We should be able to get the config regarless of storage (minio, IPFS, KVS,....)
	cp, err := GetConfig(ctx, c.minioClient, c.cpconfig.MinioBucketName, btccp.cid)

	if cp != "" {
		// Decode hex checkpoint to bytes
		cpBytes, err := hex.DecodeString(cp)
		if err != nil {
			log.Errorf("couldnt decode checkpoint: %v", err)
			return
		}
		// Cache latest checkpoint value for when we sync and compare wit Eudico key tipset values
		c.latestConfigCheckpoint, err = types.TipSetKeyFromBytes(cpBytes)
		if err != nil {
			log.Errorf("couldnt get tipset key from checkpoint: %v", err)
			return
		}
	}

	// Pre-compute values from participants in the signing process
	if c.config != nil {
		// save public key taproot
		// NOTES: cidBytes is the tipset key value (aka checkpoint) from the genesis block. When Eudico is stopped it should remember what was the last tipset key value
		// it signed and replace it with it. Config is not saved, neither when new DKG is done.
		c.pubkey = genCheckpointPublicKeyTaproot(c.config.PublicKey, cidBytes)

		//address, _ := pubkeyToTapprootAddress(c.pubkey)
		//fmt.Println(address)

		// Save tweaked value
		merkleRoot := hashMerkleRoot(c.config.PublicKey, cidBytes)
		c.tweakedValue = hashTweakedValue(c.config.PublicKey, merkleRoot)
	}

	// Start the checkpoint module
	err = c.Start(ctx)
	if err != nil {
		log.Errorf("couldn't start checkpointing module: %v", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?

			// NOTES: new config and checkpoint should be saved in a file for when we restart the node

			return nil
		},
	})

}
