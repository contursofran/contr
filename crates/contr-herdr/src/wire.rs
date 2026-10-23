//! Wire format of herdr's JSON API: request lines, replies, the snapshot
//! shape and the event subscription. Every call here is a read.

use std::error::Error;
use std::fmt;
use std::io::{self, Read, Write};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use serde::de::DeserializeOwned;
use serde_json::{Value, json};

/// Protocol number contr was written against (herdr 0.9.3).
pub const API_PROTOCOL: u32 = 22;

/// Longest JSON line accepted from herdr, in bytes, without the newline.
pub const MAX_LINE: usize = 8 * 1024 * 1024;

/// herdr's API socket: `HERDR_SOCKET_PATH`, else `$XDG_CONFIG_HOME/herdr/herdr.sock`,
/// else `$HOME/.config/herdr/herdr.sock`. `None` when none of these is set.
pub fn api_socket_path() -> Option<PathBuf> {
    socket_path_from(|key| std::env::var(key).ok())
}

/// Testable core of [`api_socket_path`]. Empty values count as unset.
pub(crate) fn socket_path_from(env: impl Fn(&str) -> Option<String>) -> Option<PathBuf> {
    let value = |key: &str| env(key).filter(|v| !v.is_empty());
    if let Some(path) = value("HERDR_SOCKET_PATH") {
        return Some(PathBuf::from(path));
    }
    let config_home = value("XDG_CONFIG_HOME")
        .map(PathBuf::from)
        .or_else(|| value("HOME").map(|home| PathBuf::from(home).join(".config")));
    config_home.map(|dir| dir.join("herdr").join("herdr.sock"))
}

/// Agent state as herdr reports it. Any other string maps to `Unknown`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum WireStatus {
    Idle,
    Working,
    Blocked,
    Done,
    #[serde(other)]
    Unknown,
}

/// `ping` result.
#[derive(Debug, Clone, serde::Deserialize)]
pub struct Pong {
    pub version: String,
    pub protocol: u32,
}

/// The `snapshot` object of a `session_snapshot` result.
#[derive(Debug, Clone, Default, serde::Deserialize)]
pub struct WireSnapshot {
    pub version: String,
    pub protocol: u32,
    #[serde(default)]
    pub focused_workspace_id: Option<String>,
    #[serde(default)]
    pub focused_tab_id: Option<String>,
    #[serde(default)]
    pub focused_pane_id: Option<String>,
    #[serde(default)]
    pub workspaces: Vec<WireWorkspace>,
    #[serde(default)]
    pub tabs: Vec<WireTab>,
    #[serde(default)]
    pub panes: Vec<WirePane>,
}

#[derive(Debug, Clone, serde::Deserialize)]
pub struct WireWorkspace {
    pub workspace_id: String,
    pub label: String,
    #[serde(default)]
    pub focused: bool,
    pub agent_status: WireStatus,
}

#[derive(Debug, Clone, serde::Deserialize)]
pub struct WireTab {
    pub tab_id: String,
    pub workspace_id: String,
    pub label: String,
    #[serde(default)]
    pub focused: bool,
    pub agent_status: WireStatus,
}

#[derive(Debug, Clone, serde::Deserialize)]
pub struct WirePane {
    pub pane_id: String,
    pub tab_id: String,
    pub workspace_id: String,
    #[serde(default)]
    pub focused: bool,
    pub agent_status: WireStatus,
    #[serde(default)]
    pub agent: Option<String>,
    #[serde(default)]
    pub display_agent: Option<String>,
    #[serde(default)]
    pub label: Option<String>,
    #[serde(default)]
    pub cwd: Option<String>,
}

/// Why a request to herdr failed.
#[derive(Debug)]
pub enum WireError {
    /// Connect, read or write failed.
    Io(io::Error),
    /// The peer closed the stream.
    Closed,
    /// herdr answered with `{"error":{code,message}}`.
    Server { code: String, message: String },
    /// The reply did not parse, had the wrong shape, or a line was too long.
    Unexpected(String),
}

impl WireError {
    /// True when no herdr is listening: the socket file is missing or refuses connections.
    pub fn is_not_running(&self) -> bool {
        matches!(
            self,
            Self::Io(e) if matches!(
                e.kind(),
                io::ErrorKind::NotFound
                    | io::ErrorKind::ConnectionRefused
                    | io::ErrorKind::AddrNotAvailable
            )
        )
    }
}

