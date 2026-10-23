//! Read-only client for herdr's JSON API socket.

mod model;
mod reader;
pub mod wire;

pub use model::*;
pub use reader::*;
