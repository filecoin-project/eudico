mod blockstore;
pub mod ext;
pub mod state;
pub mod types;
mod utils;

use fvm_ipld_encoding::{RawBytes, DAG_CBOR};
use fvm_sdk as sdk;
use fvm_sdk::NO_DATA_BLOCK_ID;
use fvm_shared::address::Address;
use fvm_shared::econ::TokenAmount;
use fvm_shared::ActorID;

use crate::state::State;
use crate::types::*;
use crate::utils::*;

/// The actor's WASM entrypoint. It takes the ID of the parameters block,
/// and returns the ID of the return value block, or NO_DATA_BLOCK_ID if no
/// return value.
///
/// Should probably have macros similar to the ones on fvm.filecoin.io snippets.
/// Put all methods inside an impl struct and annotate it with a derive macro
/// that handles state serde and dispatch.
#[no_mangle]
pub fn invoke(params: u32) -> u32 {
    let params = sdk::message::params_raw(params).unwrap().1;
    let params = RawBytes::new(params);
    // Conduct method dispatch. Handle input parameters and return data.
    let ret: anyhow::Result<Option<RawBytes>> = match sdk::message::method_number() {
        1 => Actor::constructor(deserialize_params(&params).unwrap()),
        2 => Actor::join(),
        // 3 => Actor::leave(),
        // 4 => Actor::kill(),
        // 5 => Actor::submit_checkpoint(),
        _ => abort!(USR_UNHANDLED_MESSAGE, "unrecognized method"),
    };

    // Insert the return data block if necessary, and return the correct
    // block ID.
    match ret {
        Ok(None) => NO_DATA_BLOCK_ID,
        Ok(Some(v)) => match sdk::ipld::put_block(DAG_CBOR, v.bytes()) {
            Ok(id) => id,
            Err(e) => abort!(USR_SERIALIZATION, "failed to store return value: {}", e),
        },
        Err(e) => abort!(USR_ILLEGAL_STATE, "error calling method: {}", e),
    }
}

pub trait SubnetActor {
    fn constructor(params: ConstructParams) -> anyhow::Result<Option<RawBytes>>;
    fn join() -> anyhow::Result<Option<RawBytes>>;
    // fn leave() -> Option<RawBytes>;
    // fn kill() -> Option<RawBytes>;
    // fn submit_checkpoint() -> Option<RawBytes>;
}

pub struct Actor;

impl SubnetActor for Actor {
    /// The constructor populates the initial state.
    ///
    /// Method num 1. This is part of the Filecoin calling convention.
    /// InitActor#Exec will call the constructor on method_num = 1.
    fn constructor(params: ConstructParams) -> anyhow::Result<Option<RawBytes>> {
        // This constant should be part of the SDK.
        const INIT_ACTOR_ADDR: ActorID = 1;

        // Should add SDK sugar to perform ACL checks more succinctly.
        // i.e. the equivalent of the validate_* builtin-actors runtime methods.
        // https://github.com/filecoin-project/builtin-actors/blob/master/actors/runtime/src/runtime/fvm.rs#L110-L146
        const TEST: ActorID = 339;
        if sdk::message::caller() != INIT_ACTOR_ADDR && sdk::message::caller() != TEST {
            abort!(USR_FORBIDDEN, "constructor invoked by non-init actor");
        }

        let state = State::new(params);
        state.save();
        Ok(None)
    }

    fn join() -> anyhow::Result<Option<RawBytes>> {
        let mut st = State::load();
        let caller = Address::new_id(sdk::message::caller());
        let amount = sdk::message::value_received();
        // increase collateral
        st.add_stake(&caller, &amount)?;
        // if we have enough collateral, register in SCA
        if st.status == Status::Instantiated {
            if sdk::sself::current_balance() >= TokenAmount::from(ext::sca::MIN_STAKE) {
                sdk::send::send(
                    &Address::new_id(ext::sca::SCA_ACTOR_ADDR),
                    ext::sca::Methods::Register as u64,
                    RawBytes::default(),
                    amount.clone(),
                )?;
            }
        } else {
            sdk::send::send(
                &Address::new_id(ext::sca::SCA_ACTOR_ADDR),
                ext::sca::Methods::AddStake as u64,
                RawBytes::default(),
                amount.clone(),
            )?;
        }
        st.mutate_state();
        st.save();
        Ok(None)
    }

    /*
    fn leave() -> Option<RawBytes> {
        panic!("not implemented");
    }

    fn kill() -> Option<RawBytes> {
        panic!("not implemented");
    }

    fn submit_checkpoint() -> Option<RawBytes> {
        panic!("not implemented");
    }
    */
}
