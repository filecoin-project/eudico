use anyhow::anyhow;
use fvm_ipld_encoding::{RawBytes};
use serde::{de};

/// A macro to abort concisely.
/// This should be part of the SDK as it's very handy.
#[macro_export]
macro_rules! abort {
    ($code:ident, $msg:literal $(, $ex:expr)*) => {
        fvm_sdk::vm::abort(
            fvm_shared::error::ExitCode::$code.value(),
            Some(format!($msg, $($ex,)*).as_str()),
        )
    };
}

// /// Serializes a structure as a CBOR vector of bytes, returning a serialization error on failure.
// /// `desc` is a noun phrase for the object being serialized, included in any error message.
// pub fn serialize_vec<T>(value: &T, desc: &str) -> anyhow::Result<Vec<u8>>
// where
//     T: ser::Serialize + ?Sized,
// {
//     to_vec(value).map_err(|e| anyhow!(format!("failed to serialize {}: {}", desc, e)))
// }
//
// /// Serializes a structure as CBOR bytes, returning a serialization error on failure.
// /// `desc` is a noun phrase for the object being serialized, included in any error message.
// pub fn serialize<T>(value: &T, desc: &str) -> anyhow::Result<RawBytes>
// where
//     T: ser::Serialize + ?Sized,
// {
//     Ok(RawBytes::new(serialize_vec(value, desc)?))
// }

/// Deserialises CBOR-encoded bytes as a structure, returning a serialization error on failure.
/// `desc` is a noun phrase for the object being deserialized, included in any error message.
pub fn deserialize<O: de::DeserializeOwned>(v: &RawBytes, desc: &str) -> anyhow::Result<O> {
    v.deserialize()
        .map_err(|e| anyhow!(format!("failed to deserialize {}: {}", desc, e)))
}

/// Deserialises CBOR-encoded bytes as a method parameters object.
pub fn deserialize_params<O: de::DeserializeOwned>(params: &RawBytes) -> anyhow::Result<O> {
    deserialize(params, "method parameters")
}
