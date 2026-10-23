//! The reader thread: keeps one connection to herdr's API socket, follows its
//! events and sends the connection state and the tree to the UI. It only reads;
//! it never writes to herdr.

use std::convert::Infallible;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use async_channel::Sender;
use tracing::{info, warn};

use crate::model::HerdrTree;
use crate::wire::{self, API_PROTOCOL, Subscription, WireError};

/// Shown in place of the terminal while herdr is not running.
pub const NOT_RUNNING_LINE: &str = "herdr is not running · start it with `herdr`";

/// Log target of every line this crate writes.
const LOG: &str = "contr_herdr";
/// Longest wait for one reply: ping, snapshot and the subscription ack.
const CALL_TIMEOUT: Duration = Duration::from_secs(2);
/// Longest wait for one event; the stop flag is checked after each quiet wait.
const POLL: Duration = Duration::from_millis(250);
/// Events that arrive within this window share one snapshot.
const COALESCE: Duration = Duration::from_millis(50);
/// Snapshot interval. herdr sends no event for a pane label or cwd change, so
/// without this those fields would go stale.
const REFRESH: Duration = Duration::from_secs(10);
/// Longest sleep between two stop checks while waiting.
const WAIT_STEP: Duration = Duration::from_millis(100);
const BASE_RETRY: Duration = Duration::from_millis(500);
const MAX_RETRY: Duration = Duration::from_secs(5);
/// Wait before retrying after herdr replied in a form contr does not read.
const INCOMPATIBLE_RETRY: Duration = Duration::from_secs(5);

/// The state of the connection, as the UI shows it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConnState {
    Connecting,
    Connected {
        server_version: String,
    },
    NotRunning {
        reason: String,
    },
    /// herdr replied in a form contr does not read. `message` is for the user.
    Incompatible {
        message: String,
    },
}

impl ConnState {
    /// The line the terminal area shows for this state, or `None` when connected.
    pub fn line(&self) -> Option<String> {
        match self {
            Self::Connecting => Some("connecting to herdr…".to_owned()),
            Self::Connected { .. } => None,
            Self::NotRunning { .. } => Some(NOT_RUNNING_LINE.to_owned()),
            Self::Incompatible { message } => Some(message.clone()),
        }
    }
}

/// One message from the reader thread to the UI. The UI applies them in order.
#[derive(Debug, Clone)]
pub enum HerdrUpdate {
    State(ConnState),
    Tree(Arc<HerdrTree>),
}

/// The UI's copy of the connection. Apply each `HerdrUpdate` in order.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LiveHerdr {
    pub state: ConnState,
    pub tree: Option<Arc<HerdrTree>>,
}

impl Default for LiveHerdr {
    fn default() -> Self {
        Self {
            state: ConnState::Connecting,
            tree: None,
        }
    }
}

impl LiveHerdr {
    /// Any state other than `Connected` drops the tree, so the UI never shows a
    /// tree from a connection that has ended.
    pub fn apply(&mut self, update: HerdrUpdate) {
        match update {
            HerdrUpdate::State(state) => {
                if !matches!(state, ConnState::Connected { .. }) {
                    self.tree = None;
                }
                self.state = state;
            }
            HerdrUpdate::Tree(tree) => self.tree = Some(tree),
        }
    }
}

/// Which herdr to follow.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Target {
    /// This user's herdr: the socket path is looked up at every attempt.
    Local,
    Socket(PathBuf),
}

/// Owns the reader thread. Dropping it asks the thread to stop and does not
/// wait for it, so dropping on the UI thread never blocks.
pub struct HerdrReader {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
}

impl Drop for HerdrReader {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Release);
        // The thread exits within one wait step; dropping the handle detaches it.
        self.thread.take();
    }
}

#[cfg(test)]
impl HerdrReader {
    /// Stops the thread and waits for it. `Err` when the thread panicked.
    fn join_for_test(mut self) -> thread::Result<()> {
        self.stop.store(true, Ordering::Release);
        self.thread.take().map_or(Ok(()), JoinHandle::join)
    }
}