impl fmt::Display for WireError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(e) => write!(f, "{e}"),
            Self::Closed => f.write_str("herdr closed the connection"),
            Self::Server { code, message } => write!(f, "herdr error {code}: {message}"),
            Self::Unexpected(what) => f.write_str(what),
        }
    }
}

impl Error for WireError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::Io(e) => Some(e),
            _ => None,
        }
    }
}

fn unexpected(what: impl Into<String>) -> WireError {
    WireError::Unexpected(what.into())
}

fn timed_out() -> WireError {
    WireError::Io(io::Error::from(io::ErrorKind::TimedOut))
}

/// One request line: `{"id","method","params"}` followed by the newline that ends it.
pub fn request_line(id: &str, method: &str, params: Value) -> String {
    let mut line = json!({ "id": id, "method": method, "params": params }).to_string();
    line.push('\n');
    line
}

#[derive(serde::Deserialize)]
struct Reply {
    result: Option<Value>,
    error: Option<ServerError>,
}

#[derive(serde::Deserialize)]
struct ServerError {
    code: String,
    message: String,
}

/// The `result` of one reply line, or the server's error.
pub(crate) fn parse_reply(line: &str) -> Result<Value, WireError> {
    let reply: Reply = serde_json::from_str(line)
        .map_err(|e| unexpected(format!("reply is not a JSON object: {e}")))?;
    match (reply.error, reply.result) {
        (Some(error), _) => Err(WireError::Server {
            code: error.code,
            message: error.message,
        }),
        (None, Some(result)) => Ok(result),
        (None, None) => Err(unexpected("reply has neither result nor error")),
    }
}

fn connect(path: &Path, read: Duration, write: Duration) -> Result<UnixStream, WireError> {
    let stream = UnixStream::connect(path).map_err(WireError::Io)?;
    stream.set_read_timeout(Some(read)).map_err(WireError::Io)?;
    stream
        .set_write_timeout(Some(write))
        .map_err(WireError::Io)?;
    Ok(stream)
}

/// One request on a fresh connection; returns the `result` of the reply.
fn exchange(
    path: &Path,
    timeout: Duration,
    id: &str,
    method: &str,
    params: Value,
) -> Result<Value, WireError> {
    let mut stream = connect(path, timeout, timeout)?;
    stream
        .write_all(request_line(id, method, params).as_bytes())
        .map_err(WireError::Io)?;
    // The reply id is not compared with `id`: one request per connection, so the
    // first line on it is the reply to that request.
    let line = LineReader::new(stream).next_line()?.ok_or_else(timed_out)?;
    parse_reply(&line)
}

fn expect_type(result: &Value, kind: &str) -> Result<(), WireError> {
    let found = result.get("type").and_then(Value::as_str).unwrap_or("none");
    if found == kind {
        Ok(())
    } else {
        Err(unexpected(format!(
            "expected result type {kind}, got {found}"
        )))
    }
}

fn from_result<T: DeserializeOwned>(result: Value) -> Result<T, WireError> {
    serde_json::from_value(result)
        .map_err(|e| unexpected(format!("result has an unexpected shape: {e}")))
}

#[derive(serde::Deserialize)]
struct SnapshotResult {
    snapshot: WireSnapshot,
}

/// Asks herdr for its version and protocol (`ping`). One connection per call.
/// `timeout` bounds each read and each write, not the whole call: a peer that
/// sends one byte at a time can keep the call going longer than `timeout`.
pub fn ping(path: &Path, timeout: Duration) -> Result<Pong, WireError> {
    let result = exchange(path, timeout, "contr-ping", "ping", json!({}))?;
    expect_type(&result, "pong")?;
    from_result(result)
}

/// Reads the whole session (`session.snapshot`). One connection per call.
/// `timeout` bounds each read and each write, not the whole call (see [`ping`]).
pub fn snapshot(path: &Path, timeout: Duration) -> Result<WireSnapshot, WireError> {
    let result = exchange(
        path,
        timeout,
        "contr-snapshot",
        "session.snapshot",
        json!({}),
    )?;
    expect_type(&result, "session_snapshot")?;
    let body: SnapshotResult = from_result(result)?;
    Ok(body.snapshot)
}

