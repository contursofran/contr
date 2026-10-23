//! Sends the title bar's commands to herdr. Each command is one request on its own connection,
//! made on a background task so the UI thread never waits on the socket. A failed command is
//! logged; the next herdr tree shows what herdr did. Nothing is shown optimistically.

use crate::title_bar::TabCommand;
use contr_herdr::command;
use contr_herdr::wire::{self, WireError};
use gpui::{App, AppContext};
use std::path::{Path, PathBuf};
use std::time::Duration;
use tracing::warn;

/// How long one command may take before it is given up.
const TIMEOUT: Duration = Duration::from_secs(2);

/// Sends `cmd` to the local herdr. Returns at once; the request runs on a background task.
pub fn send(cmd: TabCommand, cx: &mut App) {
    let Some(path) = socket_or_warn(wire::api_socket_path(), &cmd) else {
        return;
    };
    cx.background_spawn(async move {
        if let Err(error) = run(&path, &cmd) {
            warn!(target: "contr_herdr", "{cmd:?} failed: {error}");
        }
    })
    .detach();
}

/// The socket path, or `None` with a warning that `cmd` is not sent.
fn socket_or_warn(path: Option<PathBuf>, cmd: &TabCommand) -> Option<PathBuf> {
    if path.is_none() {
        warn!(target: "contr_herdr", "no herdr socket path; {cmd:?} not sent");
    }
    path
}

/// The one request `cmd` stands for: `tab.focus`, `tab.create` or `pane.split` to the right.
fn run(path: &Path, cmd: &TabCommand) -> Result<(), WireError> {
    match cmd {
        TabCommand::Focus(tab) => command::focus_tab(path, tab, TIMEOUT),
        TabCommand::Create(workspace) => command::create_tab(path, workspace, TIMEOUT),
        TabCommand::SplitRight(pane) => command::split_right(path, pane, TIMEOUT),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use serde_json::{Value, json};
    use std::io::{BufRead, BufReader, Write};
    use std::os::unix::net::UnixListener;
    use std::thread::{self, JoinHandle};

    /// A fake herdr that accepts one client, writes `reply` (one line) and returns the request
    /// line it read. Returns the directory (keep it alive), the socket path and the thread.
    fn fake_herdr(reply: &'static str) -> (tempfile::TempDir, PathBuf, JoinHandle<Value>) {
        let dir = tempfile::Builder::new()
            .prefix("tc")
            .tempdir_in("/tmp")
            .expect("temp dir");
        let path = dir.path().join("herdr.sock");
        let listener = UnixListener::bind(&path).expect("bind the fake herdr");
        let handle = thread::spawn(move || {
            let (stream, _) = listener.accept().expect("accept");
            let mut request = String::new();
            BufReader::new(&stream)
                .read_line(&mut request)
                .expect("read the request");
            (&stream)
                .write_all(format!("{reply}\n").as_bytes())
                .expect("write the reply");
            serde_json::from_str(&request).expect("the request is JSON")
        });
        (dir, path, handle)
    }

    #[test]
    fn focus_sends_tab_focus_and_accepts_tab_info() {
        let (_dir, path, handle) = fake_herdr(r#"{"result":{"type":"tab_info"}}"#);
        let cmd = TabCommand::Focus("w1:t2".into());
        run(&path, &cmd).unwrap();
        assert_eq!(
            handle.join().unwrap(),
            json!({"id": "contr-tab-focus", "method": "tab.focus", "params": {"tab_id": "w1:t2"}})
        );
    }

    #[test]
    fn create_sends_tab_create_in_the_workspace_focused() {
        let (_dir, path, handle) = fake_herdr(r#"{"result":{"type":"tab_created"}}"#);
        let cmd = TabCommand::Create("w1".into());
        run(&path, &cmd).unwrap();
        assert_eq!(
            handle.join().unwrap(),
            json!({
                "id": "contr-tab-create",
                "method": "tab.create",
                "params": {"workspace_id": "w1", "focus": true}
            })
        );
    }

    #[test]
    fn split_sends_pane_split_to_the_right_focused() {
        let (_dir, path, handle) = fake_herdr(r#"{"result":{"type":"pane_info"}}"#);
        let cmd = TabCommand::SplitRight("w1:p1".into());
        run(&path, &cmd).unwrap();
        assert_eq!(
            handle.join().unwrap(),
            json!({
                "id": "contr-split",
                "method": "pane.split",
                "params": {"target_pane_id": "w1:p1", "direction": "right", "focus": true}
            })
        );
    }

    #[test]
    fn a_server_error_is_returned_with_its_code() {
        let (_dir, path, handle) =
            fake_herdr(r#"{"error":{"code":"tab_not_found","message":"no such tab"}}"#);
        let cmd = TabCommand::Focus("w9:t9".into());
        match run(&path, &cmd) {
            Err(WireError::Server { code, .. }) => assert_eq!(code, "tab_not_found"),
            other => panic!("expected a server error, got {other:?}"),
        }
        handle.join().unwrap();
    }

    #[test]
    fn no_running_herdr_is_reported_as_not_running() {
        let dir = tempfile::Builder::new()
            .prefix("tc")
            .tempdir_in("/tmp")
            .expect("temp dir");
        let cmd = TabCommand::Create("w1".into());
        let error = run(&dir.path().join("missing.sock"), &cmd).unwrap_err();
        assert!(error.is_not_running(), "got {error}");
    }

    #[test]
    fn a_path_is_kept_and_a_missing_path_is_dropped() {
        let cmd = TabCommand::SplitRight("w1:p1".into());
        assert_eq!(
            socket_or_warn(Some(PathBuf::from("herdr.sock")), &cmd),
            Some(PathBuf::from("herdr.sock"))
        );
        assert_eq!(socket_or_warn(None, &cmd), None);
    }
}
