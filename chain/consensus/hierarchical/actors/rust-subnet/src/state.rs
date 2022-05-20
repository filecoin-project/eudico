use anyhow::anyhow;
use cid::multihash::Code;
use cid::Cid;
use fvm_ipld_encoding::tuple::{Deserialize_tuple, Serialize_tuple};
use fvm_ipld_encoding::{to_vec, CborStore, DAG_CBOR};
use fvm_ipld_hamt::{Error as HamtError, Hamt};
use fvm_sdk as sdk;
use fvm_shared::address::{Address, SubnetID};
use fvm_shared::bigint::bigint_ser;
use fvm_shared::bigint::bigint_ser::BigIntDe;
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
    pub min_validator_stake: TokenAmount,
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
            min_validator_stake: if params.min_validator_stake < min_stake {
                min_stake
            } else {
                params.min_validator_stake
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

    pub fn add_stake(&mut self, addr: &Address, amount: &TokenAmount) -> anyhow::Result<()> {
        // update miner stake
        let mut bt = make_map_with_root::<_, BigIntDe>(&self.stake, &Blockstore)?;
        let mut stake = get_stake(&bt, addr)
            .map_err(|e| anyhow!(format!("error getting stake from Hamt: {:?}", e)))?;
        stake += amount;
        set_stake(&mut bt, addr, stake.clone())?;
        self.stake = bt.flush()?;

        // update total collateral
        self.total_stake += amount;

        // check if the miner has coollateral to become a validator
        if stake >= self.min_validator_stake
            && (self.consensus != ConsensusType::Delegated || self.validator_set.len() < 1)
        {
            self.validator_set.push(Validator {
                subnet: SubnetID::new(&self.parent_id, Address::new_id(sdk::message::receiver())),
                addr: addr.clone(),
                // FIXME: Receive address in params
                net_addr: String::new(),
            });
        }

        Ok(())
    }

    pub fn mutate_state(&mut self) {
        match self.status {
            Status::Instantiated => {
                if self.total_stake >= TokenAmount::from(ext::sca::MIN_STAKE) {
                    self.status = Status::Active
                }
            }
            Status::Active => {
                if self.total_stake < TokenAmount::from(ext::sca::MIN_STAKE) {
                    self.status = Status::Inactive
                }
            }
            Status::Inactive => {
                if self.total_stake >= TokenAmount::from(ext::sca::MIN_STAKE) {
                    self.status = Status::Active
                }
            }
            Status::Terminating => {
                if self.total_stake == TokenAmount::zero()
                    && sdk::sself::current_balance() == TokenAmount::zero()
                {
                    self.status = Status::Killed
                }
            }
            _ => {}
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

impl Default for State {
    fn default() -> Self {
        Self {
            name: String::new(),
            parent_id: SubnetID::default(),
            consensus: ConsensusType::Delegated,
            min_validator_stake: TokenAmount::from(ext::sca::MIN_STAKE),
            total_stake: TokenAmount::zero(),
            check_period: 10,
            genesis: Vec::new(),
            status: Status::Instantiated,
            checkpoints: Cid::default(),
            stake: Cid::default(),
            window_checks: Cid::default(),
            validator_set: Vec::new(),
        }
    }
}

pub fn set_stake<BS: fvm_ipld_blockstore::Blockstore>(
    stakes: &mut Hamt<BS, BigIntDe>,
    addr: &Address,
    amount: TokenAmount,
) -> anyhow::Result<()> {
    stakes
        .set(addr.to_bytes().into(), BigIntDe(amount))
        .map_err(|e| anyhow!(format!("failed to set stake for addr {}: {:?}", addr, e)))?;
    Ok(())
}

/// Gets token amount for given address in balance table
pub fn get_stake<'m, BS: fvm_ipld_blockstore::Blockstore>(
    stakes: &'m Hamt<BS, BigIntDe>,
    key: &Address,
) -> Result<TokenAmount, HamtError> {
    if let Some(v) = stakes.get(&key.to_bytes())? {
        Ok(v.0.clone())
    } else {
        Ok(0.into())
    }
}
