//! The reader thread: keeps one connection to herdr's API socket, follows its
//! events and sends the connection state and the tree to the UI. It only reads;
//! it never writes to herdr.

use std::convert::Infallible;
use std::net::Shutdown;
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex, MutexGuard, PoisonError};
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
/// After the first event not yet in a snapshot, wait this long for the burst to
/// end, then snapshot.
const COALESCE: Duration = Duration::from_millis(50);
/// Least time between two snapshots while events keep arriving: a busy stream
/// gets at most four snapshots per second.
const SNAPSHOT_GAP: Duration = Duration::from_millis(250);
/// Snapshot interval. herdr sends no event for a pane label or cwd change, so
/// without this those fields would go stale.
const REFRESH: Duration = Duration::from_secs(10);
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
    control: Arc<Control>,
    thread: Option<JoinHandle<()>>,
}

impl HerdrReader {
    /// Tells the thread whether the window is shown. While it is hidden, the
    /// thread keeps its connection and applies events, but takes no snapshot on
    /// the refresh timer. When the window is shown again, the thread takes one
    /// snapshot. This never blocks.
    pub fn set_visible(&self, visible: bool) {
        let was_visible = self.control.visible.swap(visible, Ordering::AcqRel);
        if visible && !was_visible {
            self.control.wake();
        }
    }

    /// The value last given to [`HerdrReader::set_visible`]. `true` at start.
    pub fn is_visible(&self) -> bool {
        self.control.visible.load(Ordering::Acquire)
    }
}

impl Drop for HerdrReader {
    fn drop(&mut self) {
        // This ends a blocked event read and a retry wait at once; a snapshot in
        // flight ends within CALL_TIMEOUT. Dropping the handle detaches the thread.
        self.control.stop();
        self.thread.take();
    }
}

/// What the UI side shares with the reader thread. The thread never wakes on a
/// timer to look at it: [`Control::stop`] and [`Control::wake`] wake the thread.
struct Control {
    /// Whether the window is shown. Set by [`HerdrReader::set_visible`].
    visible: AtomicBool,
    signals: Mutex<Signals>,
    /// Notified on stop, so a retry wait ends at once.
    stopped: Condvar,
    /// Event reads started. Tests count them to prove that a quiet stream does
    /// not wake the thread.
    #[cfg(test)]
    reads: std::sync::atomic::AtomicUsize,
}

#[derive(Default)]
struct Signals {
    stop: bool,
    /// A second handle to the open subscription, while there is one.
    /// A shutdown on it ends the thread's blocked read.
    events: Option<UnixStream>,
    /// Set when [`Control::wake`] shut down `events`.
    woken: bool,
}

impl Control {
    fn new() -> Self {
        Self {
            visible: AtomicBool::new(true),
            signals: Mutex::new(Signals::default()),
            stopped: Condvar::new(),
            #[cfg(test)]
            reads: std::sync::atomic::AtomicUsize::new(0),
        }
    }

    /// The flags are plain values, so a panic while they were held leaves them valid.
    fn signals(&self) -> MutexGuard<'_, Signals> {
        self.signals.lock().unwrap_or_else(PoisonError::into_inner)
    }

    fn stop(&self) {
        let mut signals = self.signals();
        signals.stop = true;
        if let Some(events) = signals.events.take() {
            events.shutdown(Shutdown::Both).ok();
        }
        self.stopped.notify_all();
    }

    /// Ends the open subscription, if any, so the thread subscribes again and
    /// takes a snapshot. With no subscription open, the thread takes a snapshot
    /// before its next read anyway.
    fn wake(&self) {
        let mut signals = self.signals();
        if let Some(events) = signals.events.take() {
            signals.woken = true;
            events.shutdown(Shutdown::Both).ok();
        }
    }

    /// Keeps `events` for [`Control::stop`] and [`Control::wake`]. `false` when a
    /// stop came first: then nothing will wake a read on it.
    fn follow(&self, events: UnixStream) -> bool {
        let mut signals = self.signals();
        signals.woken = false;
        if signals.stop {
            return false;
        }
        signals.events = Some(events);
        true
    }

    /// Forgets the subscription handle. `true` when [`Control::wake`] ended it.
    fn unfollow(&self) -> bool {
        let mut signals = self.signals();
        signals.events = None;
        std::mem::take(&mut signals.woken)
    }

    fn is_stopped(&self) -> bool {
        self.signals().stop
    }

    /// Waits for `delay` or a stop. `false` on a stop.
    fn wait(&self, delay: Duration) -> bool {
        let (signals, _) = self
            .stopped
            .wait_timeout_while(self.signals(), delay, |signals| !signals.stop)
            .unwrap_or_else(PoisonError::into_inner);
        !signals.stop
    }
}

