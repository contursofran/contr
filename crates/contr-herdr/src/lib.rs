//! Read-only client for the local herdr daemon: a tree of workspaces, tabs,
//! panes and agent states, kept up to date by a reader thread that reconnects
//! on its own. It never sends input to herdr.

mod connection;
mod model;

pub use connection::*;
pub use herdr_client::ConnectTarget;
pub use model::*;