/// Splits a byte stream into newline-terminated lines and keeps partial lines
/// between calls, so a read timeout never loses bytes.
#[derive(Debug)]
pub struct LineReader<R> {
    inner: R,
    buf: Vec<u8>,
}

impl<R: Read> LineReader<R> {
    pub fn new(inner: R) -> Self {
        Self {
            inner,
            buf: Vec::new(),
        }
    }

    /// `Ok(Some(line))` for a full line without its `\n`. `Ok(None)` on a read
    /// timeout (`WouldBlock` or `TimedOut`). `Err(Closed)` on end of stream.
    /// `Err(Unexpected)` when a line exceeds [`MAX_LINE`] or is not UTF-8.
    pub fn next_line(&mut self) -> Result<Option<String>, WireError> {
        // Bytes at the front of `buf` already known to hold no newline.
        let mut scanned = 0;
        loop {
            if let Some(offset) = self.buf[scanned..].iter().position(|&b| b == b'\n') {
                let end = scanned + offset;
                if end > MAX_LINE {
                    return Err(overlong());
                }
                let rest = self.buf.split_off(end + 1);
                let mut line = std::mem::replace(&mut self.buf, rest);
                line.pop();
                return String::from_utf8(line)
                    .map(Some)
                    .map_err(|_| unexpected("herdr sent a line that is not UTF-8"));
            }
            scanned = self.buf.len();
            if scanned > MAX_LINE {
                return Err(overlong());
            }
            let mut chunk = [0_u8; 8192];
            match self.inner.read(&mut chunk) {
                Ok(0) => return Err(WireError::Closed),
                Ok(n) => self.buf.extend_from_slice(&chunk[..n]),
                Err(e) if e.kind() == io::ErrorKind::Interrupted => {}
                Err(e)
                    if matches!(
                        e.kind(),
                        io::ErrorKind::WouldBlock | io::ErrorKind::TimedOut
                    ) =>
                {
                    return Ok(None);
                }
                Err(e) => return Err(WireError::Io(e)),
            }
        }
    }
}

fn overlong() -> WireError {
    unexpected(format!("herdr sent a line longer than {MAX_LINE} bytes"))
}

/// Event kinds subscribed to globally. Agent status is not one of them:
/// it needs one `pane.agent_status_changed` per pane.
pub const GLOBAL_EVENTS: &[&str] = &[
    "workspace.created",
    "workspace.updated",
    "workspace.renamed",
    "workspace.moved",
    "workspace.reordered",
    "workspace.closed",
    "workspace.focused",
    "tab.created",
    "tab.closed",
    "tab.focused",
    "tab.renamed",
    "tab.moved",
    "pane.created",
    "pane.closed",
    "pane.updated",
    "pane.focused",
    "pane.moved",
    "pane.exited",
    "pane.agent_detected",
];

/// An open `events.subscribe` stream. Each event only means "something changed".
#[derive(Debug)]
pub struct Subscription {
    reader: LineReader<UnixStream>,
}

impl Subscription {
    /// Connects and subscribes to [`GLOBAL_EVENTS`] plus one `pane.agent_status_changed`
    /// per pane id. Waits up to `timeout` for `subscription_started`; a server error
    /// (for example `pane_not_found`) fails. Then reads wait up to `poll` per call.
    /// `timeout` bounds the write and the ack wait; the ack wait is checked after
    /// each `poll`-long read, so it can overrun `timeout` by up to one `poll`.
    /// A zero `poll` fails with `Io(InvalidInput)`: the socket rejects a zero read
    /// timeout.
    pub fn open(
        path: &Path,
        pane_ids: &[String],
        timeout: Duration,
        poll: Duration,
    ) -> Result<Self, WireError> {
        // Reads use `poll` from the start; the ack is awaited against `timeout`
        // below, so no socket option changes after the ack.
        let mut stream = connect(path, poll, timeout)?;
        let mut subscriptions: Vec<Value> = GLOBAL_EVENTS
            .iter()
            .map(|kind| json!({ "type": kind }))
            .collect();
        subscriptions.extend(
            pane_ids
                .iter()
                .map(|id| json!({ "type": "pane.agent_status_changed", "pane_id": id })),
        );
        let request = request_line(
            "contr-sub",
            "events.subscribe",
            json!({ "subscriptions": subscriptions }),
        );
        stream
            .write_all(request.as_bytes())
            .map_err(WireError::Io)?;
        let mut reader = LineReader::new(stream);
        let deadline = Instant::now() + timeout;
        let ack = loop {
            if let Some(line) = reader.next_line()? {
                break line;
            }
            if Instant::now() >= deadline {
                return Err(timed_out());
            }
        };
        // The ack id is not compared with `contr-sub`: one subscription per connection.
        expect_type(&parse_reply(&ack)?, "subscription_started")?;
        Ok(Self { reader })
    }

