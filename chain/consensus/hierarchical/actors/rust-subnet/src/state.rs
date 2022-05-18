use cid::multihash::Code;
use cid::Cid;
use fvm_ipld_encoding::tuple::{Deserialize_tuple, Serialize_tuple};
use fvm_ipld_encoding::{to_vec, CborStore, DAG_CBOR};
use fvm_sdk as sdk;
use fvm_shared::address::SubnetID;
use fvm_shared::bigint::bigint_ser;
use fvm_shared::bigint::Zero;
use fvm_shared::clock::ChainEpoch;
use fvm_shared::econ::TokenAmount;

use crate::abort;
use crate::blockstore::*;
use crate::ext;
use crate::types::*;

/// The state object.
#[derive(Serialize_tuple, Deserialize_tuple, Clone, Debug)]
pub struct State {
    pub name: String,
    pub parent_id: SubnetID,
    pub consensus: ConsensusType,
    #[serde(with = "bigint_ser")]
    pub min_miner_stake: TokenAmount,
    #[serde(with = "bigint_ser")]
    pub total_stake: TokenAmount,
    pub stake: Cid, // BalanceTable of stake (HAMT[address]TokenAmount)
    pub status: Status,
    pub genesis: Vec<u8>,
    pub check_period: ChainEpoch,
    pub checkpoints: Cid,   // HAMT[cid]Checkpoint
    pub window_checks: Cid, // HAMT[cid]Votes
    pub validator_set: Vec<Validator>,
}

/// We should probably have a derive macro to mark an object as a state object,
/// and have load and save methods automatically generated for them as part of a
/// StateObject trait (i.e. impl StateObject for State).
impl State {
    pub fn new(params: ConstructParams) -> Self {
        let empty_checkpoint_map = match make_empty_map::<_, ()>(&Blockstore).flush() {
            Ok(c) => c,
            Err(e) => abort!(USR_ILLEGAL_STATE, "failed to create empty map: {:?}", e),
        };
        let empty_votes_map = match make_empty_map::<_, ()>(&Blockstore).flush() {
            Ok(c) => c,
            Err(e) => abort!(USR_ILLEGAL_STATE, "failed to create empty map: {:?}", e),
        };
        let empty_stake_map = match make_empty_map::<_, ()>(&Blockstore).flush() {
            Ok(c) => c,
            Err(e) => abort!(USR_ILLEGAL_STATE, "failed to create empty map: {:?}", e),
        };

        let min_stake = TokenAmount::from(ext::sca::MIN_STAKE);

        State {
            name: params.name,
            parent_id: params.parent,
            consensus: params.consensus,
            total_stake: TokenAmount::zero(),
            min_miner_stake: if params.min_miner_stake < min_stake {
                min_stake
            } else {
                params.min_miner_stake
            },
            check_period: if params.check_period < ext::sca::MIN_CHECK_PERIOD {
                ext::sca::MIN_CHECK_PERIOD
            } else {
                params.check_period
            },
            genesis: params.genesis,
            status: Status::Instantiated,
            checkpoints: empty_checkpoint_map,
            stake: empty_stake_map,
            window_checks: empty_votes_map,
            validator_set: vec![],
        }
    }
    pub fn load() -> Self {
        // First, load the current state root.
        let root = match sdk::sself::root() {
            Ok(root) => root,
            Err(err) => abort!(USR_ILLEGAL_STATE, "failed to get root: {:?}", err),
        };

        // Load the actor state from the state tree.
        match Blockstore.get_cbor::<Self>(&root) {
            Ok(Some(state)) => state,
            Ok(None) => abort!(USR_ILLEGAL_STATE, "state does not exist"),
            Err(err) => abort!(USR_ILLEGAL_STATE, "failed to get state: {}", err),
        }
    }

    pub fn save(&self) -> Cid {
        let serialized = match to_vec(self) {
            Ok(s) => s,
            Err(err) => abort!(USR_SERIALIZATION, "failed to serialize state: {:?}", err),
        };
        let cid = match sdk::ipld::put(Code::Blake2b256.into(), 32, DAG_CBOR, serialized.as_slice())
        {
            Ok(cid) => cid,
            Err(err) => abort!(USR_SERIALIZATION, "failed to store initial state: {:}", err),
        };
        if let Err(err) = sdk::sself::set_root(&cid) {
            abort!(USR_ILLEGAL_STATE, "failed to set root ciid: {:}", err);
        }
        cid
    }
}
