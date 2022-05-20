use std::env;

use cid::multihash::{Code, MultihashDigest};
use cid::Cid;
use fvm::executor::ApplyKind;
use fvm::executor::Executor;
use fvm::state_tree::StateTree;
use fvm_integration_tests::tester::{Account, Tester};
use fvm_ipld_blockstore::{Blockstore, MemoryBlockstore};
use fvm_ipld_encoding::{to_vec, RawBytes};
use fvm_shared::address::Address;
use fvm_shared::bigint::BigInt;
use fvm_shared::econ::TokenAmount;
use fvm_shared::error::ExitCode;
use fvm_shared::message::Message;
use fvm_shared::state::StateTreeVersion;
use fvm_shared::version::NetworkVersion;
use fvm_shared::ActorID;
use num_traits::Zero;
use std::borrow::Borrow;

use fil_hierarchical_subnet_actor::state::State;
use fil_hierarchical_subnet_actor::types::ConstructParams;

pub const TEST_INIT_ACTOR_ADDR: ActorID = 339;

// pub struct Harness<B: Blockstore + 'static> {
//     pub tester: Tester<B>,
// }
pub struct Harness {
    pub tester: Tester<MemoryBlockstore>,
    pub actor_address: Address,
}

const WASM_COMPILED_PATH: &str =
    "target/debug/wbuild/fil_hierarchical_subnet_actor/fil_hierarchical_subnet_actor.compact.wasm";

// impl<B: fvm_ipld_blockstore::Blockstore> Harness<B> {
impl Harness {
    pub fn new() -> Self {
        // Instantiate tester
        let mut tester = Tester::new(
            NetworkVersion::V15,
            StateTreeVersion::V4,
            MemoryBlockstore::default(),
        )
        .unwrap();

        let sender: [Account; 1] = tester.create_accounts().unwrap();

        // Get wasm bin
        let wasm_path = env::current_dir()
            .unwrap()
            .join(WASM_COMPILED_PATH)
            .canonicalize()
            .unwrap();
        let wasm_bin = std::fs::read(wasm_path).expect("Unable to read file");

        // Set actor state
        // let actor_state = State::new(params);
        let mut actor_state = State::default();
        // let actor_state = TState::default();
        let state_cid = tester.set_state(&actor_state).unwrap();
        // let mh_code = Code::Blake2b256;
        // let p_cid = Cid::new_v1(
        //     fvm_ipld_encoding::DAG_CBOR,
        //     mh_code.digest(&to_vec(&actor_state).unwrap()),
        // );
        // assert_eq!(state_cid, p_cid);

        // Set actor
        let actor_address = Address::new_id(10000);
        let init_addr = tester
            .make_id_account(TEST_INIT_ACTOR_ADDR, TokenAmount::from(10_u64.pow(18)))
            .unwrap()
            .0;

        tester
            .set_actor_from_bin(&wasm_bin, state_cid.clone(), actor_address, BigInt::zero())
            .unwrap();

        // Instantiate machine
        tester.instantiate_machine().unwrap();

        Self {
            tester,
            actor_address,
        }
    }

    pub fn constructor(&mut self, params: ConstructParams) {
        let message = Message {
            from: Address::new_id(TEST_INIT_ACTOR_ADDR), // INIT_ACTOR_ADDR
            to: self.actor_address,
            gas_limit: 1000000000,
            method_num: 1,
            params: RawBytes::serialize(params.clone()).unwrap(),
            ..Message::default()
        };

        let res = self
            .tester
            .executor
            .as_mut()
            .unwrap()
            .execute_message(message, ApplyKind::Explicit, 100)
            .unwrap();

        assert_eq!(
            ExitCode::from(res.msg_receipt.exit_code.value()),
            ExitCode::OK
        );

        // check state
        let bs = self.tester.blockstore();
        StateTree::new(bs, StateTreeVersion::V4).unwrap();
        // let mh_code = Code::Blake2b256;
        // let p_cid = Cid::new_v1(
        //     fvm_ipld_encoding::DAG_CBOR,
        //     mh_code.digest(&to_vec(&st).unwrap()),
        // );
        // let tree = self.tester.state_tree.as_ref().unwrap();
        // let st_cid = tree.get_actor(&self.actor_address).unwrap().unwrap().state;
        // let st = bs.get(&st_cid).unwrap().unwrap();
        // let sst: State = RawBytes::deserialize(&RawBytes::from(st)).unwrap();
    }
}
