package checkpointing

import (
	"context"
	"encoding/binary"
	"encoding/hex"

	"encoding/json"
	"fmt"
	"os"
	"bytes"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	//"github.com/libp2p/go-libp2p"
	//datastore "github.com/ipfs/go-datastore"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/BurntSushi/toml"
	"github.com/sa8/multi-party-sig/pkg/math/curve"
	"github.com/sa8/multi-party-sig/pkg/party"
	"github.com/sa8/multi-party-sig/pkg/protocol"
	"github.com/sa8/multi-party-sig/pkg/taproot"
	"github.com/sa8/multi-party-sig/protocols/frost"
	//"github.com/sa8/multi-party-sig/protocols/frost/keygen"
	"github.com/sa8/multi-party-sig/protocols/frost/keygen_gennaro"
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/consensus/actors/mpower"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	//peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
	// act "github.com/filecoin-project/lotus/chain/consensus/actors"
	// init_ "github.com/filecoin-project/specs-actors/actors/builtin/init"
	// "github.com/filecoin-project/lotus/build"
	// "github.com/filecoin-project/lotus/api"
)

var log = logging.Logger("checkpointing")


//update this value with the amount you want to send to the initial aggregated key (for testing purpose)
const initialValueInWallet = 50
// for testnet I recommend using 0.002
//const initialValueInWallet = 0.002

// change this to true to alternatively send all the amount from our wallet
var sendall = false

// this variable is the number of blocks (in eudico) we want between each checkpoints
const checkpointFrequency = 100

//change to true if regtest is used
const Regtest = true

// struct used to propagate detected changes.
type diffInfo struct {
	newMiners    []string
	newPublicKey []byte
	hash         []byte
	cp           []byte
}

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
	//Keys of the new set of participants
	newKey []byte
	// Participants list identified with their libp2p cid
	participants []string
	// Participants list identified with their libp2p cid
	newParticipants []string
	// boolean to keep track of when the new config has finished the DKG
	newDKGComplete bool
	// boolean to keep
	keysUpdated bool
	// taproot config
	taprootConfig *keygen_gennaro.TaprootConfig
	// new config generated
	newTaprootConfig *keygen_gennaro.TaprootConfig
	// Previous tx
	ptxid string
	// Tweaked value
	tweakedValue []byte
	// Checkpoint section in config.toml
	cpconfig *config.Checkpoint
	// minio client -> using KVS now
	//minioClient *minio.Client
	// Bitcoin latest checkpoint used when syncing
	latestConfigCheckpoint types.TipSetKey
	// Is Eudico synced (do we have all the blocks)
	synced bool
	// height verified! (the height of the latest checkpoint)
	height abi.ChainEpoch
	// last cid pushed to bitcoin
	lastCid string
	// keeps track of the amount left in the last UTXO
	amount float64
	// keeps track of last script
	scriptPubkeyBytes []byte


	// KVS
	r *Resolver
	ds  dtypes.MetadataDS

	// file to write measurements
	file *os.File
}

/*
	Initiate checkpoint module.
	It will load config and initiate CheckpointingSub struct
	using some pre-generated data. (TODO: change this and re-generate
		the data locally each time)
*/
func NewCheckpointSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	host host.Host,
	pubsub *pubsub.PubSub,
	api impl.FullNodeAPI,
	ds dtypes.MetadataDS) (*CheckpointingSub, error) {

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
	var taprootConfig *keygen_gennaro.TaprootConfig
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

		taprootConfig = &keygen_gennaro.TaprootConfig{
			ID:                 party.ID(host.ID().String()),
			Threshold:          configTOML.Threshold,
			PrivateShare:       &privateShare,
			PublicKey:          publickey,
			VerificationShares: verificationShares,
		}

		// this is where we append the original list of signers
		// note: they are not added in the mocked power actor (they probably should? TODO)
		for id := range taprootConfig.VerificationShares {
			minerSigners = append(minerSigners, string(id))
		}

	}


	return &CheckpointingSub{
		pubsub:           pubsub,
		topic:            nil,
		sub:              nil,
		host:             host,
		api:              &api,
		events:           e,
		pubkey:           make([]byte, 0),
		ptxid:            "",
		taprootConfig:    taprootConfig, //either nil (if no shares) or the configuration pre-generated for Alice, Bob and Charlie
		participants:     minerSigners,
		newDKGComplete:   false,
		keysUpdated:      true,
		newTaprootConfig: nil,
		cpconfig:         &cpconfig,
		synced:           synced,
		// r:				  r,
		ds: 			  ds,
	}, nil
}