/// Starts the reader thread and returns at once. The thread sends `Connecting`,
/// then each change of state and tree, until the reader or the receiver is dropped.
pub fn spawn_reader(target: Target, tx: Sender<HerdrUpdate>) -> std::io::Result<HerdrReader> {
    let stop = Arc::new(AtomicBool::new(false));
    let mut reader = Reader::new(tx, Arc::clone(&stop));
    let thread = thread::Builder::new()
        .name("contr-herdr".into())
        .spawn(move || reader.run(&target))?;
    Ok(HerdrReader {
        stop,
        thread: Some(thread),
    })
}

/// Wait before retry `attempt` (0 is the first): 500 ms, doubling, capped at 5 s.
fn retry_delay(attempt: u32) -> Duration {
    let factor = 2_u32.checked_pow(attempt).unwrap_or(u32::MAX);
    BASE_RETRY.saturating_mul(factor).min(MAX_RETRY)
}

/// The socket to dial: the target's own path, or `local()` for [`Target::Local`].
fn resolve(target: &Target, local: impl FnOnce() -> Option<PathBuf>) -> Option<PathBuf> {
    match target {
        Target::Local => local(),
        Target::Socket(path) => Some(path.clone()),
    }
}

/// Why a connection ended.
#[derive(Debug)]
enum Ended {
    /// Nothing answers at the socket: retry with backoff.
    NotRunning(String),
    /// herdr answered in a form contr does not read: retry after a fixed wait.
    Incompatible(String),
    /// The reader was dropped, or the UI dropped its receiver.
    Stop,
}

/// The thread's side of the connection. `state` and `tree` are the last values
/// sent, so nothing is sent or logged twice.
struct Reader {
    tx: Sender<HerdrUpdate>,
    stop: Arc<AtomicBool>,
    state: Option<ConnState>,
    tree: Option<Arc<HerdrTree>>,
    server_version: Option<String>,
    attempt: u32,
}

impl Reader {
    fn new(tx: Sender<HerdrUpdate>, stop: Arc<AtomicBool>) -> Self {
        Self {
            tx,
            stop,
            state: None,
            tree: None,
            server_version: None,
            attempt: 0,
        }
    }

    fn run(&mut self, target: &Target) {
        if self.report(ConnState::Connecting).is_err() {
            return;
        }
        loop {
            let ended = match resolve(target, wire::api_socket_path) {
                Some(path) => self.session(&path),
                None => Ended::NotRunning("no HOME".to_owned()),
            };
            let (state, delay) = match ended {
                Ended::NotRunning(reason) => {
                    let delay = retry_delay(self.attempt);
                    self.attempt = self.attempt.saturating_add(1);
                    (ConnState::NotRunning { reason }, delay)
                }
                Ended::Incompatible(message) => {
                    (ConnState::Incompatible { message }, INCOMPATIBLE_RETRY)
                }
                Ended::Stop => return,
            };
            self.tree = None;
            self.server_version = None;
            if self.report(state).is_err() || !self.wait(delay) {
                return;
            }
        }
    }

    fn session(&mut self, path: &Path) -> Ended {
        match self.connect(path) {
            Err(ended) => ended,
        }
    }

    /// Follows herdr until the connection ends. Never returns `Ok`: a session
    /// always ends with an error.
    fn connect(&mut self, path: &Path) -> Result<Infallible, Ended> {
        let pong = wire::ping(path, CALL_TIMEOUT).map_err(|e| self.failed(e))?;
        if pong.protocol != API_PROTOCOL {
            warn!(target: LOG, "herdr api protocol={}, contr expects {API_PROTOCOL}", pong.protocol);
        }
        info!(target: LOG, "herdr connected server_version={} protocol={}", pong.version, pong.protocol);
        self.server_version = Some(pong.version.clone());
        self.attempt = 0;
        self.report(ConnState::Connected {
            server_version: pong.version,
        })?;
        let mut panes = Vec::new();
        let mut fell_back = false;
        loop {
            // Subscribe before the snapshot, so no change falls between the two.
            let mut sub = self.open(path, &mut panes, &mut fell_back)?;
            if self.refresh(path, &mut panes)? {
                self.watch(path, &mut sub, &mut panes)?;
            }
        }
    }

