use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use async_channel::Sender;
use herdr_client::{
    Client, ClientEvent, ConnectOptions, ConnectTarget, MIN_HERDR_VERSION, VersionMismatch,
    connect_with_surface_active,
};
use tracing::info;

use crate::model::HerdrTree;

/// Shown in place of the terminal while herdr is not running.
pub const NOT_RUNNING_LINE: &str = "herdr is not running · start it with `herdr`";

/// Log target of every line this crate writes.
const LOG: &str = "contr_herdr";
/// How long one wait for a client event lasts before the reader checks for a stop.
const POLL: Duration = Duration::from_millis(250);
/// Longest sleep between two stop checks while the reader waits to retry.
const SLEEP_STEP: Duration = Duration::from_millis(100);
const BASE_RETRY: Duration = Duration::from_millis(500);
const MAX_RETRY: Duration = Duration::from_secs(5);

/// What the reader last learned about the connection.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConnState {
    Connecting,
    Connected { server_version: String },
    NotRunning { reason: String },
    Incompatible { message: String },
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

/// One message from the reader thread to the UI.
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
    /// Applies one update. Any state other than `Connected` drops the tree, so
    /// the UI never shows a tree from a connection that has ended.
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

/// Owns the reader thread. Dropping it asks the thread to stop and does not
/// wait for it, so dropping on the UI thread never blocks.
pub struct HerdrReader {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
}

impl Drop for HerdrReader {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Release);
        // Dropping the handle detaches the thread; it exits within one poll.
        self.thread.take();
    }
}

#[cfg(test)]
impl HerdrReader {
    /// Asks the thread to stop and waits for it. Returns `Err` if it panicked.
    pub(crate) fn join_for_test(mut self) -> thread::Result<()> {
        self.stop.store(true, Ordering::Release);
        match self.thread.take() {
            Some(handle) => handle.join(),
            None => Ok(()),
        }
    }
}

/// Starts the reader thread and returns at once. The thread sends `Connecting`,
/// then every change of state and tree, until the reader is dropped or `tx`'s
/// receiver is dropped.
pub fn spawn_reader(
    target: ConnectTarget,
    tx: Sender<HerdrUpdate>,
) -> std::io::Result<HerdrReader> {
    let stop = Arc::new(AtomicBool::new(false));
    let thread_stop = Arc::clone(&stop);
    let thread = thread::Builder::new()
        .name("contr-herdr".into())
        .spawn(move || run(&target, tx, &thread_stop))?;
    Ok(HerdrReader {
        stop,
        thread: Some(thread),
    })
}

/// Waits before retry `attempt` (0 is the first): 500 ms, doubling, capped at 5 s.
fn retry_delay(attempt: u32) -> Duration {
    let factor = 2_u32.checked_pow(attempt).unwrap_or(u32::MAX);
    BASE_RETRY.saturating_mul(factor).min(MAX_RETRY)
}

fn run(target: &ConnectTarget, tx: Sender<HerdrUpdate>, stop: &AtomicBool) {
    let mut reader = Reader::new(tx, stop);
    if !reader.state(ConnState::Connecting) {
        return;
    }
    loop {
        let keep_going =
            match connect_with_surface_active(target.clone(), ConnectOptions::default(), false) {
                Ok(client) => reader.session(&client),
                Err(err) => reader.state(ConnState::NotRunning {
                    reason: err.to_string(),
                }),
            };
        if !keep_going {
            return;
        }
        let delay = if reader.incompatible {
            MAX_RETRY
        } else {
            retry_delay(reader.attempt)
        };
        reader.attempt = reader.attempt.saturating_add(1);
        if !reader.wait(delay) {
            return;
        }
    }
}

/// The reader thread's state: what was last sent, the last tree, the retry count.
struct Reader<'a> {
    tx: Sender<HerdrUpdate>,
    stop: &'a AtomicBool,
    /// Last state sent. A repeat is neither sent nor logged, so retries stay quiet.
    sent: Option<ConnState>,
    /// Last tree sent, used to log only what changed.
    tree: Option<Arc<HerdrTree>>,
    attempt: u32,
    /// Set by a version mismatch, cleared by the next `Connected`. While set,
    /// `Disconnected` does not report "not running" and retries wait the maximum.
    incompatible: bool,
}

impl<'a> Reader<'a> {
    fn new(tx: Sender<HerdrUpdate>, stop: &'a AtomicBool) -> Self {
        Self {
            tx,
            stop,
            sent: None,
            tree: None,
            attempt: 0,
            incompatible: false,
        }
    }

    /// True when the reader must end: a stop was asked for or the window closed.
    fn stopping(&self) -> bool {
        self.stop.load(Ordering::Acquire) || self.tx.is_closed()
    }