func (c *CheckpointingSub) listenCheckpointEvents(ctx context.Context) {

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {

		// Verify if we are sync here (maybe ?)
		// if not sync return done = true and more = false

		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for mocked power actor")
		diff, ok := states.(*diffInfo)
		if !ok {
			log.Error("Error casting states, not of type *diffInfo")
			return true, err
		}
		//return true, nil
		return c.triggerChange(ctx, diff)
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		c.lk.Lock()
		defer c.lk.Unlock()

		diff := &diffInfo{}

		// verify we are synced
		// Maybe move it to checkFunc
		st, err := c.api.SyncState(ctx)
		if err != nil {
			log.Errorf("unable to sync: %v", err)
			return false, nil, err
		}

		if !c.synced {
			// Are we synced ?
			// Replace this WaitForSync logic with this function
			// https://github.com/Zondax/eudico/blob/1de9d0f773e49b61cd405add93c3c28c9f74cb38/node/modules/services.go#L104
			if len(st.ActiveSyncs) > 0 &&
				st.ActiveSyncs[len(st.ActiveSyncs)-1].Height == newTs.Height() {

				log.Infow("we are synced")
				// Yes then verify our checkpoint from Bitcoin and verify if we find in it in our Eudico chain
				
				//first we fetch the checkpoint from the KVS using the cid found from bitcoin
				cid := c.lastCid
				fmt.Println("try pull with cid: ", cid)
				ctx1, _ := context.WithTimeout(ctx, 1000 * time.Second)
				out := c.r.WaitCheckpointResolved(ctx1, cid)
				select {
					case <-ctx1.Done():
						 log.Errorf("context timeout")
					case err := <-out:
						if err != nil {
							log.Errorf("error fully resolving messages: %s", err)
						}
				}
				data, found, err := c.r.ResolveCheckpointMsgs(ctx, cid)
				if err != nil {
					log.Errorf("Error resolving messages: %v", err)
				}
				// sanity-check, it should always be found
				if !found {
					log.Errorf("messages haven't been resolved: %v", err)
				}
				fmt.Println("Data pulled from KVS: ", data)
				if len(data) >0 {
					//extract the cid from the data
					cpCid := strings.Split(string(data), "\n")[0]
					// Decode hex checkpoint to bytes
					cpBytes, err := hex.DecodeString(cpCid)
					if err != nil {
						log.Errorf("could not decode checkpoint: %v", err)
					}
					c.latestConfigCheckpoint, err = types.TipSetKeyFromBytes(cpBytes)
					if err != nil {
						log.Errorf("could not get tipset key from checkpoint: %v", err)
					}
				}
				ts, err := c.api.ChainGetTipSet(ctx, c.latestConfigCheckpoint)
				if err != nil {
					log.Errorf("couldnt get tipset: %v", err)
					return false, nil, err

				}
				log.Infow("We have a checkpoint up to height : ", "height", ts.Height())
				c.synced = true
				c.height = ts.Height()
			} else {
				return false, nil, nil
			}
		}
		// Get actors at specified tipset
		newAct, err := c.api.StateGetActor(ctx, mpower.PowerActorAddr, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		oldAct, err := c.api.StateGetActor(ctx, mpower.PowerActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}

		// Get state from specified actors
		var oldSt, newSt mpower.State
		bs := blockstore.NewAPIBlockstore(c.api)
		cst := cbor.NewCborStore(bs)
		if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
			return false, nil, err
		}
		if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
			return false, nil, err
		}

		log.Infow("Height:", "height", newTs.Height().String())
		fmt.Println("Height:", newTs.Height())
		// fmt.Println("Old address: ", oldSt.PublicKey)
		// fmt.Println("New address: ", newSt.PublicKey)


		// check if there is a new configuration (i.e. new miners)
		// if yes, trigger DKG
		change, err := c.matchNewConfig(ctx, oldTs, newTs, oldSt, newSt, diff)
		if err != nil {
			log.Errorw("Error checking for new configuration", "err", err)
			return false, nil, err
		}

		// check if a new public key was created (i.e. the DKG completed)
		change3, err := c.matchNewPublicKey(ctx, oldTs, newTs, oldSt, newSt, diff)
		if err != nil {
			log.Errorw("Error checking for new public key", "err", err)
			return false, nil, err
		}
		//check if it is time for a new checkpoint
		change2, err := c.matchCheckpoint(ctx, oldTs, newTs, oldSt, newSt, diff)
		if err != nil {
			log.Errorw("Error checking if it is time for a new checkpoint", "err", err)
			return false, nil, err
		}


		return change || change2 || change3 , diff, nil
	}

	// Listen to changes in Eudico
	// `76587687658765876` <- This is the confidence threshold used to determine if the StateChangeHandler should be triggered.
	// It is an absurdly high number so the metric used to determine if to trigger it or not is the number of tipsets that have passed in the heaviest chain (the 5 you see there)
	// put 1 here for testing purpose (i,e, there are no forks)
	err := c.events.StateChanged(checkFunc, changeHandler, revertHandler, 1, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (c *CheckpointingSub) matchNewPublicKey(ctx context.Context, oldTs, newTs *types.TipSet, oldSt, newSt mpower.State, diff *diffInfo) (bool, error) {
	if !bytes.Equal(oldSt.PublicKey, newSt.PublicKey) {
		// update this variable to add later on the ability to remove miners
		c.newDKGComplete = true
		c.newKey = newSt.PublicKey
		c.keysUpdated = false
		diff.newPublicKey = newSt.PublicKey
		fmt.Println("The new public key has correctly been updated")
		return true, nil
	}
	return false, nil
}

func (c *CheckpointingSub) matchNewConfig(ctx context.Context, oldTs, newTs *types.TipSet, oldSt, newSt mpower.State, diff *diffInfo) (bool, error) {
	/*
		Now we compared old Power Actor State and new Power Actor State
	*/
	

	// If no changes in configuration
	if sameStringSlice(oldSt.Miners, newSt.Miners) {
		return false, nil
	}
	// only the participants in the new config need to trigger the DKG
	for _, participant := range newSt.Miners {
		log.Infow("New config detected")
		if participant == c.host.ID().String() {
			diff.newMiners = newSt.Miners
			return true, nil
		}
	}
	return false, nil

}

func (c *CheckpointingSub) matchCheckpoint(ctx context.Context, oldTs, newTs *types.TipSet, oldSt, newSt mpower.State, diff *diffInfo) (bool, error) {
	// we are checking that the list of mocked actor is not empty before starting the checkpoint
	
	//if c.newDKGComplete && newTs.Height()%checkpointFrequency == 0 && len(oldSt.Miners) > 0 && (c.taprootConfig != nil || c.newTaprootConfig != nil) && len(newSt.PublicKey)>0 {
	if newTs.Height()%checkpointFrequency == 0 && len(oldSt.Miners) > 0 && (c.taprootConfig != nil || c.newTaprootConfig != nil) && len(newSt.PublicKey)>0 {
	
		log.Infow("New checkpoint to start")
		cp := oldTs.Key().Bytes() // this is the checkpoint
		diff.cp = cp

		// If we don't have a taprootconfig we don't sign because it means we were not part
		// of the previous DKG and hence we need to let the "previous" miners update the aggregated
		// key on bitcoin before starting signing.
		// We update our config to be ready for next checkpointing
		// This is the case for any "new" miner (i.e., not Alice, Bob and Charlie)
		// Basically we skip the next
		if c.taprootConfig == nil {
			log.Infow("We don't have any config")
			pubkey := c.newTaprootConfig.PublicKey // the new taproot config has been initialized
			//during the DKG (in which the new node took part when they joined)

			pubkeyShort := genCheckpointPublicKeyTaproot(pubkey, cp)

			c.taprootConfig = c.newTaprootConfig
			merkleRoot := hashMerkleRoot(pubkey, cp)
			c.tweakedValue = hashTweakedValue(pubkey, merkleRoot)
			c.pubkey = pubkeyShort
			c.newTaprootConfig = nil
			//c.participants = newSt.Miners // we add ourselves to the list of participants
			// even the participants who did not do the checkpoint need to update their participant list
			if len(c.newParticipants)>0 {
				c.participants = make([]string, len(c.newParticipants))
				copy(c.participants,c.newParticipants)
				c.newParticipants = make([]string,0)
			}
			c.newDKGComplete = false
			//c.newKey =

		} else {
			// Miners config is the data that will be stored on a eudico-KVS
			var minersConfig string = hex.EncodeToString(cp) + "\n"
			for _, partyId := range newSt.Miners { // list of new miners
				minersConfig += partyId + "\n"
			}

			// This creates the file that will be stored in the KVS (or any storage)
			hash, err := CreateMinersConfig([]byte(minersConfig))
			if err != nil {
				log.Errorf("could not create miners config: %v", err)
				return false, err
			}
			//keep track of the change so we can trigger the signing
			diff.hash = hash

			//Push config to the KVS
			msgs := &MsgData{Content: []byte(minersConfig)}
			//push config to kvs
			cid_str, err := msgs.HashedCid() 
			err = c.r.setLocal(ctx, cid_str, msgs)
			if err != nil {
				log.Errorf("could not push miners config to kvs: %v", err)
				return false, err
			} 
			//Push data to everyone
			c.r.PushCheckpointMsgs(*msgs,false)
			fmt.Println("Data pushed to KVS: ",[]byte(minersConfig))
		}

		return true, nil
	}
	return true, nil
}

func (c *CheckpointingSub) triggerChange(ctx context.Context, diff *diffInfo) (more bool, err error) {
	//If there is a new configuration, trigger the DKG
	if len(diff.newMiners) > 0 {
		log.Infow("Generate new aggregated key")
		err := c.GenerateNewKeys(ctx, diff.newMiners)
		if err != nil {
			log.Errorw("error while generating new key: %v", err)
			// If generating new key failed, checkpointing should not be possible
			return true, err
		}

		log.Infow("Successful DKG")
	}

	// trigger the new checkpoint
	if diff.cp != nil && diff.hash != nil {
	//if len(diff.newPublicKey)>0 {
		// the checkpoint is created by the "previous" set of miners
		// so that the new key is updated
		fmt.Println("Checkpoint participants: ", c.participants)
		for _, participant := range(c.participants){
			if c.host.ID().String()==participant {
				err = c.CreateCheckpoint(ctx, diff.cp, diff.hash, c.participants)
				if err != nil {
					log.Errorw("could not create checkpoint: %v", err)
					return true, err
				}
				break
			}
		}
		c.newDKGComplete = false
	}
	return true, nil
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
	// NOTE: 1000 has been choosen arbitraly there is no reason for this number besides it just works.
	sub, err := topic.Subscribe(pubsub.WithBufferSize(15000))
	if err != nil {
		return err
	}
	c.sub = sub


	c.listenCheckpointEvents(ctx)



	return nil
}

func (c *CheckpointingSub) GenerateNewKeys(ctx context.Context, participants []string) error {
	fmt.Println("DKG participants: ", participants)
	fmt.Println("Myself (DKG): ", c.host.ID().String())
	idsStrings := participants
	sort.Strings(idsStrings)

	log.Infow("participants list :", "participants", idsStrings)

	ids := c.formIDSlice(idsStrings)

	id := party.ID(c.host.ID().String())

	// var threshold int
	// if threshold = (len(idsStrings) / 2); len(idsStrings) % 2 ==1 {
 //    	threshold = (len(idsStrings) / 2) + 1
	// }
	threshold := (len(idsStrings) / 2) // we need threshold +1 parties to sign
	
	//starting a new ceremony with the subscription and topic that were
	// already defined
	//why not call the checkpointing sub directly?
	n := NewNetwork(c.sub, c.topic)

	// Keygen with Gennaro protocol if failing
	f := frost.KeygenTaprootGennaro(id, ids, threshold)
	//f := frost.KeygenTaproot(id, ids, threshold)



	//handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})
	sessionID := strings.Join(idsStrings, "")
	handler, err := protocol.NewMultiHandler(f, []byte(sessionID))
	if err != nil {
		return err
	}
	LoopHandlerDKG(ctx, handler, n, len(idsStrings),c.file,) //use the new network, could be re-written
	r, err := handler.Result()
	if err != nil {
		// if a participant is mibehaving the DKG entirely fail (no fallback)
		return err
	}
	log.Infow("result :", "result", r)

	var ok bool
	c.newTaprootConfig, ok = r.(*keygen_gennaro.TaprootConfig)
	fmt.Println(c.newTaprootConfig)
	if !ok {
		return xerrors.Errorf("state change propagated is the wrong type")
	}
	//c.newDKGComplete = true
	c.newKey = []byte(c.newTaprootConfig.PublicKey)
	c.newParticipants = make([]string,0)
	// we remove the misbehaving participants from the new set of signers
	participantsToRemove := make([]string,0)
	for  participant,_ := range(c.newTaprootConfig.VerificationShares){
		//if participant != "12D3KooWSpyoi7KghH98SWDfDFMyAwuvtP8MWWGDcC1e1uHWzjSm"{
		c.newParticipants = append(c.newParticipants,string(participant))
		participantsToRemove = append(participantsToRemove, string(participant))
	}

	// we need to update the taproot public key in the mocked actor
	// this is done by sending a transaction with method 4 (which
	// corresponds to the "add new public key method")
	// for now only alice sends the transaction (will need to be changed TODO)
	if c.host.ID().String() == "12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4" {
		addp := &mpower.NewTaprootAddressParam{
			PublicKey: []byte(c.newTaprootConfig.PublicKey), // new public key that was just generated
		}

		seraddp, err1 := actors.SerializeParams(addp)
		if err1 != nil {
			return err1
		}

		a, err2 := address.NewIDAddress(65)
		if err2 != nil {
			return xerrors.Errorf("mocked actor address not working")
		}

		//TODO: change this, import the wallet automatically
		// right now we are just copying Alice's address manually (short-term solution)
		aliceaddr, err3 := address.NewFromString("t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba")
		if err3 != nil {
			return xerrors.Errorf("alice address not working")
		}

		_, aerr := c.api.MpoolPushMessage(ctx, &types.Message{
			To:     a,         //this is the mocked actor address
			From:   aliceaddr, // this is alice address, will need to be changed at some point
			Value:  abi.NewTokenAmount(0),
			Method: 4, // this method corresponds to the one updating the key
			Params: seraddp,
		}, nil)

		if aerr != nil {
			return aerr
		}

		fmt.Println("message to update the public key sent")

		// the issue is that removing participants will also trigger 
		// a DKG so we do not do it here
		// if len(participantsToRemove)>0 {
		// 	addp = &mpower.NewTaprootAddressParam{
		// 		PublicKey: []byte(c.newTaprootConfig.PublicKey), // new public key that was just generated
		// 	}

		// 	seraddp, err1 = actors.SerializeParams(addp)
		// 	if err1 != nil {
		// 		return err1
		// 	}
		// 	_, aerr = c.api.MpoolPushMessage(ctx, &types.Message{
		// 		To:     a,         //this is the mocked actor address
		// 		From:   aliceaddr, // this is alice address, will need to be changed at some point
		// 		Value:  abi.NewTokenAmount(0),
		// 		Method: 4,
		// 		Params: seraddp,
		// 	}, nil)

		// 	if aerr != nil {
		// 		return aerr
		// 	}

		// 	fmt.Println("message to update list of miners sentsent")
		// }
	}
	return nil
}

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context, cp, data []byte, participants []string) error {

	//if participants.Contains(c.host.ID().String()) && c.host.ID().String() != "12D3KooWSpyoi7KghH98SWDfDFMyAwuvtP8MWWGDcC1e1uHWzjSm"{
		fmt.Println("I'm a checkpointer")
		taprootAddress, err := pubkeyToTapprootAddress(c.pubkey)
		if err != nil {
			return err
		}

		pubkey := c.taprootConfig.PublicKey
		// if a new public key was generated (i.e. new miners), we use this key in the checkpoint
		if c.newDKGComplete {
			//pubkey = c.newTaprootConfig.PublicKey // change this to update from the actor
			pubkey = taproot.PublicKey(c.newKey)
		}

		pubkeyShort := genCheckpointPublicKeyTaproot(pubkey, cp)
		newTaprootAddress, err := pubkeyToTapprootAddress(pubkeyShort)
		if err != nil {
			return err
		}
		//hex.DecodeString
		//hex.DecodeString(newTaprootAddress))
		// the list of participants is ordered
		// we will chose the "first" half of participants
		// in order to sign the transaction in the threshold signing.
		// In later improvement we will choose them randomly.

		// list from mocked power actor:
		sort.Strings(participants)
		idsStrings := participants

		log.Infow("participants list :", "participants", idsStrings)
		log.Infow("precedent tx", "txid", c.ptxid)
		ids := c.formIDSlice(idsStrings)
		taprootScript := getTaprootScript(c.pubkey)
		//we add our public key to our bitcoin wallet
		success := addTaprootToWallet(c.cpconfig.BitcoinHost, taprootScript)
		if !success {
			return xerrors.Errorf("failed to add taproot address to wallet")
		}
		if c.ptxid == "" {
			log.Infow("missing precedent txid")

			// sleep an arbitrary long time to be sure it has been scanned
			// removed this because now we are adding without rescanning (too long)
			time.Sleep(3 * time.Second)
			//time.Sleep(20 * time.Second)

			//we get the transaction id using our bitcoin client
			ptxid, err := walletGetTxidFromAddress(c.cpconfig.BitcoinHost, taprootAddress)
			if err != nil {
				return err
			}
			c.ptxid = ptxid
			log.Infow("found precedent txid:", "txid", c.ptxid)
		}

		index := 0
		var value float64
		var scriptPubkeyBytes []byte
		//fmt.Println("Previous tx id: ", c.ptxid)
		//value, scriptPubkeyBytes = getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)
		//fmt.Println("scriptPubkeyBytes: ", scriptPubkeyBytes)
		if len(c.scriptPubkeyBytes) == 0 {
			value, scriptPubkeyBytes = getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)

			// TODO: instead of calling getTxOUt we need to check for the latest transaction
			// same as is done in the verification.sh script

			if scriptPubkeyBytes[0] != 0x51 {
				log.Infow("wrong txout")
				index = 1
				value, scriptPubkeyBytes = getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)
			}

			fmt.Println("scriptPubkeyBytes: ", scriptPubkeyBytes)
		} else {
			fmt.Println("Did not call gettxout")
			value = c.amount
			scriptPubkeyBytes = c.scriptPubkeyBytes
		}
		newValue := value - c.cpconfig.Fee
		c.amount = newValue
		//fmt.Println("Fee for next transaction is: ", c.cpconfig.Fee)
		payload1 := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createrawtransaction\", \"params\": [[{\"txid\":\"" + c.ptxid + "\",\"vout\": " + strconv.Itoa(index) + ", \"sequence\": 4294967295}], [{\"" + newTaprootAddress + "\": \"" + fmt.Sprintf("%.8f", newValue) + "\"}, {\"data\": \"" + hex.EncodeToString(data) + "\"}]]}"
		fmt.Println("Data pushed to opreturn: ", hex.EncodeToString(data))
		

		result := jsonRPC(c.cpconfig.BitcoinHost, payload1)
		fmt.Println("Raw tx payload1: ", payload1)
		if result == nil {
			return xerrors.Errorf("can not create new transaction")
		}
		fmt.Println("Create raw tx result:", result)
		var hexString map[string]interface{}
		

		if err = json.Unmarshal([]byte(payload1), &hexString); err != nil {
			return err
		}
		fmt.Println(hexString)
		//a,_ := hex.DecodeString(hexString)
		//fmt.Println("trying to replicate Create raw tx result:",  a)

		rawTransaction := result["result"].(string)

		tx, err := hex.DecodeString(rawTransaction)
		if err != nil {
			return err
		}

		fmt.Println("TX: ", tx)
		
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(value*100000000))
		utxo := append(buf[:], []byte{34}...)
		utxo = append(utxo, scriptPubkeyBytes...)
		
		fmt.Println("UTXO: ", utxo)

		hashedTx, err := TaprootSignatureHash(tx, utxo, 0x00)
		if err != nil {
			return err
		}


		/*
		 * Orchestrate the signing message
		 */
		fmt.Println("I'm starting the checkpointing")
		log.Infow("starting signing")
		fmt.Println("Message to be signed: ",hashedTx[:])
		// Here all the participants sign the transaction
		// in practice we only need "threshold" of them to sign
		f := frost.SignTaprootWithTweak(c.taprootConfig, ids, hashedTx[:], c.tweakedValue[:])
		n := NewNetwork(c.sub, c.topic)
		// hashedTx[:] is the session id
		// ensure everyone is on the same session id
		fmt.Println("SSID: ", hashedTx[:])
		handler, err := protocol.NewMultiHandler(f, hashedTx[:])
		if err != nil {
			return err
		}
		LoopHandlerSign(ctx, handler, n, len(idsStrings), c.file)
		r, err := handler.Result()
		// if err != nil {
		// 	return err
		// }
		newSetOfParticipants := make([]string, 0)
		ssid := hashedTx[:]
		j := 0
		for (err != nil) {	
			strErr := err.Error()
			if strErr[:8] == "culprits" {
				i := strings.Index(strErr, "]")
				culprits := strErr[11:i]
				s := strings.Split(culprits, " ")
				fmt.Println("culprits: ",s )
				for _, i := range(s){ 
					fmt.Println(i)
					for _,p := range(c.participants) {
						if ! (p == i) { 
							newSetOfParticipants = append(newSetOfParticipants, p)
							}
					}
				}
				time.Sleep(2 * time.Second) // make sure everyone has finished prev protocol before starting
				ids = c.formIDSlice(newSetOfParticipants)
				f = frost.SignTaprootWithTweak(c.taprootConfig, ids, hashedTx[:], c.tweakedValue[:])
				ssid = append(ssid,byte(j))
				fmt.Println("SSID: ", ssid)
				handler, err2 := protocol.NewMultiHandler(f, ssid)
				if err2 != nil {
					return err
				}
				LoopHandlerSign(ctx, handler, n, len(newSetOfParticipants), c.file)
				r, err = handler.Result()
				i = i+1
			} else {return err }
		}
		log.Infow("result :", "result", r)

		if len(newSetOfParticipants)>0 {
			c.participants = make([]string, len(newSetOfParticipants))
			copy(c.participants, newSetOfParticipants)
			//c.participants = newSetOfParticipants
			fmt.Println("New list of participants: ", c.participants)
			//return nil
		}

		// if signing is a success we register the new value
		merkleRoot := hashMerkleRoot(pubkey, cp)
		c.tweakedValue = hashTweakedValue(pubkey, merkleRoot)
		c.pubkey = pubkeyShort //updates the public key to the new key

		c.ptxid = ""

		// Only first one broadcast the transaction ?
		// Actually all participants can broadcast the transcation. It will be the same everywhere.
		rawtx := prepareWitnessRawTransaction(rawTransaction, r.(taproot.Signature))
		payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendrawtransaction\", \"params\": [\"" + rawtx + "\"]}"
		//fmt.Println("Send raw transaction command:", payload)
		//c.scriptPubkeyBytes = 
		if c.host.ID().String() == "12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4" {
			// only alice needs to broadcast the transaction
			result = jsonRPC(c.cpconfig.BitcoinHost, payload)
			//fmt.Println("Transaction to be sent: ", result)
			if result["error"] != nil {
				return xerrors.Errorf("failed to broadcast transaction")
			}
			fmt.Println("Tx id: ", result["result"].(string))
		}
		c.scriptPubkeyBytes,_ = hex.DecodeString(getTaprootScript(pubkeyShort))

		/* Need to keep this to build next one */
		// byte array of raw tx
		hexTx,_ := hex.DecodeString(rawTransaction)
		// byte tx id need to encode to string
		// double hash to get the tx id
		doubleSha := sha256Util(sha256Util(hexTx))
		doubleShaReversed := reverse(doubleSha)
		newtxid := hex.EncodeToString(doubleShaReversed)
	
		//newtxid := result["result"].(string)
		log.Infow("new Txid:", "newtxid", newtxid)
		fmt.Println("new Txid:", newtxid)

		c.ptxid = newtxid


		// If we have new config (i.e. a DKG has completed and we participated in it)
		// we replace the previous config with this config
		// Note: if someone left the protocol, they will not do this so this is not great
		if c.newTaprootConfig != nil {
			fmt.Println("Changed my config")
			c.taprootConfig = c.newTaprootConfig
			c.newTaprootConfig = nil
		}

		// even miners who left the protocol will do this as their newDKGComplete
		// return true for everyone after a DKG has completed (whether they took part or no)
		if c.newDKGComplete {
			c.keysUpdated = true
			c.participants = make([]string, len(c.newParticipants))
			copy(c.participants, c.newParticipants)
			c.newParticipants = []string{}
			c.newDKGComplete = false

		}

	return nil
}