    /// `Ok(true)`: an event line arrived. `Ok(false)`: nothing within the poll
    /// interval. `Err(Closed)`: herdr ended the stream.
    pub fn next_event(&mut self) -> Result<bool, WireError> {
        Ok(self.reader.next_line()?.is_some())
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use std::collections::{HashMap, VecDeque};
    use std::io::{BufRead, BufReader};
    use std::os::unix::net::UnixListener;
    use std::thread::{self, JoinHandle};

    use super::*;

    const T: Duration = Duration::from_secs(2);
    const SHORT: Duration = Duration::from_millis(200);
    const POLL: Duration = Duration::from_millis(50);

    const PONG: &str = r#"{"id":"contr-ping","result":{"type":"pong","version":"0.9.3","protocol":22,"capabilities":{"endpoint_protocol_generation":1}}}"#;
    const ACK: &str = r#"{"id":"contr-sub","result":{"type":"subscription_started"}}"#;
    const EVENT: &str = r#"{"event":"pane.agent_status_changed","data":{"type":"pane.agent_status_changed","agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"}}"#;

    /// Hand-written `snapshot` object, shaped after the keys herdr documents.
    const SNAPSHOT: &str = r#"{
        "version": "0.9.3",
        "protocol": 22,
        "focused_workspace_id": "w1",
        "focused_tab_id": "w1:t1",
        "focused_pane_id": "w1:p1",
        "workspaces": [
            {"workspace_id": "w1", "number": 1, "label": "contr", "focused": true, "pane_count": 2, "tab_count": 2, "active_tab_id": "w1:t1", "agent_status": "working"},
            {"workspace_id": "w2", "number": 2, "label": "notes", "focused": false, "pane_count": 2, "tab_count": 1, "active_tab_id": "w2:t2", "agent_status": "idle"}
        ],
        "tabs": [
            {"tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "planner", "focused": true, "pane_count": 2, "agent_status": "working"},
            {"tab_id": "w1:t2", "workspace_id": "w1", "number": 2, "label": "build", "focused": false, "pane_count": 1, "agent_status": "done"},
            {"tab_id": "w2:t2", "workspace_id": "w2", "number": 1, "label": "main", "focused": false, "pane_count": 2, "agent_status": "idle"}
        ],
        "panes": [
            {"pane_id": "w1:p1", "tab_id": "w1:t1", "workspace_id": "w1", "terminal_id": "t1", "focused": true, "revision": 7, "agent_status": "working", "agent": "claude", "display_agent": "claude-code", "label": "api", "cwd": "/srv/example", "scroll": {"offset": 0}},
            {"pane_id": "w1:p2", "tab_id": "w1:t1", "workspace_id": "w1", "terminal_id": "t2", "focused": false, "revision": 3, "agent_status": "unknown"},
            {"pane_id": "w1:p3", "tab_id": "w1:t2", "workspace_id": "w1", "terminal_id": "t3", "focused": false, "revision": 9, "agent_status": "done", "agent": "codex"},
            {"pane_id": "w2:p1", "tab_id": "w2:t2", "workspace_id": "w2", "terminal_id": "t4", "focused": false, "revision": 1, "agent_status": "idle"}
        ],
        "agents": [],
        "layouts": []
    }"#;

    const EMPTY_SNAPSHOT: &str = r#"{"version":"0.9.3","protocol":22,"workspaces":[],"tabs":[],"panes":[],"agents":[],"layouts":[]}"#;

    fn env_of(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let vars: HashMap<String, String> = pairs
            .iter()
            .map(|&(k, v)| (k.to_owned(), v.to_owned()))
            .collect();
        move |key| vars.get(key).cloned()
    }

    fn tempdir() -> tempfile::TempDir {
        tempfile::Builder::new()
            .prefix("ch")
            .tempdir_in("/tmp")
            .expect("temp dir")
    }

    /// Fake herdr: binds `herdr.sock` in `dir`, accepts one client, reads its request
    /// line, writes `replies` (one line each), waits `hold`, then closes.
    /// The thread returns the request line.
    fn serve(dir: &Path, replies: &[&str], hold: Duration) -> (PathBuf, JoinHandle<String>) {
        let path = dir.join("herdr.sock");
        let listener = UnixListener::bind(&path).expect("bind fake herdr");
        let replies: Vec<String> = replies.iter().map(|line| format!("{line}\n")).collect();
        let handle = thread::spawn(move || {
            let (stream, _) = listener.accept().expect("accept");
            let mut request = String::new();
            BufReader::new(&stream)
                .read_line(&mut request)
                .expect("read request");
            let mut writer = &stream;
            for reply in &replies {
                writer.write_all(reply.as_bytes()).expect("write reply");
            }
            thread::sleep(hold);
            request.trim_end().to_owned()
        });
        (path, handle)
    }

    fn request_json(request: &str) -> Value {
        serde_json::from_str(request).expect("request is JSON")
    }

    #[test]
    fn socket_path_prefers_herdr_socket_path() {
        let env = env_of(&[
            ("HERDR_SOCKET_PATH", "/s/herdr.sock"),
            ("XDG_CONFIG_HOME", "/x"),
            ("HOME", "/h"),
        ]);
        assert_eq!(socket_path_from(env), Some(PathBuf::from("/s/herdr.sock")));
    }

    #[test]
    fn socket_path_falls_back_to_xdg_config_home() {
        let env = env_of(&[("XDG_CONFIG_HOME", "/x"), ("HOME", "/h")]);
        assert_eq!(
            socket_path_from(env),
            Some(PathBuf::from("/x/herdr/herdr.sock"))
        );
    }

    #[test]
    fn socket_path_falls_back_to_home() {
        let env = env_of(&[("HOME", "/h")]);
        assert_eq!(
            socket_path_from(env),
            Some(PathBuf::from("/h/.config/herdr/herdr.sock"))
        );
    }

    #[test]
    fn socket_path_ignores_empty_values() {
        let env = env_of(&[
            ("HERDR_SOCKET_PATH", ""),
            ("XDG_CONFIG_HOME", ""),
            ("HOME", "/h"),
        ]);
        assert_eq!(
            socket_path_from(env),
            Some(PathBuf::from("/h/.config/herdr/herdr.sock"))
        );
    }

    #[test]
    fn socket_path_is_none_without_home_or_xdg() {
        assert_eq!(socket_path_from(env_of(&[])), None);
        assert_eq!(socket_path_from(env_of(&[("HERDR_SOCKET_PATH", "")])), None);
    }

    #[test]
    fn request_line_is_one_json_line() {
        let line = request_line("contr-ping", "ping", json!({}));
        assert!(line.ends_with('\n'));
        assert_eq!(line.matches('\n').count(), 1);
        let parsed: Value = serde_json::from_str(line.trim_end()).expect("json");
        assert_eq!(
            parsed,
            json!({"id": "contr-ping", "method": "ping", "params": {}})
        );
    }

    #[test]
    fn parse_reply_returns_result() {
        let result = parse_reply(r#"{"id":"r1","result":{"type":"pong"}}"#).expect("result");
        assert_eq!(result, json!({"type": "pong"}));
    }

    #[test]
    fn parse_reply_maps_server_error() {
        let err = parse_reply(
            r#"{"id":"r1","error":{"code":"pane_not_found","message":"pane w9:p99 not found"}}"#,
        )
        .expect_err("server error");
        assert!(matches!(
            err,
            WireError::Server { ref code, ref message }
                if code == "pane_not_found" && message == "pane w9:p99 not found"
        ));
    }

    #[test]
    fn parse_reply_rejects_garbage_and_replies_without_result() {
        assert!(matches!(
            parse_reply("not json"),
            Err(WireError::Unexpected(_))
        ));
        assert!(matches!(
            parse_reply(r#"{"id":"r1"}"#),
            Err(WireError::Unexpected(_))
        ));
        assert!(matches!(
            parse_reply(r#"{"id":"r1","result":null}"#),
            Err(WireError::Unexpected(_))
        ));
    }

    #[test]
    fn wire_error_messages_are_plain() {
        assert_eq!(WireError::Closed.to_string(), "herdr closed the connection");
        let server = WireError::Server {
            code: "pane_not_found".into(),
            message: "pane w9:p99 not found".into(),
        };
        assert_eq!(
            server.to_string(),
            "herdr error pane_not_found: pane w9:p99 not found"
        );
        assert_eq!(WireError::Unexpected("bad".into()).to_string(), "bad");
    }

    #[test]
    fn is_not_running_only_for_missing_or_refused_sockets() {
        for kind in [
            io::ErrorKind::NotFound,
            io::ErrorKind::ConnectionRefused,
            io::ErrorKind::AddrNotAvailable,
        ] {
            assert!(
                WireError::Io(io::Error::from(kind)).is_not_running(),
                "{kind:?}"
            );
        }
        for kind in [io::ErrorKind::TimedOut, io::ErrorKind::PermissionDenied] {
            assert!(
                !WireError::Io(io::Error::from(kind)).is_not_running(),
                "{kind:?}"
            );
        }
        assert!(!WireError::Closed.is_not_running());
        assert!(!WireError::Unexpected(String::new()).is_not_running());
    }

    #[test]
    fn snapshot_fixture_has_two_workspaces_three_tabs_four_panes() {
        let snap: WireSnapshot = serde_json::from_str(SNAPSHOT).expect("fixture");
        assert_eq!((snap.version.as_str(), snap.protocol), ("0.9.3", 22));
        assert_eq!(snap.focused_workspace_id.as_deref(), Some("w1"));
        assert_eq!(snap.focused_pane_id.as_deref(), Some("w1:p1"));
        assert_eq!(snap.workspaces.len(), 2);
        assert_eq!(snap.workspaces[0].label, "contr");
        assert!(snap.workspaces[0].focused);
        assert_eq!(snap.workspaces[1].agent_status, WireStatus::Idle);
        assert_eq!(snap.tabs.len(), 3);
        assert_eq!(snap.tabs[1].agent_status, WireStatus::Done);
        assert_eq!(snap.panes.len(), 4);
    }

    #[test]
    fn snapshot_fixture_keeps_agent_fields_and_done_status() {
        let snap: WireSnapshot = serde_json::from_str(SNAPSHOT).expect("fixture");
        let first = &snap.panes[0];
        assert_eq!(first.agent.as_deref(), Some("claude"));
        assert_eq!(first.display_agent.as_deref(), Some("claude-code"));
        assert!(first.focused);
        assert_eq!(snap.panes[1].agent, None);
        assert_eq!(snap.panes[1].agent_status, WireStatus::Unknown);
        assert_eq!(snap.panes[2].agent_status, WireStatus::Done);
    }

    #[test]
    fn empty_server_snapshot_without_focus_parses() {
        let snap: WireSnapshot = serde_json::from_str(EMPTY_SNAPSHOT).expect("empty");
        assert_eq!(snap.focused_workspace_id, None);
        assert_eq!(snap.focused_tab_id, None);
        assert_eq!(snap.focused_pane_id, None);
        assert!(snap.workspaces.is_empty());
        assert!(snap.tabs.is_empty());
        assert!(snap.panes.is_empty());
    }

    #[test]
    fn wire_status_maps_five_states_and_unknown_strings() {
        let cases = [
            ("idle", WireStatus::Idle),
            ("working", WireStatus::Working),
            ("blocked", WireStatus::Blocked),
            ("done", WireStatus::Done),
            ("unknown", WireStatus::Unknown),
            ("sleeping", WireStatus::Unknown),
        ];
        for (text, want) in cases {
            let got: WireStatus = serde_json::from_str(&format!("\"{text}\"")).expect("status");
            assert_eq!(got, want, "{text}");
        }
    }

    /// One scripted read: data, or a failure of the given kind.
    enum Step {
        Data(&'static [u8]),
        Fail(io::ErrorKind),
    }

    /// Yields the scripted reads in order. An exhausted script is end of stream.
    struct Script(VecDeque<Step>);

    impl Read for Script {
        fn read(&mut self, out: &mut [u8]) -> io::Result<usize> {
            match self.0.pop_front() {
                Some(Step::Data(bytes)) => {
                    out[..bytes.len()].copy_from_slice(bytes);
                    Ok(bytes.len())
                }
                Some(Step::Fail(kind)) => Err(kind.into()),
                None => Ok(0),
            }
        }
    }

    #[test]
    fn line_reader_keeps_partial_line_across_timeout() {
        let script = Script(VecDeque::from([
            Step::Data(&b"{\"a\":1}\n{\"b\""[..]),
            Step::Fail(io::ErrorKind::WouldBlock),
            Step::Data(&b":2}\n"[..]),
        ]));
        let mut reader = LineReader::new(script);
        assert_eq!(
            reader.next_line().expect("line 1"),
            Some(r#"{"a":1}"#.to_owned())
        );
        assert_eq!(reader.next_line().expect("timeout"), None);
        assert_eq!(
            reader.next_line().expect("line 2"),
            Some(r#"{"b":2}"#.to_owned())
        );
        assert!(matches!(reader.next_line(), Err(WireError::Closed)));
    }

    #[test]
    fn line_reader_retries_interrupted_and_maps_other_read_errors() {
        let interrupted = Script(VecDeque::from([
            Step::Fail(io::ErrorKind::Interrupted),
            Step::Data(&b"ok\n"[..]),
        ]));
        assert_eq!(
            LineReader::new(interrupted).next_line().expect("retried"),
            Some("ok".to_owned())
        );

        let timed_out = Script(VecDeque::from([Step::Fail(io::ErrorKind::TimedOut)]));
        assert_eq!(
            LineReader::new(timed_out).next_line().expect("timeout"),
            None
        );

        let reset = Script(VecDeque::from([Step::Fail(io::ErrorKind::ConnectionReset)]));
        let err = LineReader::new(reset).next_line().expect_err("reset");
        assert!(matches!(
            err,
            WireError::Io(ref e) if e.kind() == io::ErrorKind::ConnectionReset
        ));
    }

    #[test]
    fn line_reader_reports_end_of_stream_as_closed() {
        let mut reader = LineReader::new(&b""[..]);
        assert!(matches!(reader.next_line(), Err(WireError::Closed)));
    }

    #[test]
    fn line_reader_rejects_overlong_line() {
        let mut reader = LineReader::new(io::repeat(b'a'));
        assert!(matches!(reader.next_line(), Err(WireError::Unexpected(_))));
    }

    #[test]
    fn line_reader_rejects_non_utf8_line() {
        let mut reader = LineReader::new(&b"\xff\n"[..]);
        assert!(matches!(reader.next_line(), Err(WireError::Unexpected(_))));
    }

    #[test]
    fn ping_parses_pong_and_sends_ping() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[PONG], Duration::ZERO);
        let pong = ping(&path, T).expect("pong");
        assert_eq!(pong.version, "0.9.3");
        assert_eq!(pong.protocol, 22);
        let request = request_json(&server.join().expect("server"));
        assert_eq!(request["id"], "contr-ping");
        assert_eq!(request["method"], "ping");
    }

    #[test]
    fn ping_rejects_a_result_of_another_type() {
        let dir = tempdir();
        let reply = r#"{"id":"contr-ping","result":{"type":"session_snapshot"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = ping(&path, T).expect_err("wrong type");
        assert!(matches!(err, WireError::Unexpected(_)));
        server.join().expect("server");
    }

    #[test]
    fn ping_missing_socket_is_not_running() {
        let dir = tempdir();
        let err = ping(&dir.path().join("missing.sock"), T).expect_err("no socket");
        assert!(err.is_not_running());
    }

    #[test]
    fn ping_stale_socket_file_is_not_running() {
        let dir = tempdir();
        let path = dir.path().join("herdr.sock");
        let listener = UnixListener::bind(&path).expect("bind");
        drop(listener); // the socket file stays, nobody listens
        let err = ping(&path, T).expect_err("refused");
        assert!(err.is_not_running());
    }

    #[test]
    fn ping_times_out_when_server_is_silent() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[], Duration::from_millis(600));
        let err = ping(&path, SHORT).expect_err("silent server");
        assert!(matches!(err, WireError::Io(ref e) if e.kind() == io::ErrorKind::TimedOut));
        assert!(!err.is_not_running());
        server.join().expect("server");
    }

    #[test]
    fn snapshot_parses_fixture_over_a_socket() {
        let dir = tempdir();
        // A reply is one line: flatten the pretty-printed fixture first.
        let flat = SNAPSHOT.replace('\n', " ");
        let reply = format!(
            r#"{{"id":"contr-snapshot","result":{{"type":"session_snapshot","snapshot":{flat}}}}}"#
        );
        let (path, server) = serve(dir.path(), &[reply.as_str()], Duration::ZERO);
        let snap = snapshot(&path, T).expect("snapshot");
        assert_eq!(snap.workspaces.len(), 2);
        assert_eq!(snap.panes.len(), 4);
        let request = request_json(&server.join().expect("server"));
        assert_eq!(request["method"], "session.snapshot");
    }

    #[test]
    fn snapshot_rejects_a_body_without_required_fields() {
        let dir = tempdir();
        let reply = r#"{"id":"contr-snapshot","result":{"type":"session_snapshot","snapshot":{"version":"0.9.3"}}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = snapshot(&path, T).expect_err("malformed");
        assert!(matches!(err, WireError::Unexpected(_)));
        server.join().expect("server");
    }

    #[test]
    fn snapshot_rejects_a_result_of_another_type() {
        let dir = tempdir();
        let reply =
            r#"{"id":"contr-snapshot","result":{"type":"pong","version":"0.9.3","protocol":22}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err = snapshot(&path, T).expect_err("wrong type");
        assert!(matches!(err, WireError::Unexpected(_)));
        server.join().expect("server");
    }

    #[test]
    fn subscription_asks_for_global_and_pane_events() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[ACK, EVENT], Duration::ZERO);
        let panes = ["w1:p1".to_owned(), "w1:p2".to_owned()];
        let mut stream = Subscription::open(&path, &panes, T, POLL).expect("open");
        assert!(stream.next_event().expect("event"));
        assert!(matches!(stream.next_event(), Err(WireError::Closed)));

        let request = request_json(&server.join().expect("server"));
        assert_eq!(request["id"], "contr-sub");
        assert_eq!(request["method"], "events.subscribe");
        let subs = request["params"]["subscriptions"]
            .as_array()
            .expect("subscriptions array");
        assert_eq!(GLOBAL_EVENTS.len(), 19);
        assert_eq!(subs.len(), 21);
        for (sub, kind) in subs.iter().zip(GLOBAL_EVENTS) {
            assert_eq!(sub["type"], *kind);
        }
        assert_eq!(
            subs[19],
            json!({"type": "pane.agent_status_changed", "pane_id": "w1:p1"})
        );
        assert_eq!(
            subs[20],
            json!({"type": "pane.agent_status_changed", "pane_id": "w1:p2"})
        );
    }

    #[test]
    fn subscription_quiet_poll_is_not_an_error() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[ACK], Duration::from_millis(300));
        let mut stream = Subscription::open(&path, &[], T, POLL).expect("open");
        assert!(!stream.next_event().expect("quiet poll"));
        let end = loop {
            match stream.next_event() {
                Ok(false) => continue,
                other => break other,
            }
        };
        assert!(matches!(end, Err(WireError::Closed)));
        server.join().expect("server");
    }

    #[test]
    fn subscription_open_fails_on_pane_not_found() {
        let dir = tempdir();
        let reply = r#"{"id":"contr-sub","error":{"code":"pane_not_found","message":"pane w9:p99 not found"}}"#;
        let (path, server) = serve(dir.path(), &[reply], Duration::ZERO);
        let err =
            Subscription::open(&path, &["w9:p99".to_owned()], T, POLL).expect_err("open must fail");
        assert!(matches!(err, WireError::Server { ref code, .. } if code == "pane_not_found"));
        server.join().expect("server");
    }

    #[test]
    fn subscription_open_fails_when_closed_before_ack() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[], Duration::ZERO);
        let err = Subscription::open(&path, &[], T, POLL).expect_err("closed");
        assert!(matches!(err, WireError::Closed));
        server.join().expect("server");
    }

    #[test]
    fn subscription_open_times_out_without_ack() {
        let dir = tempdir();
        let (path, server) = serve(dir.path(), &[], Duration::from_millis(600));
        let err = Subscription::open(&path, &[], SHORT, POLL).expect_err("no ack");
        assert!(matches!(err, WireError::Io(ref e) if e.kind() == io::ErrorKind::TimedOut));
        server.join().expect("server");
    }
}
