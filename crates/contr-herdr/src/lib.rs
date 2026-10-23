//! Clients for herdr's sockets. `wire` reads herdr's JSON API, `command`
//! changes herdr's state through the same API, and `endpoint` is the binary
//! endpoint socket that the surface link uses.

pub mod command;
pub mod endpoint;
mod model;
mod tabs;
pub mod wire;

pub use model::*;
pub use tabs::*;
