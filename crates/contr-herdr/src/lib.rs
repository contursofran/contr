//! Client for herdr's JSON API socket. `wire` reads; `command` changes herdr's state.

pub mod command;
mod model;
pub mod wire;

pub use model::*;
