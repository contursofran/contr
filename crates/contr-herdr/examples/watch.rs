//! Prints the herdr tree, then one line per agent status change, until killed.
//! Read-only: it connects the way contr does and never sends input to herdr.
//!
//! `cargo run -p contr-herdr --example watch`

use contr_herdr::{ConnectTarget, spawn_reader};
use tracing_subscriber::EnvFilter;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| "contr_herdr=info".into()),
        )
        .init();
    let (tx, rx) = async_channel::bounded(64);
    let _reader = spawn_reader(ConnectTarget::Local, tx)?;
    while rx.recv_blocking().is_ok() {}
    Ok(())
}
