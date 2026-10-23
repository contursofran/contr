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

/// `tab.focus`: makes `tab_id` herdr's focused tab (and its workspace the focused workspace).
pub fn focus_tab(path: &Path, tab_id: &str, timeout: Duration) -> Result<(), WireError> {
    let result = exchange(
        path,
        timeout,
        "contr-tab-focus",
        "tab.focus",
        focus_tab_params(tab_id),
    )?;
    expect_type(&result, "tab_info")
}

pub(crate) fn focus_tab_params(tab_id: &str) -> Value {
    json!({ "tab_id": tab_id })
}

/// `tab.create` in `workspace_id`, focused.
pub fn create_tab(path: &Path, workspace_id: &str, timeout: Duration) -> Result<(), WireError> {
    let result = exchange(
        path,
        timeout,
        "contr-tab-create",
        "tab.create",
        create_tab_params(workspace_id),
    )?;
    expect_type(&result, "tab_created")
}

pub(crate) fn create_tab_params(workspace_id: &str) -> Value {
    json!({ "workspace_id": workspace_id, "focus": true })
}

/// `pane.split` of `pane_id` to the right (D20), the new pane focused.
pub fn split_right(path: &Path, pane_id: &str, timeout: Duration) -> Result<(), WireError> {
    let result = exchange(
        path,
        timeout,
        "contr-split",
        "pane.split",
        split_right_params(pane_id),
    )?;
    expect_type(&result, "pane_info")
}

pub(crate) fn split_right_params(pane_id: &str) -> Value {
    json!({ "target_pane_id": pane_id, "direction": "right", "focus": true })
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
    const TAB_INFO: &str =
        r#"{"id":"contr-tab-focus","result":{"type":"tab_info","tab":{"tab_id":"w1:t1"}}}"#;
    const TAB_CREATED: &str = r#"{"id":"contr-tab-create","result":{"type":"tab_created","tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p3"}}}"#;
    const SPLIT_PANE_INFO: &str =
        r#"{"id":"contr-split","result":{"type":"pane_info","pane":{"pane_id":"w1:p3"}}}"#;

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

    #[test]
    fn focus_tab_params_is_the_tab_target() {
        assert_eq!(focus_tab_params("w1:t2"), json!({"tab_id": "w1:t2"}));
    }

    #[test]
    fn create_tab_params_is_the_workspace_and_focus() {
        assert_eq!(
            create_tab_params("w1"),
            json!({"workspace_id": "w1", "focus": true})
        );
    }

    #[test]
    fn split_right_params_is_the_pane_to_the_right_focused() {
        assert_eq!(
            split_right_params("w1:p2"),
            json!({"target_pane_id": "w1:p2", "direction": "right", "focus": true})
        );
    }

    #[test]
    fn focus_tab_sends_tab_focus_and_accepts_tab_info() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[TAB_INFO], Duration::ZERO);
        focus_tab(&path, "w1:t1", T).expect("focused");
        let request: Value =
            serde_json::from_str(&server.join().expect("server")).expect("request is JSON");
        assert_eq!(request["id"], "contr-tab-focus");
        assert_eq!(request["method"], "tab.focus");
        assert_eq!(request["params"]["tab_id"], "w1:t1");
    }

    #[test]
    fn focus_tab_maps_tab_not_found_to_a_server_error() {
        let dir = tempdir();
        let reply =
            r#"{"id":"contr-tab-focus","error":{"code":"tab_not_found","message":"no such tab"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = focus_tab(&path, "w9:t9", T).expect_err("missing tab");
        assert!(matches!(err, WireError::Server { ref code, .. } if code == "tab_not_found"));
        server.join().expect("server");
    }

    #[test]
    fn focus_tab_missing_socket_is_not_running() {
        let dir = tempdir();
        let err = focus_tab(&dir.path().join("missing.sock"), "w1:t1", T).expect_err("no socket");
        assert!(err.is_not_running());
    }

    #[test]
    fn create_tab_sends_tab_create_focused_and_accepts_tab_created() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[TAB_CREATED], Duration::ZERO);
        create_tab(&path, "w1", T).expect("created");
        let request: Value =
            serde_json::from_str(&server.join().expect("server")).expect("request is JSON");
        assert_eq!(request["id"], "contr-tab-create");
        assert_eq!(request["method"], "tab.create");
        assert_eq!(request["params"]["workspace_id"], "w1");
        assert_eq!(request["params"]["focus"], true);
    }

    #[test]
    fn create_tab_maps_workspace_not_found_to_a_server_error() {
        let dir = tempdir();
        let reply = r#"{"id":"contr-tab-create","error":{"code":"workspace_not_found","message":"no such workspace"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = create_tab(&path, "w9", T).expect_err("missing workspace");
        assert!(matches!(err, WireError::Server { ref code, .. } if code == "workspace_not_found"));
        server.join().expect("server");
    }

    #[test]
    fn create_tab_rejects_a_result_of_another_type() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[TAB_INFO], Duration::ZERO);
        let err = create_tab(&path, "w1", T).expect_err("wrong type");
        assert!(matches!(err, WireError::Unexpected(_)));
        server.join().expect("server");
    }

    #[test]
    fn split_right_sends_pane_split_right_and_accepts_pane_info() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[SPLIT_PANE_INFO], Duration::ZERO);
        split_right(&path, "w1:p2", T).expect("split");
        let request: Value =
            serde_json::from_str(&server.join().expect("server")).expect("request is JSON");
        assert_eq!(request["id"], "contr-split");
        assert_eq!(request["method"], "pane.split");
        assert_eq!(request["params"]["target_pane_id"], "w1:p2");
        assert_eq!(request["params"]["direction"], "right");
        assert_eq!(request["params"]["focus"], true);
    }

    #[test]
    fn split_right_maps_pane_not_found_to_a_server_error() {
        let dir = tempdir();
        let reply =
            r#"{"id":"contr-split","error":{"code":"pane_not_found","message":"no such pane"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = split_right(&path, "w9:p9", T).expect_err("missing pane");
        assert!(matches!(err, WireError::Server { ref code, .. } if code == "pane_not_found"));
        server.join().expect("server");
    }
}
