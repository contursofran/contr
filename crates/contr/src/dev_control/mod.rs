//! The dev control server. The module exists only in debug builds (`lib.rs` gates it), and it
//! listens only when `CONTR_DEV_CONTROL` names a socket path. A release build has none of it.

pub mod board;
pub mod dispatch;
pub mod server;
pub mod sidebar;
pub mod title;
pub mod window_id;

use crate::dev_protocol::{self, Reply, Request};
use gpui::{App, Window, WindowHandle};
use std::ffi::OsString;
use std::path::PathBuf;
use std::rc::Rc;

/// Answers one request in the window. It runs on the UI thread, inside the window's update.
pub type Handler = Rc<dyn Fn(Request, &mut Window, &mut App) -> Reply>;

/// The handler for the window: the request dispatcher.
pub fn handler() -> Handler {
    Rc::new(dispatch::handle)
}

/// The socket path that `CONTR_DEV_CONTROL` names. An unset or empty variable names none.
pub fn requested_path(value: Option<OsString>) -> Option<PathBuf> {
    value.filter(|value| !value.is_empty()).map(PathBuf::from)
}

/// Starts the server for `window` when `CONTR_DEV_CONTROL` names a path. Without it, nothing
/// listens and the app runs as before.
pub fn start_from_env<V: 'static>(window: WindowHandle<V>, cx: &mut App) {
    if let Some(path) = requested_path(std::env::var_os(dev_protocol::ENV)) {
        server::start(path, window.into(), handler(), cx);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_unset_or_empty_variable_names_no_socket() {
        assert_eq!(requested_path(None), None);
        assert_eq!(requested_path(Some(OsString::new())), None);
    }

    #[test]
    fn a_set_variable_names_its_path() {
        assert_eq!(
            requested_path(Some("/tmp/dev.sock".into())),
            Some(PathBuf::from("/tmp/dev.sock"))
        );
    }
}