    /// Sends and logs `state` unless it repeats the last one sent. Returns
    /// `false` when the receiver is gone.
    fn state(&mut self, state: ConnState) -> bool {
        if self.sent.as_ref() == Some(&state) {
            return true;
        }
        log_state(&state);
        self.sent = Some(state.clone());
        self.send(HerdrUpdate::State(state))
    }

    /// Logs the tree (in full when its shape changed, else only the status
    /// changes), then sends it. Returns `false` when the receiver is gone.
    fn tree(&mut self, tree: Arc<HerdrTree>) -> bool {
        match &self.tree {
            Some(prev) if prev.same_shape(&tree) => {
                for change in prev.status_changes(&tree) {
                    info!(target: LOG, "{}", change.log_line());
                }
            }
            _ => {
                info!(
                    target: LOG,
                    "herdr tree rev={} workspaces={} tabs={} panes={}",
                    tree.revision,
                    tree.workspaces.len(),
                    tree.tab_count(),
                    tree.pane_count(),
                );
                for line in tree.log_lines() {
                    info!(target: LOG, "{line}");
                }
            }
        }
        self.tree = Some(Arc::clone(&tree));
        self.send(HerdrUpdate::Tree(tree))
    }

    fn apply(&mut self, update: HerdrUpdate) -> bool {
        match update {
            HerdrUpdate::State(state) => {
                if matches!(state, ConnState::Connected { .. }) {
                    self.attempt = 0;
                }
                self.state(state)
            }
            HerdrUpdate::Tree(tree) => self.tree(tree),
        }
    }

    fn send(&self, update: HerdrUpdate) -> bool {
        self.tx.send_blocking(update).is_ok()
    }

    /// Pumps one connection until it disconnects. Returns `false` when the
    /// reader must stop, and `true` when it should retry.
    fn session(&mut self, client: &Client) -> bool {
        self.tree = None;
        loop {
            if self.stopping() {
                client.handle.disconnect();
                return false;
            }
            let event = match client.events.recv_timeout(POLL) {
                Ok(event) => event,
                Err(err) if err.is_timeout() => continue,
                // The worker ended without sending `Disconnected`.
                Err(_) => ClientEvent::Disconnected {
                    reason: "connection closed".to_owned(),
                    ssh: None,
                },
            };
            let ended = matches!(event, ClientEvent::Disconnected { .. });
            let open =
                translate(event, &mut self.incompatible).is_none_or(|update| self.apply(update));
            if !open {
                client.handle.disconnect();
                return false;
            }
            if ended {
                return true;
            }
        }
    }

    /// Sleeps for `delay` in short steps. Returns `false` when the reader must stop.
    fn wait(&self, delay: Duration) -> bool {
        let deadline = Instant::now() + delay;
        loop {
            if self.stopping() {
                return false;
            }
            let left = deadline.saturating_duration_since(Instant::now());
            if left.is_zero() {
                return true;
            }
            thread::sleep(left.min(SLEEP_STEP));
        }
    }
}

/// Maps one client event to the update for the UI, or `None` when the UI does
/// not need it. A version mismatch sets `incompatible`, so the `Disconnected`
/// that follows is not reported as "not running".
fn translate(event: ClientEvent, incompatible: &mut bool) -> Option<HerdrUpdate> {
    match event {
        ClientEvent::Connected(welcome) => {
            *incompatible = false;
            Some(HerdrUpdate::State(ConnState::Connected {
                server_version: welcome.server_version,
            }))
        }
        ClientEvent::Snapshot(snapshot) => Some(HerdrUpdate::Tree(Arc::new(
            HerdrTree::from_snapshot(&snapshot),
        ))),
        ClientEvent::VersionMismatch(mismatch) => {
            *incompatible = true;
            Some(HerdrUpdate::State(ConnState::Incompatible {
                message: incompatible_message(&mismatch),
            }))
        }
        ClientEvent::Disconnected { reason, .. } => {
            (!*incompatible).then_some(HerdrUpdate::State(ConnState::NotRunning { reason }))
        }
        _ => None,
    }
}

fn incompatible_message(mismatch: &VersionMismatch) -> String {
    let version = mismatch.server_version();
    match mismatch {
        VersionMismatch::DaemonOutdated { .. } => match version {
            Some(version) => {
                format!("herdr {version} is too old · update herdr (needs {MIN_HERDR_VERSION}+)")
            }
            None => format!("herdr is too old · update herdr (needs {MIN_HERDR_VERSION}+)"),
        },
        VersionMismatch::ClientOutdated { .. } => match version {
            Some(version) => format!("herdr {version} is newer than contr supports · update contr"),
            None => "herdr is newer than contr supports · update contr".to_owned(),
        },
    }
}