    /// Subscribes to `panes`. If one closed since the last snapshot, subscribes
    /// to none, once per connection; the next snapshot brings the current list.
    fn open(
        &self,
        path: &Path,
        panes: &mut Vec<String>,
        fell_back: &mut bool,
    ) -> Result<Subscription, Ended> {
        match Subscription::open(path, panes, CALL_TIMEOUT, POLL) {
            Err(WireError::Server { ref code, .. }) if code == "pane_not_found" && !*fell_back => {
                *fell_back = true;
                panes.clear();
                Subscription::open(path, panes, CALL_TIMEOUT, POLL).map_err(|e| self.failed(e))
            }
            other => other.map_err(|e| self.failed(e)),
        }
    }

    /// Takes a snapshot and publishes it. `Ok(false)` when its pane ids differ
    /// from `panes`: `panes` becomes the new list and the subscription must reopen.
    fn refresh(&mut self, path: &Path, panes: &mut Vec<String>) -> Result<bool, Ended> {
        let snapshot = wire::snapshot(path, CALL_TIMEOUT).map_err(|e| self.failed(e))?;
        let tree = HerdrTree::from_snapshot(&snapshot);
        if tree.pane_ids() != *panes {
            *panes = tree.pane_ids();
            return Ok(false);
        }
        self.publish(tree)?;
        Ok(true)
    }

    /// Reads events until the stream fails (`Err`) or the pane ids change (`Ok`).
    /// Events that arrive close together share one snapshot; a snapshot also
    /// runs every [`REFRESH`].
    fn watch(
        &mut self,
        path: &Path,
        sub: &mut Subscription,
        panes: &mut Vec<String>,
    ) -> Result<(), Ended> {
        let mut pending: Option<Instant> = None; // first event not yet in a snapshot
        let mut refreshed = Instant::now();
        loop {
            let quiet = match sub.next_event() {
                Ok(true) => false,
                Ok(false) => true,
                Err(err) => return Err(self.failed(err)),
            };
            if quiet && self.stopped() {
                return Err(Ended::Stop);
            }
            if !quiet && pending.is_none() {
                pending = Some(Instant::now());
            }
            let due = pending.is_some_and(|since| quiet || since.elapsed() >= COALESCE)
                || refreshed.elapsed() >= REFRESH;
            if due {
                if !self.refresh(path, panes)? {
                    return Ok(());
                }
                pending = None;
                refreshed = Instant::now();
            }
        }
    }

    /// Sends `tree` when it differs from the last one sent, and logs what changed.
    fn publish(&mut self, tree: HerdrTree) -> Result<(), Ended> {
        match self.tree.as_deref() {
            Some(prev) if *prev == tree => return Ok(()),
            Some(prev) if prev.same_shape(&tree) => {
                for change in prev.status_changes(&tree) {
                    info!(target: LOG, "{}", change.log_line());
                }
            }
            _ => {
                info!(
                    target: LOG,
                    "herdr tree workspaces={} tabs={} panes={}",
                    tree.workspaces.len(),
                    tree.tab_count(),
                    tree.pane_count()
                );
                for line in tree.log_lines() {
                    info!(target: LOG, "{line}");
                }
            }
        }
        let tree = Arc::new(tree);
        self.tree = Some(Arc::clone(&tree));
        self.send(HerdrUpdate::Tree(tree))
    }

    /// Sends `state` unless it is the state already sent, and logs the change.
    fn report(&mut self, state: ConnState) -> Result<(), Ended> {
        if self.state.as_ref() == Some(&state) {
            return Ok(());
        }
        match &state {
            ConnState::NotRunning { reason } => {
                info!(target: LOG, "herdr not running ({reason}); retrying");
            }
            ConnState::Incompatible { message } => {
                info!(target: LOG, "herdr incompatible: {message}");
            }
            ConnState::Connecting | ConnState::Connected { .. } => {}
        }
        self.state = Some(state.clone());
        self.send(HerdrUpdate::State(state))
    }

    fn send(&self, update: HerdrUpdate) -> Result<(), Ended> {
        self.tx.send_blocking(update).map_err(|_| Ended::Stop)
    }

