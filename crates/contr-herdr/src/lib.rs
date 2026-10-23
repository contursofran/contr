//! Clients for herdr's sockets: the read-only JSON API (`wire`) and the
//! binary endpoint socket (`endpoint`).

pub mod endpoint;
mod model;
pub mod wire;

pub use model::*;
