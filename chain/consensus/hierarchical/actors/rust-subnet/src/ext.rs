pub mod sca {
    use fvm_shared::clock::ChainEpoch;
    use fvm_shared::econ::TokenAmount;
    pub const MIN_CHECK_PERIOD: ChainEpoch = 10;
    pub const MIN_STAKE: u64 = 10_u64.pow(18);
}