    /// How a failed call ends the session. A reply that does not parse, or an
    /// error reply, means an incompatible herdr; anything else means none runs.
    fn failed(&self, err: WireError) -> Ended {
        match err {
            WireError::Unexpected(_) | WireError::Server { .. } => {
                Ended::Incompatible(self.incompatible())
            }
            other => Ended::NotRunning(other.to_string()),
        }
    }

    /// The message for a reply contr cannot read. Names the version once a pong was read.
    fn incompatible(&self) -> String {
        match &self.server_version {
            Some(version) => {
                format!("herdr {version} replied in an unexpected form · update contr")
            }
            None => "herdr replied in an unexpected form · update contr".to_owned(),
        }
    }

    fn stopped(&self) -> bool {
        self.stop.load(Ordering::Acquire) || self.tx.is_closed()
    }

    /// Sleeps for `delay` in short steps. `false` when the reader must stop first.
    fn wait(&self, delay: Duration) -> bool {
        let until = Instant::now() + delay;
        while !self.stopped() {
            let now = Instant::now();
            if now >= until {
                return true;
            }
            thread::sleep(WAIT_STEP.min(until - now));
        }
        false
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use std::io::{BufRead, BufReader, Write};
    use std::os::unix::net::UnixListener;
    use std::sync::atomic::AtomicUsize;

    use async_channel::{Receiver, TryRecvError};
    use serde_json::Value;

    use super::*;
    use crate::model::AgentState;
    use crate::wire::WireSnapshot;

    const PONG: &str =
        r#"{"id":"contr-ping","result":{"type":"pong","version":"0.9.3","protocol":22}}"#;
    const ACK: &str = r#"{"id":"contr-sub","result":{"type":"subscription_started"}}"#;
    const PANE_NOT_FOUND: &str =
        r#"{"id":"contr-sub","error":{"code":"pane_not_found","message":"pane w1:p1 not found"}}"#;
    const EVENT: &str = r#"{"event":"pane.agent_status_changed","data":{"type":"pane.agent_status_changed","agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"}}"#;

    /// A `session.snapshot` object with one workspace, tab and pane (`w1:p1`, claude).
    fn snapshot(status: &str) -> String {
        format!(
            r#"{{"version":"0.9.3","protocol":22,"workspaces":[{{"workspace_id":"w1","label":"contr","agent_status":"{status}"}}],"tabs":[{{"tab_id":"w1:t1","workspace_id":"w1","label":"planner","agent_status":"{status}"}}],"panes":[{{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent":"claude","agent_status":"{status}"}}]}}"#
        )
    }

    const EMPTY_SNAPSHOT: &str =
        r#"{"version":"0.9.3","protocol":22,"workspaces":[],"tabs":[],"panes":[]}"#;

    /// The reply line to a request that carries `snapshot` as its result.
    fn snapshot_reply(snapshot: &str) -> String {
        format!(
            r#"{{"id":"contr-snapshot","result":{{"type":"session_snapshot","snapshot":{snapshot}}}}}"#
        )
    }

    /// What the fake herdr sends on one connection: lines, then the hold before close.
    struct Answer {
        lines: Vec<String>,
        hold: Duration,
    }

    fn answer(line: &str) -> Answer {
        Answer {
            lines: vec![line.to_owned()],
            hold: Duration::ZERO,
        }
    }

    /// A fake herdr in `dir`. Each connection gets a thread: it reads the request and
    /// returns `answer(method, request)`.
    fn fake_herdr(
        dir: &Path,
        answer: impl Fn(&str, &Value) -> Answer + Send + Sync + 'static,
    ) -> PathBuf {
        let path = dir.join("herdr.sock");
        let listener = UnixListener::bind(&path).expect("bind fake herdr");
        let answer = Arc::new(answer);
        thread::spawn(move || {
            for stream in listener.incoming().flatten() {
                let answer = Arc::clone(&answer);
                thread::spawn(move || {
                    let mut line = String::new();
                    BufReader::new(&stream)
                        .read_line(&mut line)
                        .expect("read request");
                    let request: Value = serde_json::from_str(&line).expect("request is JSON");
                    let reply = answer(request["method"].as_str().unwrap_or_default(), &request);
                    let mut writer = &stream;
                    for line in reply.lines {
                        writer.write_all(format!("{line}\n").as_bytes()).ok();
                    }
                    thread::sleep(reply.hold);
                });
            }
        });
        path
    }

    fn tempdir() -> tempfile::TempDir {
        tempfile::Builder::new()
            .prefix("cr")
            .tempdir_in("/tmp")
            .expect("temp dir")
    }

    /// The next update. Panics after 3 s, so a stuck reader fails the test.
    fn next(rx: &Receiver<HerdrUpdate>) -> HerdrUpdate {
        let deadline = Instant::now() + Duration::from_secs(3);
        loop {
            match rx.try_recv() {
                Ok(update) => return update,
                Err(TryRecvError::Empty) if Instant::now() < deadline => {
                    thread::sleep(Duration::from_millis(10));
                }
                Err(err) => panic!("no update: {err:?}"),
            }
        }
    }

    fn tree_of(update: HerdrUpdate) -> Arc<HerdrTree> {
        match update {
            HerdrUpdate::Tree(tree) => tree,
            other => panic!("expected a tree, got {other:?}"),
        }
    }

    fn tree_from(snapshot_json: &str) -> HerdrTree {
        let snapshot: WireSnapshot = serde_json::from_str(snapshot_json).expect("snapshot");
        HerdrTree::from_snapshot(&snapshot)
    }

    #[test]
    fn retry_delay_doubles_from_half_a_second_and_caps_at_five() {
        assert_eq!(retry_delay(0), Duration::from_millis(500));
        assert_eq!(retry_delay(1), Duration::from_secs(1));
        assert_eq!(retry_delay(3), Duration::from_secs(4));
        assert_eq!(retry_delay(4), Duration::from_secs(5));
        assert_eq!(retry_delay(10), Duration::from_secs(5));
        assert_eq!(retry_delay(u32::MAX), Duration::from_secs(5));
    }

    #[test]
    fn live_herdr_keeps_the_tree_only_while_connected() {
        let tree = Arc::new(tree_from(EMPTY_SNAPSHOT));
        let mut live = LiveHerdr::default();
        live.apply(HerdrUpdate::State(ConnState::Connected {
            server_version: "0.9.3".into(),
        }));
        live.apply(HerdrUpdate::Tree(Arc::clone(&tree)));
        assert_eq!(live.tree, Some(tree));

        live.apply(HerdrUpdate::State(ConnState::NotRunning {
            reason: "gone".into(),
        }));
        assert_eq!(live.tree, None);
        assert_eq!(
            live.state,
            ConnState::NotRunning {
                reason: "gone".into()
            }
        );
    }

    #[test]
    fn conn_state_line_is_the_text_the_terminal_area_shows() {
        assert_eq!(
            ConnState::Connecting.line().as_deref(),
            Some("connecting to herdr…")
        );
        assert_eq!(
            ConnState::Connected {
                server_version: "0.9.3".into()
            }
            .line(),
            None
        );
        assert_eq!(
            ConnState::NotRunning { reason: "x".into() }
                .line()
                .as_deref(),
            Some(NOT_RUNNING_LINE)
        );
        assert_eq!(
            ConnState::Incompatible {
                message: "m".into()
            }
            .line()
            .as_deref(),
            Some("m")
        );
    }

    #[test]
    fn resolve_uses_the_socket_target_or_the_local_lookup() {
        let local = || Some(PathBuf::from("/local.sock"));
        assert_eq!(
            resolve(&Target::Socket("/s.sock".into()), local),
            Some(PathBuf::from("/s.sock"))
        );
        assert_eq!(
            resolve(&Target::Local, local),
            Some(PathBuf::from("/local.sock"))
        );
        assert_eq!(resolve(&Target::Local, || None), None);
    }

    #[test]
    fn a_missing_socket_is_not_running_and_the_thread_ends_on_stop() {
        let dir = tempdir();
        let (tx, rx) = async_channel::bounded(16);
        let reader =
            spawn_reader(Target::Socket(dir.path().join("missing.sock")), tx).expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::NotRunning { .. })
        ));
        reader.join_for_test().expect("reader thread ends");
    }

    #[test]
    fn follows_snapshots_and_status_events_over_a_socket() {
        let dir = tempdir();
        let snapshots = AtomicUsize::new(0);
        let path = fake_herdr(dir.path(), move |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => {
                // The first two snapshots are idle; the one after the event is working.
                let n = snapshots.fetch_add(1, Ordering::SeqCst);
                answer(&snapshot_reply(&snapshot(if n < 2 {
                    "idle"
                } else {
                    "working"
                })))
            }
            _ => Answer {
                lines: vec![ACK.into(), EVENT.into()],
                hold: Duration::from_millis(600),
            },
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { ref server_version }) if server_version == "0.9.3"
        ));
        let idle = tree_of(next(&rx));
        let working = tree_of(next(&rx));
        let status = |tree: &HerdrTree| {
            tree.workspaces[0].tabs[0].panes[0]
                .agent
                .as_ref()
                .map(|a| a.status)
        };
        assert_eq!(status(&idle), Some(AgentState::Idle));
        assert_eq!(status(&working), Some(AgentState::Working));
        reader.join_for_test().expect("reader thread ends");
    }

    #[test]
    fn a_pong_the_reader_cannot_parse_is_incompatible() {
        let dir = tempdir();
        let path = fake_herdr(dir.path(), |_, _| {
            answer(r#"{"id":"contr-ping","result":{"type":"pong"}}"#)
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert_eq!(
            match next(&rx) {
                HerdrUpdate::State(state) => state,
                other => panic!("expected a state, got {other:?}"),
            },
            ConnState::Incompatible {
                message: "herdr replied in an unexpected form · update contr".into(),
            }
        );
        reader.join_for_test().expect("reader thread ends");
    }

    #[test]
    fn a_pane_that_closed_before_subscribing_is_dropped_from_the_subscription() {
        let dir = tempdir();
        let snapshots = AtomicUsize::new(0);
        let path = fake_herdr(dir.path(), move |method, request| match method {
            "ping" => answer(PONG),
            "session.snapshot" => {
                let n = snapshots.fetch_add(1, Ordering::SeqCst);
                answer(&snapshot_reply(&if n == 0 {
                    snapshot("idle")
                } else {
                    EMPTY_SNAPSHOT.into()
                }))
            }
            // Global events plus one per pane: a list longer than the globals names a pane.
            _ if request["params"]["subscriptions"]
                .as_array()
                .is_some_and(|subs| subs.len() > wire::GLOBAL_EVENTS.len()) =>
            {
                answer(PANE_NOT_FOUND)
            }
            _ => Answer {
                lines: vec![ACK.into()],
                hold: Duration::from_millis(600),
            },
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        assert!(tree_of(next(&rx)).pane_ids().is_empty());
        reader.join_for_test().expect("reader thread ends");
    }

    #[test]
    fn a_state_or_tree_equal_to_the_last_one_is_not_sent_again() {
        let (tx, rx) = async_channel::bounded(16);
        let mut reader = Reader::new(tx, Arc::new(AtomicBool::new(false)));
        let idle = tree_from(&snapshot("idle"));

        reader.report(ConnState::Connecting).expect("open");
        reader.report(ConnState::Connecting).expect("open");
        reader.publish(idle.clone()).expect("open");
        reader.publish(idle).expect("open");
        reader
            .publish(tree_from(&snapshot("working")))
            .expect("open");

        assert_eq!(rx.len(), 3);
    }

    #[test]
    fn a_protocol_other_than_22_only_warns_and_still_connects() {
        let dir = tempdir();
        let path = fake_herdr(dir.path(), |_, _| {
            answer(
                r#"{"id":"contr-ping","result":{"type":"pong","version":"0.9.9","protocol":23}}"#,
            )
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { ref server_version }) if server_version == "0.9.9"
        ));
        reader.join_for_test().expect("reader thread ends");
    }

    #[test]
    fn a_reader_whose_receiver_is_gone_ends_without_a_stop() {
        let dir = tempdir();
        let (tx, rx) = async_channel::bounded(16);
        let reader =
            spawn_reader(Target::Socket(dir.path().join("missing.sock")), tx).expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        drop(rx);
        // Its next send fails, or its next wait sees the closed channel: no retry follows.
        thread::sleep(Duration::from_millis(700));
        assert!(reader.thread.as_ref().is_some_and(JoinHandle::is_finished));
    }
}