fn log_state(state: &ConnState) {
    match state {
        ConnState::Connecting => {}
        ConnState::Connected { server_version } => {
            info!(target: LOG, "herdr connected server_version={server_version}");
        }
        ConnState::NotRunning { reason } => {
            info!(target: LOG, "herdr not running ({reason}); retrying");
        }
        ConnState::Incompatible { message } => {
            info!(target: LOG, "herdr incompatible: {message}");
        }
    }
}

#[cfg(test)]
#[allow(clippy::expect_used)]
mod tests {
    use herdr_client::Error;
    use herdr_client::protocol::endpoint::EndpointServerWelcome;
    use herdr_client::protocol::{AgentStatus, ClientShellSnapshot, ClientShellWorkspace};

    use super::*;
    use crate::model::empty_snapshot;

    fn welcome(version: &str) -> EndpointServerWelcome {
        EndpointServerWelcome {
            generation: 1,
            server_version: version.to_owned(),
            snapshot_codec: String::new(),
            surface_codec: String::new(),
            input_codec: String::new(),
            blob_codec: String::new(),
            methods: vec![],
            capabilities: vec![],
            error: None,
        }
    }

    fn snapshot_with_one_workspace() -> ClientShellSnapshot {
        let mut s = empty_snapshot();
        s.workspaces.push(ClientShellWorkspace {
            workspace_id: "w1".into(),
            active_tab_id: String::new(),
            new_workspace_cwd: String::new(),
            number: 1,
            label: "contr".into(),
            custom_label: false,
            branch: None,
            git_ahead_behind: None,
            tokens: vec![],
            worktree: None,
            focused: false,
            agent_status: AgentStatus::Idle,
        });
        s
    }

    fn state_of(update: Option<HerdrUpdate>) -> Option<ConnState> {
        match update {
            Some(HerdrUpdate::State(state)) => Some(state),
            _ => None,
        }
    }

    fn missing_socket() -> std::path::PathBuf {
        std::env::temp_dir().join(format!("contr-herdr-missing-{}.sock", std::process::id()))
    }

    #[test]
    fn connected_reports_the_server_version_and_clears_incompatible() {
        let mut incompatible = true;
        let update = translate(ClientEvent::Connected(welcome("0.9.3")), &mut incompatible);

        assert_eq!(
            state_of(update),
            Some(ConnState::Connected {
                server_version: "0.9.3".into()
            })
        );
        assert!(!incompatible);
    }

    #[test]
    fn snapshot_becomes_a_tree() {
        let mut incompatible = false;
        let event = ClientEvent::Snapshot(Arc::new(snapshot_with_one_workspace()));

        match translate(event, &mut incompatible) {
            Some(HerdrUpdate::Tree(tree)) => {
                assert_eq!(tree.workspaces.len(), 1);
                assert_eq!(tree.workspaces[0].label, "contr");
            }
            other => panic!("expected a tree, got {other:?}"),
        }
    }

    #[test]
    fn daemon_outdated_with_a_version_names_it() {
        let mut incompatible = false;
        let event = ClientEvent::VersionMismatch(VersionMismatch::DaemonOutdated {
            server_version: Some("0.8.0".into()),
        });

        let update = translate(event, &mut incompatible);

        assert!(incompatible);
        assert_eq!(
            state_of(update),
            Some(ConnState::Incompatible {
                message: "herdr 0.8.0 is too old · update herdr (needs 0.9.0+)".into()
            })
        );
    }

    #[test]
    fn daemon_outdated_without_a_version_says_too_old() {
        let mut incompatible = false;
        let event = ClientEvent::VersionMismatch(VersionMismatch::DaemonOutdated {
            server_version: None,
        });

        let update = translate(event, &mut incompatible);

        assert_eq!(
            state_of(update),
            Some(ConnState::Incompatible {
                message: "herdr is too old · update herdr (needs 0.9.0+)".into()
            })
        );
    }

    #[test]
    fn client_outdated_names_the_newer_daemon_and_says_update_contr() {
        let mut incompatible = false;
        let with_version = ClientEvent::VersionMismatch(VersionMismatch::ClientOutdated {
            server_version: Some("0.10.0".into()),
        });
        let without_version = ClientEvent::VersionMismatch(VersionMismatch::ClientOutdated {
            server_version: None,
        });

        assert_eq!(
            state_of(translate(with_version, &mut incompatible)),
            Some(ConnState::Incompatible {
                message: "herdr 0.10.0 is newer than contr supports · update contr".into()
            })
        );
        assert_eq!(
            state_of(translate(without_version, &mut incompatible)),
            Some(ConnState::Incompatible {
                message: "herdr is newer than contr supports · update contr".into()
            })
        );
    }

    #[test]
    fn disconnected_reports_not_running_with_its_reason() {
        let mut incompatible = false;
        let event = ClientEvent::Disconnected {
            reason: "connection refused".into(),
            ssh: None,
        };

        assert_eq!(
            state_of(translate(event, &mut incompatible)),
            Some(ConnState::NotRunning {
                reason: "connection refused".into()
            })
        );
    }

