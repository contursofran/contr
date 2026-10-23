//! Commands that change herdr's state. Each opens one connection, sends one
//! request and reads one reply.

use std::path::Path;
use std::time::Duration;

use serde_json::{Value, json};

use crate::wire::{WireError, exchange, expect_type};

/// Moves herdr's focus to `pane_id` (`pane.focus`). herdr also switches to the
/// tab and workspace that hold the pane.
pub fn focus_pane(path: &Path, pane_id: &str, timeout: Duration) -> Result<(), WireError> {
    let result = exchange(
        path,
        timeout,
        "contr-focus",
        "pane.focus",
        focus_pane_params(pane_id),
    )?;
    expect_type(&result, "pane_info")
}

pub(crate) fn focus_pane_params(pane_id: &str) -> Value {
    json!({ "pane_id": pane_id })
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use crate::wire::fake::{serve, tempdir};

    use super::*;

    const T: Duration = Duration::from_secs(2);
    const SHORT: Duration = Duration::from_millis(200);

    const PANE_INFO: &str =
        r#"{"id":"contr-focus","result":{"type":"pane_info","pane":{"pane_id":"w1:p1"}}}"#;

    #[test]
    fn focus_pane_params_is_the_pane_target() {
        assert_eq!(focus_pane_params("w1:p2"), json!({"pane_id": "w1:p2"}));
    }

    #[test]
    fn focus_pane_sends_pane_focus_and_accepts_pane_info() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[PANE_INFO], Duration::ZERO);
        focus_pane(&path, "w1:p1", T).expect("focused");
        let request: Value =
            serde_json::from_str(&server.join().expect("server")).expect("request is JSON");
        assert_eq!(request["id"], "contr-focus");
        assert_eq!(request["method"], "pane.focus");
        assert_eq!(request["params"]["pane_id"], "w1:p1");
    }

    #[test]
    fn focus_pane_maps_pane_not_found_to_a_server_error() {
        let dir = tempdir();
        let reply =
            r#"{"id":"contr-focus","error":{"code":"pane_not_found","message":"no such pane"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = focus_pane(&path, "w9:p9", T).expect_err("missing pane");
        assert!(matches!(err, WireError::Server { ref code, .. } if code == "pane_not_found"));
        server.join().expect("server");
    }

    #[test]
    fn focus_pane_rejects_a_result_of_another_type() {
        let dir = tempdir();
        let reply = r#"{"id":"contr-focus","result":{"type":"pong"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = focus_pane(&path, "w1:p1", T).expect_err("wrong type");
        assert!(matches!(err, WireError::Unexpected(_)));
        server.join().expect("server");
    }

    #[test]
    fn focus_pane_times_out_when_server_is_silent() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[], Duration::from_millis(600));
        let err = focus_pane(&path, "w1:p1", SHORT).expect_err("silent server");
        assert!(matches!(err, WireError::Io(ref e) if e.kind() == std::io::ErrorKind::TimedOut));
        assert!(!err.is_not_running());
        server.join().expect("server");
    }

    #[test]
    fn focus_pane_missing_socket_is_not_running() {
        let dir = tempdir();
        let err = focus_pane(&dir.path().join("missing.sock"), "w1:p1", T).expect_err("no socket");
        assert!(err.is_not_running());
    }
}
