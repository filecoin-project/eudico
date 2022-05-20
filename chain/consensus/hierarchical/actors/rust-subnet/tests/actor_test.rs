mod harness;
use std::str::FromStr;

use fil_hierarchical_subnet_actor::types::{ConsensusType, ConstructParams};
use fvm_shared::address::{Address, SubnetID};
use fvm_shared::econ::TokenAmount;

use crate::harness::Harness;

#[test]
fn test_constructor() {
    let params = ConstructParams {
        parent: SubnetID::from_str("/root").unwrap(),
        name: String::from("test"),
        consensus: ConsensusType::Delegated,
        min_validator_stake: TokenAmount::from(10_u64.pow(18)),
        check_period: 10,
        genesis: Vec::new(),
    };

    let mut h = Harness::new();
    h.constructor(params);
    // Send message
    // let message = Message {
    //     from: sender[0].1,
    //     // from: Address::new_id(1), // INIT_ACTOR_ADDR
    //     to: actor_address,
    //     gas_limit: 1000000000,
    //     method_num: 2,
    //     // params: RawBytes::serialize(params).unwrap(),
    //     ..Message::default()
    // };

    // actor_state.total_stake += 100;
    // let bs = tester.blockstore();
    // let mh_code = Code::Blake2b256;
    // let p_cid = Cid::new_v1(
    //     fvm_ipld_encoding::DAG_CBOR,
    //     mh_code.digest(&to_vec(&actor_state).unwrap()),
    // );
    // bs.get(&p_cid).unwrap().unwrap();
}