#[cfg(test)]
impl HerdrReader {
    /// Event reads the thread has started.
    fn reads(&self) -> usize {
        self.control.reads.load(Ordering::SeqCst)
    }

    /// Stops the thread and waits up to 1 s for it to end. Panics when it is
    /// still running then, or when it panicked.
    fn join_for_test(mut self) {
        self.control.stop();
        let Some(thread) = self.thread.take() else {
            return;
        };
        let deadline = Instant::now() + Duration::from_secs(1);
        while !thread.is_finished() {
            assert!(
                Instant::now() < deadline,
                "reader thread still running 1 s after stop"
            );
            thread::sleep(Duration::from_millis(10));
        }
        assert!(thread.join().is_ok(), "reader thread panicked");
    }
}

/// Starts the reader thread and returns at once. The thread sends `Connecting`,
/// then each change of state and tree, until the reader or the receiver is dropped.
pub fn spawn_reader(target: Target, tx: Sender<HerdrUpdate>) -> std::io::Result<HerdrReader> {
    spawn_with_refresh(target, tx, REFRESH)
}

/// [`spawn_reader`] with the snapshot interval given, so a test can use a short one.
fn spawn_with_refresh(
    target: Target,
    tx: Sender<HerdrUpdate>,
    refresh_every: Duration,
) -> std::io::Result<HerdrReader> {
    let control = Arc::new(Control::new());
    let mut reader = Reader::new(tx, Arc::clone(&control), refresh_every);
    let thread = thread::Builder::new()
        .name("contr-herdr".into())
        .spawn(move || reader.run(&target))?;
    Ok(HerdrReader {
        control,
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
    control: Arc<Control>,
    /// Snapshot interval with no event: [`REFRESH`], or a shorter one in tests.
    refresh_every: Duration,
    state: Option<ConnState>,
    tree: Option<Arc<HerdrTree>>,
    server_version: Option<String>,
    attempt: u32,
}

impl Reader {
    fn new(tx: Sender<HerdrUpdate>, control: Arc<Control>, refresh_every: Duration) -> Self {
        Self {
            tx,
            control,
            refresh_every,
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
        let mut refused = None;
        loop {
            // Subscribe before the snapshot, so no change falls between the two.
            let mut sub = self.open(path, &mut panes, &mut refused)?;
            let events = sub.wake_handle().map_err(|e| self.failed(e))?;
            // Kept before the snapshot, so a show during the snapshot is not lost.
            if !self.control.follow(events) {
                return Err(Ended::Stop);
            }
            let watched = match self.refresh(path, &mut panes) {
                Ok(true) => self.watch(path, &mut sub, &mut panes),
                other => other.map(drop),
            };
            let woken = self.control.unfollow();
            match watched {
                Err(_) if self.stopped() => return Err(Ended::Stop),
                // The window was shown: subscribe again, then take a snapshot.
                Err(Ended::NotRunning(_)) if woken => {}
                result => result?,
            }
        }
    }

    /// Subscribes to `panes`. A `pane_not_found` means a pane closed after the
    /// last snapshot: this open subscribes to none, and the next snapshot brings
    /// the panes that are left. Each open falls back at most once. Herdr refusing
    /// the same set twice in a row is not a race, so the second refusal ends the
    /// session. `refused` is the set of the last refusal; a subscription with
    /// panes that succeeds clears it.
    fn open(
        &self,
        path: &Path,
        panes: &mut Vec<String>,
        refused: &mut Option<Vec<String>>,
    ) -> Result<Subscription, Ended> {
        match Subscription::open(path, panes, CALL_TIMEOUT) {
            Ok(sub) => {
                if !panes.is_empty() {
                    *refused = None;
                }
                Ok(sub)
            }
            Err(WireError::Server { ref code, .. })
                if code == "pane_not_found"
                    && !panes.is_empty()
                    && refused.as_deref() != Some(panes.as_slice()) =>
            {
                *refused = Some(panes.clone());
                panes.clear();
                Subscription::open(path, panes, CALL_TIMEOUT).map_err(|e| self.failed(e))
            }
            Err(err) => Err(self.failed(err)),
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
    /// The stop flag is checked before every read, however busy the stream is.
    /// A snapshot runs when the stream goes quiet, or [`COALESCE`] after the
    /// first event not yet in a snapshot, but never less than [`SNAPSHOT_GAP`]
    /// after the last one. While the window is shown, it also runs every
    /// `refresh_every`. A read waits only until the next snapshot is due; with
    /// none due (window hidden, no event pending), it waits with no timer until
    /// an event, the end of the stream, or a stop or show from [`Control`].
    fn watch(
        &mut self,
        path: &Path,
        sub: &mut Subscription,
        panes: &mut Vec<String>,
    ) -> Result<(), Ended> {
        let mut pending: Option<Instant> = None; // first event not yet in a snapshot
        let mut refreshed = Instant::now();
        loop {
            if self.stopped() {
                return Err(Ended::Stop);
            }
            let visible = self.control.visible.load(Ordering::Acquire);
            let burst_end = pending.map(|since| (since + COALESCE).max(refreshed + SNAPSHOT_GAP));
            let timer = visible.then(|| refreshed + self.refresh_every);
            let due_at = match (burst_end, timer) {
                (Some(a), Some(b)) => Some(a.min(b)),
                (a, b) => a.or(b),
            };
            #[cfg(test)]
            self.control.reads.fetch_add(1, Ordering::SeqCst);
            let wait = due_at.map(|at| at.saturating_duration_since(Instant::now()));
            let quiet = match sub.next_event(wait) {
                Ok(true) => false,
                Ok(false) => true,
                Err(err) => return Err(self.failed(err)),
            };
            if !quiet && pending.is_none() {
                pending = Some(Instant::now());
            }
            // Read again: the window can be hidden while the read waits for the timer.
            let visible = self.control.visible.load(Ordering::Acquire);
            let due = pending.is_some_and(|since| {
                quiet || (since.elapsed() >= COALESCE && refreshed.elapsed() >= SNAPSHOT_GAP)
            }) || (visible && refreshed.elapsed() >= self.refresh_every);
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
        self.control.is_stopped() || self.tx.is_closed()
    }

    /// Waits for `delay`. `false` when the reader must stop first. A receiver
    /// dropped during the wait is seen at its end.
    fn wait(&self, delay: Duration) -> bool {
        !self.stopped() && self.control.wait(delay) && !self.stopped()
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

    /// A `session.snapshot` object with one workspace, tab and pane `pane`, running claude.
    fn snapshot_of(pane: &str, status: &str) -> String {
        format!(
            r#"{{"version":"0.9.3","protocol":22,"workspaces":[{{"workspace_id":"w1","label":"contr","agent_status":"{status}"}}],"tabs":[{{"tab_id":"w1:t1","workspace_id":"w1","label":"planner","agent_status":"{status}"}}],"panes":[{{"pane_id":"{pane}","tab_id":"w1:t1","workspace_id":"w1","agent":"claude","agent_status":"{status}"}}]}}"#
        )
    }

    /// [`snapshot_of`] for the pane `w1:p1`.
    fn snapshot(status: &str) -> String {
        snapshot_of("w1:p1", status)
    }

    const EMPTY_SNAPSHOT: &str =
        r#"{"version":"0.9.3","protocol":22,"workspaces":[],"tabs":[],"panes":[]}"#;

    /// The reply line to a request that carries `snapshot` as its result.
    fn snapshot_reply(snapshot: &str) -> String {
        format!(
            r#"{{"id":"contr-snapshot","result":{{"type":"session_snapshot","snapshot":{snapshot}}}}}"#
        )
    }

    /// What the fake herdr sends on one connection: `lines`, then the `hold` before
    /// close. With `stream`, it then writes that line once per interval until the
    /// client disconnects, so the stream never goes quiet.
    struct Answer {
        lines: Vec<String>,
        hold: Duration,
        stream: Option<(String, Duration)>,
    }

    fn answer(line: &str) -> Answer {
        Answer {
            lines: vec![line.to_owned()],
            hold: Duration::ZERO,
            stream: None,
        }
    }

    /// A subscription that is acked, then sends `EVENT` every `every`.
    fn event_stream(every: Duration) -> Answer {
        Answer {
            lines: vec![ACK.into()],
            hold: Duration::ZERO,
            stream: Some((EVENT.into(), every)),
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
                    if let Some((line, every)) = reply.stream {
                        while writer.write_all(format!("{line}\n").as_bytes()).is_ok() {
                            thread::sleep(every);
                        }
                    }
                });
            }
        });
        path
    }

    /// True when an `events.subscribe` request names panes, not only the global events.
    fn names_panes(request: &Value) -> bool {
        request["params"]["subscriptions"]
            .as_array()
            .is_some_and(|subs| subs.len() > wire::GLOBAL_EVENTS.len())
    }

    /// True when an `events.subscribe` request names pane `pane`.
    fn subscribes_to(request: &Value, pane: &str) -> bool {
        request["params"]["subscriptions"]
            .as_array()
            .is_some_and(|subs| subs.iter().any(|sub| sub["pane_id"] == pane))
    }

    /// The first `count` updates, applied in order.
    fn live_after(rx: &Receiver<HerdrUpdate>, count: usize) -> LiveHerdr {
        let mut live = LiveHerdr::default();
        for _ in 0..count {
            live.apply(next(rx));
        }
        live
    }

    fn first_pane_status(tree: &HerdrTree) -> Option<AgentState> {
        tree.workspaces[0].tabs[0].panes[0]
            .agent
            .as_ref()
            .map(|a| a.status)
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
        reader.join_for_test();
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
                stream: None,
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
        assert_eq!(first_pane_status(&idle), Some(AgentState::Idle));
        assert_eq!(first_pane_status(&working), Some(AgentState::Working));
        reader.join_for_test();
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
        reader.join_for_test();
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
            _ if names_panes(request) => answer(PANE_NOT_FOUND),
            _ => Answer {
                lines: vec![ACK.into()],
                hold: Duration::from_millis(600),
                stream: None,
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
        reader.join_for_test();
    }

    #[test]
    fn a_state_or_tree_equal_to_the_last_one_is_not_sent_again() {
        let (tx, rx) = async_channel::bounded(16);
        let mut reader = Reader::new(tx, Arc::new(Control::new()), REFRESH);
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
        reader.join_for_test();
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

    /// The subscription ack, then a connection that stays open for 5 s.
    fn held_ack() -> Answer {
        Answer {
            lines: vec![ACK.into()],
            hold: Duration::from_secs(5),
            stream: None,
        }
    }

    #[test]
    fn a_stop_ends_the_reader_while_events_keep_coming() {
        let dir = tempdir();
        let path = fake_herdr(dir.path(), |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => answer(&snapshot_reply(&snapshot("idle"))),
            _ => event_stream(Duration::from_millis(10)),
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        // Once the first tree is out, the reader is in its event loop.
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        assert_eq!(
            first_pane_status(&tree_of(next(&rx))),
            Some(AgentState::Idle)
        );
        // Events 10 ms apart never leave the stream quiet, so only a check on every read stops it.
        reader.join_for_test();
    }

    /// A reader connected to a herdr that acks and then stays quiet, with the
    /// window hidden. Returns once the reader is in its event loop. Keep the
    /// receiver: when it is dropped, the reader ends at its next send.
    fn quiet_hidden_reader(dir: &Path) -> (HerdrReader, Receiver<HerdrUpdate>) {
        let path = fake_herdr(dir, |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => answer(&snapshot_reply(&snapshot("idle"))),
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        // A short timer: if hiding did not stop it, the reader would wake often.
        let reader =
            spawn_with_refresh(Target::Socket(path), tx, Duration::from_millis(50)).expect("spawn");
        reader.set_visible(false);
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        tree_of(next(&rx));
        (reader, rx)
    }

    #[test]
    fn a_quiet_stream_does_not_wake_the_reader_while_hidden() {
        let dir = tempdir();
        let (reader, _rx) = quiet_hidden_reader(dir.path());
        let before = reader.reads();
        // Time must pass to show that nothing happens in it.
        thread::sleep(Duration::from_secs(1));
        // At most the read that was waiting when the test started.
        let reads = reader.reads() - before;
        assert!(reads <= 1, "{reads} event reads in 1 s with no event");
        reader.join_for_test();
    }

    #[test]
    fn a_stop_ends_a_read_that_waits_with_no_timer_at_once() {
        let dir = tempdir();
        let (reader, _rx) = quiet_hidden_reader(dir.path());
        let Some(thread) = reader.thread.as_ref() else {
            panic!("no thread");
        };
        assert!(!thread.is_finished());
        let started = Instant::now();
        reader.join_for_test();
        assert!(
            started.elapsed() < Duration::from_millis(200),
            "the thread ended {:?} after the stop",
            started.elapsed()
        );
    }

    #[test]
    fn a_busy_stream_snapshots_at_most_once_per_snapshot_gap() {
        let dir = tempdir();
        let snapshots = Arc::new(AtomicUsize::new(0));
        let counted = Arc::clone(&snapshots);
        let path = fake_herdr(dir.path(), move |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => {
                counted.fetch_add(1, Ordering::SeqCst);
                answer(&snapshot_reply(&snapshot("idle")))
            }
            _ => event_stream(Duration::from_millis(10)),
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
        tree_of(next(&rx));

        let before = snapshots.load(Ordering::SeqCst);
        let window = Instant::now();
        thread::sleep(Duration::from_secs(1));
        let taken = snapshots.load(Ordering::SeqCst) - before;
        // One snapshot per SNAPSHOT_GAP at most, plus two for the edges of the window.
        let allowed = window.elapsed().as_millis() / SNAPSHOT_GAP.as_millis() + 2;
        assert!(
            taken as u128 <= allowed,
            "{taken} snapshots in {:?}",
            window.elapsed()
        );
        reader.join_for_test();
    }

    #[test]
    fn the_tree_is_refreshed_on_the_timer_when_no_event_comes() {
        let dir = tempdir();
        let snapshots = AtomicUsize::new(0);
        let path = fake_herdr(dir.path(), move |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => {
                // Idle for the first two snapshots, working after that. No event is sent.
                let n = snapshots.fetch_add(1, Ordering::SeqCst);
                answer(&snapshot_reply(&snapshot(if n < 2 {
                    "idle"
                } else {
                    "working"
                })))
            }
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_with_refresh(Target::Socket(path), tx, Duration::from_millis(100))
            .expect("spawn");

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        assert_eq!(
            first_pane_status(&tree_of(next(&rx))),
            Some(AgentState::Idle)
        );
        assert_eq!(
            first_pane_status(&tree_of(next(&rx))),
            Some(AgentState::Working)
        );
        reader.join_for_test();
    }

    #[test]
    fn the_timer_refresh_pauses_while_hidden() {
        let dir = tempdir();
        let snapshots = Arc::new(AtomicUsize::new(0));
        let counted = Arc::clone(&snapshots);
        let path = fake_herdr(dir.path(), move |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => {
                counted.fetch_add(1, Ordering::SeqCst);
                answer(&snapshot_reply(&snapshot("idle")))
            }
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        // One second between timer snapshots: the hidden phase below spans more than one.
        let reader =
            spawn_with_refresh(Target::Socket(path), tx, Duration::from_secs(1)).expect("spawn");
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        tree_of(next(&rx));
        // The first session takes two snapshots (see `connect`). Count from here.
        let connected = snapshots.load(Ordering::SeqCst);
        assert!(reader.is_visible(), "a reader starts with the window shown");

        reader.set_visible(false);
        assert!(!reader.is_visible());
        thread::sleep(Duration::from_millis(1500));
        assert_eq!(
            snapshots.load(Ordering::SeqCst),
            connected,
            "the timer takes no snapshot while the window is hidden"
        );
        reader.join_for_test();
    }

    #[test]
    fn the_window_shown_again_takes_one_snapshot() {
        let dir = tempdir();
        let snapshots = Arc::new(AtomicUsize::new(0));
        let counted = Arc::clone(&snapshots);
        let path = fake_herdr(dir.path(), move |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => {
                counted.fetch_add(1, Ordering::SeqCst);
                answer(&snapshot_reply(&snapshot("idle")))
            }
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        // The timer is due only every 30 s, so only the show can take a snapshot here.
        let reader =
            spawn_with_refresh(Target::Socket(path), tx, Duration::from_secs(30)).expect("spawn");
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        tree_of(next(&rx));
        // The first session takes two snapshots (see `connect`). Count from here.
        let connected = snapshots.load(Ordering::SeqCst);

        reader.set_visible(false);
        // Longer than one read, so the reader sees the window hidden before it is shown.
        thread::sleep(Duration::from_millis(400));
        reader.set_visible(true);
        // The snapshot comes at once. The timer is not due for 30 s.
        let deadline = Instant::now() + Duration::from_millis(900);
        while snapshots.load(Ordering::SeqCst) == connected {
            assert!(
                Instant::now() < deadline,
                "no snapshot after the window was shown"
            );
            thread::sleep(Duration::from_millis(10));
        }
        thread::sleep(Duration::from_millis(300));
        assert_eq!(
            snapshots.load(Ordering::SeqCst),
            connected + 1,
            "one snapshot when shown, then no more until the timer is due"
        );
        reader.join_for_test();
    }

    #[test]
    fn a_refresh_that_fails_when_the_window_is_shown_ends_the_session() {
        let dir = tempdir();
        let snapshots = Arc::new(AtomicUsize::new(0));
        let counted = Arc::clone(&snapshots);
        let path = fake_herdr(dir.path(), move |method, _| match method {
            "ping" => answer(PONG),
            // The two snapshots of the connect succeed. The third, taken on show, fails.
            "session.snapshot" => {
                if counted.fetch_add(1, Ordering::SeqCst) < 2 {
                    answer(&snapshot_reply(&snapshot("idle")))
                } else {
                    answer(
                        r#"{"id":"contr-snapshot","error":{"code":"internal","message":"boom"}}"#,
                    )
                }
            }
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader =
            spawn_with_refresh(Target::Socket(path), tx, Duration::from_secs(1)).expect("spawn");
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connecting)
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        tree_of(next(&rx));

        reader.set_visible(false);
        // Longer than one read, so the reader sees the window hidden before it is shown.
        thread::sleep(Duration::from_millis(400));
        reader.set_visible(true);
        match next(&rx) {
            HerdrUpdate::State(ConnState::Incompatible { message }) => {
                assert_eq!(
                    message,
                    "herdr 0.9.3 replied in an unexpected form · update contr"
                );
            }
            other => panic!("expected Incompatible after the failed refresh, got {other:?}"),
        }
        reader.join_for_test();
    }

    #[test]
    fn two_pane_closes_in_one_session_keep_the_connection() {
        let dir = tempdir();
        let snapshots = AtomicUsize::new(0);
        let refusals = AtomicUsize::new(0);
        let path = fake_herdr(dir.path(), move |method, request| match method {
            "ping" => answer(PONG),
            // w1:p1 closes, then w1:p2 closes too; after that w1:p2 stays.
            "session.snapshot" => {
                let n = snapshots.fetch_add(1, Ordering::SeqCst);
                let pane = if n == 0 { "w1:p1" } else { "w1:p2" };
                answer(&snapshot_reply(&snapshot_of(pane, "idle")))
            }
            // The first two subscriptions that name a pane find it closed.
            _ if names_panes(request) && refusals.fetch_add(1, Ordering::SeqCst) < 2 => {
                answer(PANE_NOT_FOUND)
            }
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        let live = live_after(&rx, 3); // Connecting, Connected, then the tree
        assert_eq!(
            live.state,
            ConnState::Connected {
                server_version: "0.9.3".into()
            }
        );
        assert_eq!(live.tree.expect("tree").pane_ids(), ["w1:p2"]);
        reader.join_for_test();
    }

    #[test]
    fn a_pane_refused_again_for_the_same_set_ends_the_session() {
        let dir = tempdir();
        let path = fake_herdr(dir.path(), |method, request| match method {
            "ping" => answer(PONG),
            "session.snapshot" => answer(&snapshot_reply(&snapshot("idle"))),
            _ if names_panes(request) => answer(PANE_NOT_FOUND),
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        // The snapshot still lists w1:p1 after the fallback, so the refusal repeats.
        let live = live_after(&rx, 3); // Connecting, Connected, then Incompatible
        assert_eq!(
            live.state,
            ConnState::Incompatible {
                message: "herdr 0.9.3 replied in an unexpected form · update contr".into(),
            }
        );
        assert_eq!(live.tree, None);
        reader.join_for_test();
    }

    #[test]
    fn a_refusal_after_an_accepted_subscription_is_a_new_race() {
        let dir = tempdir();
        let snapshots = AtomicUsize::new(0);
        let path = fake_herdr(dir.path(), move |method, request| match method {
            "ping" => answer(PONG),
            // Snapshots name w1:p1, w1:p2, w1:p1, then w1:p2 for good.
            "session.snapshot" => {
                let n = snapshots.fetch_add(1, Ordering::SeqCst);
                let pane = if n == 0 || n == 2 { "w1:p1" } else { "w1:p2" };
                answer(&snapshot_reply(&snapshot_of(pane, "idle")))
            }
            _ if subscribes_to(request, "w1:p1") => answer(PANE_NOT_FOUND),
            _ => held_ack(),
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path), tx).expect("spawn");

        // The subscription to w1:p2 is accepted between the two refusals of w1:p1.
        let live = live_after(&rx, 3); // Connecting, Connected, then the tree
        assert_eq!(
            live.state,
            ConnState::Connected {
                server_version: "0.9.3".into()
            }
        );
        assert_eq!(live.tree.expect("tree").pane_ids(), ["w1:p2"]);
        reader.join_for_test();
    }

    /// The server ends the subscription and exits; a new server binds the same
    /// path (a new socket file). A hidden reader, which has no refresh timer,
    /// reports that herdr is gone, connects to the new server and sends its tree.
    #[test]
    fn a_hidden_reader_follows_a_new_server_on_the_same_path() {
        let dir = tempdir();
        let path = fake_herdr(dir.path(), |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => answer(&snapshot_reply(&snapshot("idle"))),
            _ => Answer {
                lines: vec![ACK.into()],
                hold: Duration::from_millis(500),
                stream: None,
            },
        });
        let (tx, rx) = async_channel::bounded(16);
        let reader = spawn_reader(Target::Socket(path.clone()), tx).expect("spawn");
        reader.set_visible(false);
        let live = live_after(&rx, 3);
        assert_eq!(
            first_pane_status(live.tree.as_deref().expect("tree")),
            Some(AgentState::Idle)
        );

        // The old server still holds the subscription; its path now leads to a new one.
        std::fs::remove_file(&path).expect("remove the old socket");
        let new_path = fake_herdr(dir.path(), |method, _| match method {
            "ping" => answer(PONG),
            "session.snapshot" => answer(&snapshot_reply(&snapshot("working"))),
            _ => held_ack(),
        });
        assert_eq!(new_path, path);

        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::NotRunning { .. })
        ));
        assert!(matches!(
            next(&rx),
            HerdrUpdate::State(ConnState::Connected { .. })
        ));
        assert_eq!(
            first_pane_status(&tree_of(next(&rx))),
            Some(AgentState::Working)
        );
        reader.join_for_test();
    }
}