func reverse(s []byte) []byte {
    a := make([]byte, len(s))
    copy(a, s)

    for i := len(a)/2 - 1; i >= 0; i-- {
        opp := len(a) - 1 - i
        a[i], a[opp] = a[opp], a[i]
    }

    return a
}

// func (c *CheckpointingSub) orderParticipantsList() []string {
// 	var ids []string
// 	for id := range c.taprootConfig.VerificationShares { // change for mocked actor
// 		ids = append(ids, string(id))
// 	}

// 	sort.Strings(ids)

// 	return ids
// }

func (c *CheckpointingSub) formIDSlice(ids []string) party.IDSlice {
	var _ids []party.ID
	for _, p := range ids {
		_ids = append(_ids, party.ID(p))
	}

	idsSlice := party.NewIDSlice(_ids)

	return idsSlice
}

/*
	BuildCheckpointingSub is called after creating the checkpointing instance.
	It verifies connectivity with the Bitcoin node and retrieve the first checkpoint
	(i.e. the hash of the eudico genesis block)
	and if the node is a **participant** will pre-compute some values used in signing.
	The initial checkpointing transaction is then created by the taproot.sh script using an address retrieved
	manually.
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

	// Get first checkpoint from eudico block 0
	ts, err := c.api.ChainGetGenesis(ctx)
	if err != nil {
		log.Errorf("could not get genesis tipset: %v", err)
		return
	} else {
		log.Infow("Got genesis tipset")
	}

	cidBytes := ts.Key().Bytes()
	//fmt.Println("cidbytes: ", cidBytes)
	//fmt.Println("public key before decoding: ", c.cpconfig.PublicKey) // this is the checkpoint (i.e. hash of block)
	publickey, err := hex.DecodeString(c.cpconfig.PublicKey)          //publickey pre-generated
	if err != nil {
		log.Errorf("could not decode public key: %v", err)
		return
	} else {
		log.Infow("Decoded Public key")
	}

	// initialize the kvs
	c.r = NewResolver(c.host.ID(), c.ds, c.pubsub)
	fmt.Println("My id: ",c.host.ID())
	err = c.r.HandleMsgs(ctx)
	if err != nil {
		log.Errorf("error initializing KVS resolver: %s", err)
		return
	}



	//eiher send the funding transaction (if needed) or get the latest checkpoint
	// of the transaction has already been sent
	if c.taprootConfig != nil {
		c.pubkey = genCheckpointPublicKeyTaproot(c.taprootConfig.PublicKey, cidBytes)

		// this should be changed such that the public key is updated when eudico is stopped
		// (so that we can continue the checkpointing without restarting from scratch each time)
		address, _ := pubkeyToTapprootAddress(c.pubkey)
		fmt.Println("Address: ", address)

		// only Alice will send the funding transaction for testing purpose
		if c.host.ID().String() == "12D3KooWMBbLLKTM9Voo89TXLd98w4MjkJUych6QvECptousGtR4" {
			//start by getting the balance in our wallet (only if sendall is true, i.e. we send all the amount)
			var value float64
			if sendall {
				payload1 := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"getbalances\", \"params\": []}"
				result1 := jsonRPC(c.cpconfig.BitcoinHost, payload1)
				fmt.Println("Getbalances result: ", result1)
				intermediary1 := result1["result"].(map[string]interface{})
				intermediary2 := intermediary1["mine"].(map[string]interface{})
				value = intermediary2["trusted"].(float64)
				fmt.Println("Initial value in walet: ", value)
			} else {
				value = initialValueInWallet
			}
			newValue := value - c.cpconfig.Fee
			c.amount = newValue
			payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendtoaddress\", \"params\": [\"" + address + "\", \"" + fmt.Sprintf("%.8f", newValue) + "\" ]}"
			//fmt.Println(payload)
			result := jsonRPC(c.cpconfig.BitcoinHost, payload)
			//fmt.Println("result payload:", result)
			if result["error"] != nil {
				log.Errorf("could not send initial Bitcoin transaction to: %v", address)
			} else {
				log.Infow("successfully sent first bitcoin tx with id: ",result["result"].(string))
				c.ptxid = result["result"].(string)

			}
			//put the data in kvs
			var minersConfig string = hex.EncodeToString(cidBytes) + "\n"
			// c.orderParticipantsList() orders the miners from the taproot config --> to change
			//for _, partyId := range c.orderParticipantsList() {
			for _, partyId := range c.participants { // list of new miners
				minersConfig += partyId + "\n"
			}	
			msgs := &MsgData{Content: []byte(minersConfig)}
			time.Sleep(2 * time.Second)
			c.r.PushCheckpointMsgs(*msgs,false)
		}
		for {
			init, txid, err := CheckIfFirstTxHasBeenSent(c.cpconfig.BitcoinHost, publickey, cidBytes)
			if init {
				c.ptxid = txid
				if err != nil {
					log.Errorf("Error with check if first tx has been sent")
				}
				break
			}
		}
	}

	// Get the last checkpoint from the bitcoin node

	btccp, err := GetLatestCheckpoint(c.cpconfig.BitcoinHost, publickey, cidBytes)

	if err != nil {
		log.Errorf("could not get last checkpoint from Bitcoin: %v", err)
		return
	} else {
		log.Infow("Got last checkpoint from Bitcoin node")
		fmt.Println(btccp)
	}

	
	fmt.Println("last cid from bitcoin: ", btccp.cid)
	c.lastCid = btccp.cid


	// Pre-compute values from participants in the signing process
	if c.taprootConfig != nil {
		// save public key taproot
		// NOTE: cidBytes is the tipset key value (aka checkpoint) from the genesis block.
		// When Eudico is stopped it should remember what was the last tipset key value
		// it signed and replace it with it. Config is not saved, neither when new DKG is done.
		c.pubkey = genCheckpointPublicKeyTaproot(c.taprootConfig.PublicKey, cidBytes)

		// Get the taproot address used in taproot.sh
		// this should be changed such that the public key is updated when eudico is stopped
		// (so that we can continue the checkpointing without restarting from scratch each time)
		address, _ := pubkeyToTapprootAddress(c.pubkey)
		fmt.Println(address)

		// to do: write method to get the total amount in the wallet we are using
		//value, scriptPubkeyBytes := getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)

		// if scriptPubkeyBytes[0] != 0x51 {
		// 	log.Infow("wrong txout")
		// 	index = 1
		// 	value, scriptPubkeyBytes = getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)
		// }

		// Save tweaked value
		merkleRoot := hashMerkleRoot(c.taprootConfig.PublicKey, cidBytes)
		c.tweakedValue = hashTweakedValue(c.taprootConfig.PublicKey, merkleRoot)
	}
	c.file, err = os.Create("data.txt")
    if err != nil {
        log.Fatal(err)
    }

	// Start the checkpoint module
	err = c.Start(ctx)
	if err != nil {
		log.Errorf("could not start checkpointing module: %v", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?

			// NOTES: new config and checkpoint should be saved in a file for when we restart the node

			return nil
		},
	})

}