    #[test]
    fn disconnected_after_a_mismatch_is_not_reported_as_not_running() {
        let mut incompatible = false;
        let mismatch = ClientEvent::VersionMismatch(VersionMismatch::DaemonOutdated {
            server_version: None,
        });
        let _ = translate(mismatch, &mut incompatible);

        let disconnected = ClientEvent::Disconnected {
            reason: "handshake refused".into(),
            ssh: None,
        };
        assert!(translate(disconnected, &mut incompatible).is_none());
        assert!(incompatible, "the flag stays set until the next Connected");
    }

    #[test]
    fn other_events_are_ignored() {
        let mut incompatible = false;
        let event = ClientEvent::CommandRejected {
            request_id: None,
            reason: Error::Cancelled,
        };

        assert!(translate(event, &mut incompatible).is_none());
        assert!(!incompatible);
    }

    #[test]
    fn retry_delay_doubles_then_stops_at_five_seconds() {
        assert_eq!(retry_delay(0), Duration::from_millis(500));
        assert_eq!(retry_delay(1), Duration::from_secs(1));
        assert_eq!(retry_delay(3), Duration::from_secs(4));
        assert_eq!(retry_delay(4), Duration::from_secs(5));
        assert_eq!(retry_delay(10), Duration::from_secs(5));
        assert_eq!(retry_delay(u32::MAX), Duration::from_secs(5));
    }

    #[test]
    fn a_not_connected_state_clears_the_tree_and_connected_keeps_it() {
        let mut live = LiveHerdr::default();
        let tree = Arc::new(HerdrTree::from_snapshot(&snapshot_with_one_workspace()));

        live.apply(HerdrUpdate::Tree(Arc::clone(&tree)));
        assert_eq!(live.tree.as_ref(), Some(&tree));

        live.apply(HerdrUpdate::State(ConnState::Connected {
            server_version: "0.9.3".into(),
        }));
        assert_eq!(live.tree.as_ref(), Some(&tree));

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
    fn state_lines_for_the_terminal_area() {
        assert_eq!(
            ConnState::Connecting.line().as_deref(),
            Some("connecting to herdr…")
        );
        assert_eq!(
            ConnState::NotRunning { reason: "x".into() }
                .line()
                .as_deref(),
            Some("herdr is not running · start it with `herdr`")
        );
        assert_eq!(
            ConnState::Incompatible {
                message: "too old".into()
            }
            .line()
            .as_deref(),
            Some("too old")
        );
        assert_eq!(
            ConnState::Connected {
                server_version: "0.9.3".into()
            }
            .line(),
            None
        );
    }

    #[test]
    fn reader_reports_connecting_then_not_running_once() {
        let (tx, rx) = async_channel::bounded(64);
        let reader = spawn_reader(ConnectTarget::Socket(missing_socket()), tx)
            .expect("spawn the reader thread");

        let mut seen = Vec::new();
        let deadline = Instant::now() + Duration::from_secs(2);
        while seen.len() < 2 && Instant::now() < deadline {
            match rx.try_recv() {
                Ok(update) => seen.push(update),
                Err(_) => thread::sleep(Duration::from_millis(10)),
            }
        }
        assert!(matches!(
            seen.first(),
            Some(HerdrUpdate::State(ConnState::Connecting))
        ));
        assert!(matches!(
            seen.get(1),
            Some(HerdrUpdate::State(ConnState::NotRunning { .. }))
        ));

        // Two more retries must not send the same state again.
        thread::sleep(Duration::from_millis(1200));
        assert!(rx.try_recv().is_err());
        drop(reader);
    }

    #[test]
    fn reader_thread_exits_when_the_receiver_is_dropped() {
        let (tx, rx) = async_channel::bounded(64);
        let reader = spawn_reader(ConnectTarget::Socket(missing_socket()), tx)
            .expect("spawn the reader thread");
        let deadline = Instant::now() + Duration::from_secs(2);
        while rx.try_recv().is_err() && Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }

        drop(rx);

        let deadline = Instant::now() + Duration::from_secs(2);
        while !reader.thread.as_ref().is_some_and(JoinHandle::is_finished)
            && Instant::now() < deadline
        {
            thread::sleep(Duration::from_millis(10));
        }
        assert!(reader.thread.as_ref().is_some_and(JoinHandle::is_finished));
    }

    #[test]
    fn reader_thread_exits_when_stopped() {
        let (tx, rx) = async_channel::bounded(64);
        let reader = spawn_reader(ConnectTarget::Socket(missing_socket()), tx)
            .expect("spawn the reader thread");
        let deadline = Instant::now() + Duration::from_secs(2);
        while rx.try_recv().is_err() && Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }

        assert!(reader.join_for_test().is_ok(), "reader thread panicked");
    }
}
