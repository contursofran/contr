//! The surface link: one background connection to herdr's endpoint socket.
//! The `contr-surface` thread connects, keeps the connection up and reads what
//! herdr sends. A `contr-surface-w` thread per connection writes what the UI
//! sends. The UI reads `LinkUpdate`s and calls `LinkHandle::take_latest`. It
//! never waits on the socket.

use std::io;
use std::net::Shutdown;
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::thread;
use std::time::{Duration, Instant};

use async_channel::{Receiver, Sender, TrySendError};
use tracing::{debug, info, warn};

use super::codec::write_frame;
use super::conn::{Conn, ConnError, endpoint_path_from};
use super::messages::{
    CellPx, ClientMsg, GridSize, InputEvent, SNAPSHOT_KIND, ServerMsg, ShellSnapshot, VersionCheck,
};
use super::surface::Surface;

/// Log target of the link's lines, as in `reader`.
const LOG: &str = "contr_herdr";
/// Longest wait for a handshake. It is also the write timeout of the socket.
const HANDSHAKE: Duration = Duration::from_secs(4);
/// Read timeout of the reader: how often it checks for a stop.
const POLL: Duration = Duration::from_millis(250);
/// Frames queued for the writer. A full queue refuses input with `LinkSendError::Full`.
const OUTBOX: usize = 256;
/// Wait after the first failed attempt. Each further failure doubles it, up to `MAX_RETRY`.
const BASE_RETRY: Duration = Duration::from_millis(500);
const MAX_RETRY: Duration = Duration::from_secs(5);
/// Wait after herdr refused contr or sent a frame contr cannot read. A retry
/// sooner would fail the same way.
const REFUSED_WAIT: Duration = Duration::from_secs(30);
/// Longest sleep between two checks for a stop.
const WAIT_STEP: Duration = Duration::from_millis(100);

/// The connection state, as the UI shows it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LinkState {
    /// Connecting, or reconnecting.
    Connecting,
    /// Connected. `untested` is set for a herdr version contr has not been tested with.
    Live {
        server_version: String,
        untested: bool,
    },
    /// herdr is not running or closed the connection. The reason is for the log.
    NotRunning(String),
    /// herdr refused contr, or sent what contr cannot read. The message is for the user.
    Incompatible(String),
}

/// What the link tells the UI. The UI applies them in order.
#[derive(Debug, Clone)]
pub enum LinkUpdate {
    State(LinkState),
    /// The newest surface changed. Call `LinkHandle::take_latest` for it: it
    /// can be newer than the `Arc` here, because updates coalesce.
    Surface(Arc<Surface>),
    Snapshot(Arc<ShellSnapshot>),
    /// Text a program wrote to the clipboard with OSC 52. The UI writes it to the
    /// system clipboard. Each update is one write, in the order herdr sent them.
    Clipboard(String),
}

/// Where the link connects.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LinkTarget {
    /// herdr's endpoint socket, at the path herdr uses for this user.
    Local,
    /// A given socket path.
    Socket(PathBuf),
}

/// Why a send did not queue. Nothing was sent.
#[derive(Debug, PartialEq, Eq)]
pub enum LinkSendError {
    /// No live connection. Input sent now is not kept.
    NotConnected,
    /// The writer is behind and its queue is full.
    Full,
}

/// What the UI and the link threads share.
struct Shared {
    stop: AtomicBool,
    /// The size and cell the next hello carries. `resize` keeps it current.
    size: Mutex<(GridSize, CellPx)>,
    /// The outbox of the live connection. `None` while there is none.
    outbox: Mutex<Option<Sender<Vec<u8>>>>,
    /// The newest surface not yet taken.
    latest: Mutex<Option<Arc<Surface>>>,
    /// True while a `Surface` update is queued or not yet taken. Set and
    /// cleared only while `latest` is locked.
    pending: AtomicBool,
}

/// The UI's side of the link. Cheap to clone. Its methods never block.
#[derive(Clone)]
pub struct LinkHandle {
    shared: Arc<Shared>,
}

impl LinkHandle {
    /// Queues a `shell.input` for `pane_id`.
    pub fn send_input(&self, pane_id: &str, events: &[InputEvent]) -> Result<(), LinkSendError> {
        self.push(ClientMsg::Input { pane_id, events }.encode())
    }

    /// Stores the size for the next connection, and queues a resize if one is
    /// live. Returns `NotConnected` when none is.
    pub fn resize(&self, size: GridSize, cell: CellPx) -> Result<(), LinkSendError> {
        *lock(&self.shared.size) = (size, cell);
        self.push(ClientMsg::Resize { size, cell }.encode())
    }

    /// Queues the window's focus state for the server.
    pub fn set_focus(&self, focused: bool) -> Result<(), LinkSendError> {
        self.push(ClientMsg::Focus(focused).encode())
    }

    /// Takes the newest surface that has not been taken yet, or `None`.
    /// Taking it clears the pending `Surface` notice.
    pub fn take_latest(&self) -> Option<Arc<Surface>> {
        let mut latest = lock(&self.shared.latest);
        self.shared.pending.store(false, Ordering::SeqCst);
        latest.take()
    }

    fn push(&self, frame: Vec<u8>) -> Result<(), LinkSendError> {
        let outbox = lock(&self.shared.outbox);
        let Some(tx) = outbox.as_ref() else {
            return Err(LinkSendError::NotConnected);
        };
        tx.try_send(frame).map_err(|e| match e {
            TrySendError::Full(_) => LinkSendError::Full,
            TrySendError::Closed(_) => LinkSendError::NotConnected,
        })
    }
}

/// The running link. Dropping it stops the threads. The reader notices within
/// `POLL`, unless it is in a handshake, which takes up to `HANDSHAKE`. A
/// `Detach` is queued first, best effort.
pub struct SurfaceLink {
    shared: Arc<Shared>,
}

impl Drop for SurfaceLink {
    fn drop(&mut self) {
        self.shared.stop.store(true, Ordering::SeqCst);
        if let Some(tx) = lock(&self.shared.outbox).as_ref() {
            let _ = tx.try_send(ClientMsg::Detach.encode());
        }
    }
}

/// Starts the link. It connects at once and keeps trying: see the module docs.
/// `tx` should be unbounded. States and snapshots wait for room in it, and a
/// full channel would hold the link thread. Surfaces never wait.
pub fn spawn_link(
    target: LinkTarget,
    size: GridSize,
    cell: CellPx,
    tx: Sender<LinkUpdate>,
) -> io::Result<(SurfaceLink, LinkHandle)> {
    let shared = Arc::new(Shared {
        stop: AtomicBool::new(false),
        size: Mutex::new((size, cell)),
        outbox: Mutex::new(None),
        latest: Mutex::new(None),
        pending: AtomicBool::new(false),
    });
    let runner = Runner {
        target,
        shared: Arc::clone(&shared),
        tx,
        state: None,
        failures: 0,
    };
    thread::Builder::new()
        .name("contr-surface".to_owned())
        .spawn(move || runner.run())?;
    Ok((
        SurfaceLink {
            shared: Arc::clone(&shared),
        },
        LinkHandle { shared },
    ))
}

/// A link with no thread behind it, for the tests of the crates that use the link. Its frames
/// arrive on the receiver in the order the UI queued them. Dropping the link queues a `Detach`.
#[cfg(any(test, feature = "test-support"))]
pub fn test_link() -> (SurfaceLink, LinkHandle, Receiver<Vec<u8>>) {
    let (tx, rx) = async_channel::unbounded();
    let shared = Arc::new(Shared {
        stop: AtomicBool::new(false),
        size: Mutex::new((
            GridSize { cols: 80, rows: 24 },
            CellPx {
                width: 8,
                height: 16,
            },
        )),
        outbox: Mutex::new(Some(tx)),
        latest: Mutex::new(None),
        pending: AtomicBool::new(false),
    });
    let link = SurfaceLink {
        shared: Arc::clone(&shared),
    };
    (link, LinkHandle { shared }, rx)
}

/// The `contr-surface` thread's state.
struct Runner {
    target: LinkTarget,
    shared: Arc<Shared>,
    tx: Sender<LinkUpdate>,
    /// The last state sent. A state is sent only when it changes.
    state: Option<LinkState>,
    /// Failed attempts in a row. The first surface of a connection resets it. A
    /// connection that closes before that is a failure.
    failures: u32,
}

/// How a live connection ended.
enum Ended {
    /// The link was dropped, or the UI stopped listening.
    Stopped,
    /// The surface no longer follows herdr's patches. Reconnect for a full surface.
    OutOfSync,
    /// herdr closed the connection or shut down. The reason is for the user.
    Gone(String),
    /// herdr sent a frame contr cannot read. The message is for the user.
    Unreadable(String),
}

impl Runner {
    fn run(mut self) {
        self.set_state(LinkState::Connecting);
        loop {
            if self.stopping() || !self.connect_once() {
                return;
            }
        }
    }

    /// One attempt: connect, then serve the connection. False when the link must stop.
    fn connect_once(&mut self) -> bool {
        let Some(path) = resolve(&self.target, |key| std::env::var(key).ok()) else {
            self.set_state(LinkState::NotRunning("no HOME".to_owned()));
            return self.retry();
        };
        let (size, cell) = *lock(&self.shared.size);
        match Conn::open(&path, size, cell, HANDSHAKE) {
            Ok(conn) => self.serve(conn),
            Err(ConnError::NotRunning(why)) => {
                self.set_state(LinkState::NotRunning(why));
                self.retry()
            }
            Err(ConnError::Incompatible(message)) => self.refuse(message),
            Err(e @ ConnError::Codec(_)) => self.refuse(e.to_string()),
            Err(e @ ConnError::Io(_)) => {
                self.set_state(LinkState::NotRunning(e.to_string()));
                self.retry()
            }
        }
    }

    /// Runs one connection until it ends. False when the link must stop.
    fn serve(&mut self, mut conn: Conn) -> bool {
        let version = conn.welcome.server_version.clone();
        let live = LinkState::Live {
            server_version: version.clone(),
            untested: matches!(conn.version, VersionCheck::Untested(_)),
        };
        match self.session(&mut conn, live, &version) {
            Ended::Stopped => false,
            // Both wait with the backoff: a connection that ends before its first
            // surface counts as a failure, so a herdr that accepts and closes at
            // once is not retried every `BASE_RETRY`.
            Ended::OutOfSync => {
                self.set_state(LinkState::Connecting);
                self.retry()
            }
            Ended::Gone(reason) => {
                self.set_state(LinkState::NotRunning(reason));
                self.retry()
            }
            Ended::Unreadable(message) => self.refuse(message),
        }
    }

    /// Starts the writer, announces `live`, and reads until the connection ends.
    fn session(&mut self, conn: &mut Conn, live: LinkState, version: &str) -> Ended {
        // On macOS this fails with EINVAL once the peer has closed. The reads
        // below then end with EOF, which is the answer to report. The handshake's
        // timeout still bounds each read until then.
        if let Err(e) = conn.set_read_timeout(Some(POLL)) {
            debug!(target: LOG, "herdr endpoint read timeout not set: {e}");
        }
        let mut writer = match conn.try_clone_writer() {
            Ok(writer) => writer,
            Err(e) => return Ended::Gone(e.to_string()),
        };
        let (outbox_tx, outbox_rx) = async_channel::bounded(OUTBOX);
        let spawned = thread::Builder::new()
            .name("contr-surface-w".to_owned())
            .spawn(move || write_frames(&mut writer, &outbox_rx));
        if let Err(e) = spawned {
            return Ended::Gone(e.to_string());
        }
        *lock(&self.shared.outbox) = Some(outbox_tx);
        self.set_state(live);
        let ended = self.read(conn, version);
        // Dropping the sender lets the writer send what is still queued, a
        // `Detach` for one, and then exit.
        *lock(&self.shared.outbox) = None;
        *lock(&self.shared.latest) = None;
        ended
    }

    /// Reads and publishes server messages until the connection ends.
    fn read(&mut self, conn: &mut Conn, version: &str) -> Ended {
        let mut held: Option<Surface> = None;
        loop {
            if self.stopping() {
                return Ended::Stopped;
            }
            let msg = match conn.recv() {
                Ok(Some(msg)) => msg,
                // The read timeout passed. Loop round to check for a stop.
                Ok(None) => continue,
                Err(e) => return read_failure(e, version),
            };
            match msg {
                ServerMsg::Control { kind, data } if kind == SNAPSHOT_KIND => {
                    match serde_json::from_str::<ShellSnapshot>(&data) {
                        Ok(snapshot) => {
                            if !self.send(LinkUpdate::Snapshot(Arc::new(snapshot))) {
                                return Ended::Stopped;
                            }
                        }
                        Err(e) => debug!(target: LOG, "herdr snapshot not readable: {e}"),
                    }
                }
                ServerMsg::Surface(frame) => {
                    // The connection works: the next failure starts the backoff again.
                    self.failures = 0;
                    let surface = held.insert(frame.0);
                    self.publish(surface);
                }
                ServerMsg::Patch(patch) => {
                    let in_step = held
                        .as_mut()
                        .is_some_and(|surface| surface.apply_patch(patch).is_ok());
                    if !in_step {
                        warn!(target: LOG, "surface patch out of sync; reconnecting");
                        return Ended::OutOfSync;
                    }
                    if let Some(surface) = held.as_ref() {
                        self.publish(surface);
                    }
                }
                ServerMsg::Clipboard(text) => {
                    if !self.send(LinkUpdate::Clipboard(text)) {
                        return Ended::Stopped;
                    }
                }
                ServerMsg::Shutdown(reason) => {
                    return Ended::Gone(reason.unwrap_or_else(|| "herdr stopped".to_owned()));
                }
                ServerMsg::ShellError(message) => {
                    warn!(target: LOG, "herdr shell error: {message}");
                }
                ServerMsg::Control { .. }
                | ServerMsg::LegacyWelcome { .. }
                | ServerMsg::Other(_) => {}
            }
        }
    }

    /// Makes `surface` the newest one, and tells the UI unless a notice is
    /// already pending. Never blocks.
    fn publish(&self, surface: &Surface) {
        let newest = Arc::new(surface.clone());
        let mut latest = lock(&self.shared.latest);
        *latest = Some(Arc::clone(&newest));
        if self.shared.pending.swap(true, Ordering::SeqCst) {
            return;
        }
        if self.tx.try_send(LinkUpdate::Surface(newest)).is_err() {
            // The channel is full or closed. Clear the flag, so the next surface tries again.
            self.shared.pending.store(false, Ordering::SeqCst);
        }
    }

    /// Sends the state if it changed.
    fn set_state(&mut self, state: LinkState) {
        if self.state.as_ref() == Some(&state) {
            return;
        }
        info!(target: LOG, "surface link state: {state:?}");
        self.state = Some(state.clone());
        let _ = self.tx.send_blocking(LinkUpdate::State(state));
    }

    /// Sends a snapshot. False when the UI stopped listening.
    fn send(&self, update: LinkUpdate) -> bool {
        self.tx.send_blocking(update).is_ok()
    }

    /// Shows the refusal and waits before the next attempt. False when the link must stop.
    fn refuse(&mut self, message: String) -> bool {
        self.set_state(LinkState::Incompatible(message));
        self.pause(REFUSED_WAIT)
    }

    /// Waits before the next attempt, longer after each failure in a row.
    /// False when the link must stop.
    fn retry(&mut self) -> bool {
        let wait = retry_delay(self.failures);
        self.failures = self.failures.saturating_add(1);
        self.pause(wait)
    }

    /// Sleeps for `total`, in steps of `WAIT_STEP`. False when the link must stop meanwhile.
    fn pause(&self, total: Duration) -> bool {
        let deadline = Instant::now() + total;
        loop {
            if self.stopping() {
                return false;
            }
            let left = deadline.saturating_duration_since(Instant::now());
            if left.is_zero() {
                return true;
            }
            thread::sleep(left.min(WAIT_STEP));
        }
    }

    /// True when the link was dropped or the UI stopped listening.
    fn stopping(&self) -> bool {
        self.shared.stop.load(Ordering::SeqCst) || self.tx.is_closed()
    }
}

/// The writer thread: sends queued frames in order until the outbox closes or
/// a write fails. A failed write shuts the socket down, so the reader sees the
/// end too.
fn write_frames(stream: &mut UnixStream, outbox: &Receiver<Vec<u8>>) {
    while let Ok(frame) = outbox.recv_blocking() {
        if let Err(e) = write_frame(stream, &frame) {
            warn!(target: LOG, "herdr endpoint write failed: {e}");
            let _ = stream.shutdown(Shutdown::Both);
            return;
        }
    }
}

/// What a failed read means for the connection.
fn read_failure(e: ConnError, version: &str) -> Ended {
    match e {
        ConnError::Codec(_) => Ended::Unreadable(unreadable(version)),
        ConnError::Io(err) if err.kind() == io::ErrorKind::InvalidData => {
            Ended::Unreadable(unreadable(version))
        }
        ConnError::Io(err) if err.kind() == io::ErrorKind::UnexpectedEof => {
            Ended::Gone("herdr closed the endpoint".to_owned())
        }
        other => Ended::Gone(other.to_string()),
    }
}

fn unreadable(version: &str) -> String {
    format!("herdr {version} sent a surface contr cannot read · update contr")
}

/// The socket to connect to. For `Local`, the path is found from `env` the way
/// herdr finds it. `None` when that needs HOME and HOME is not set.
fn resolve(target: &LinkTarget, env: impl Fn(&str) -> Option<String>) -> Option<PathBuf> {
    match target {
        LinkTarget::Local => endpoint_path_from(env),
        LinkTarget::Socket(path) => Some(path.clone()),
    }
}

/// The wait after `failures` failed attempts in a row: `BASE_RETRY`, doubled
/// each time, at most `MAX_RETRY`.
fn retry_delay(failures: u32) -> Duration {
    BASE_RETRY
        .saturating_mul(2_u32.saturating_pow(failures))
        .min(MAX_RETRY)
}

/// Locks `m`. A panicking thread leaves the data whole, so a poisoned lock is used as it is.
fn lock<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    m.lock().unwrap_or_else(PoisonError::into_inner)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use std::io::Write;
    use std::os::unix::net::UnixListener;
    use std::sync::mpsc;

    use async_channel::TryRecvError;
    use serde_json::Value;

    use super::*;
    use crate::endpoint::codec::{Dec, Enc, read_frame};
    use crate::endpoint::messages::{
        C_CONTROL, C_DETACH, C_FOCUS, C_PANE_INPUT, HELLO_KIND, MAX_FRAME, S_CLIPBOARD, S_PATCH,
        S_SHUTDOWN, S_SURFACE, WELCOME_KIND,
    };
    use crate::endpoint::testing::{control, put, read_hello, welcome, welcome_json};

    const WAIT: Duration = Duration::from_secs(2);
    const STEP: Duration = Duration::from_millis(5);
    const SIZE: GridSize = GridSize { cols: 60, rows: 12 };
    const CELL: CellPx = CellPx {
        width: 8,
        height: 16,
    };

    /// Serves `count` connections one after the other. `script(stream, n)` runs
    /// for the n-th. The thread is not joined: it ends with the test process.
    fn serve_each(
        count: usize,
        mut script: impl FnMut(UnixStream, usize) + Send + 'static,
    ) -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::Builder::new()
            .prefix("cl")
            .tempdir_in("/tmp")
            .unwrap();
        let path = dir.path().join("herdr-client.sock");
        let listener = UnixListener::bind(&path).unwrap();
        thread::spawn(move || {
            for n in 0..count {
                let Ok((stream, _)) = listener.accept() else {
                    return;
                };
                script(stream, n);
            }
        });
        (dir, path)
    }

    fn start(target: LinkTarget) -> (SurfaceLink, LinkHandle, Receiver<LinkUpdate>) {
        let (tx, rx) = async_channel::unbounded();
        let (link, handle) = spawn_link(target, SIZE, CELL, tx).unwrap();
        (link, handle, rx)
    }

    /// The next update. Fails after `WAIT`.
    fn next(rx: &Receiver<LinkUpdate>) -> LinkUpdate {
        let deadline = Instant::now() + WAIT;
        loop {
            match rx.try_recv() {
                Ok(update) => return update,
                Err(TryRecvError::Empty) => {
                    assert!(Instant::now() < deadline, "no update within {WAIT:?}");
                    thread::sleep(STEP);
                }
                Err(TryRecvError::Closed) => panic!("the link closed its channel"),
            }
        }
    }

    fn next_state(rx: &Receiver<LinkUpdate>) -> LinkState {
        match next(rx) {
            LinkUpdate::State(state) => state,
            other => panic!("expected a state, got {other:?}"),
        }
    }

    fn next_surface(rx: &Receiver<LinkUpdate>) -> Arc<Surface> {
        match next(rx) {
            LinkUpdate::Surface(surface) => surface,
            other => panic!("expected a surface, got {other:?}"),
        }
    }

    fn live(version: &str, untested: bool) -> LinkState {
        LinkState::Live {
            server_version: version.to_owned(),
            untested,
        }
    }

    /// One `PaneSurface` frame of `cols` × `rows` cells, each showing `fill`.
    /// Boot "b1", projection 7. Fields after the panes are left out: the
    /// decoder does not read them.
    fn surface_frame(revision: u64, cols: u16, rows: u16, fill: &str) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(u64::from(S_SURFACE));
        e.str("b1");
        e.vu(7);
        e.vu(revision);
        let cells = usize::from(cols) * usize::from(rows);
        e.vu(cells as u64);
        for _ in 0..cells {
            put_cell(&mut e, fill);
        }
        e.vu(u64::from(cols));
        e.vu(u64::from(rows));
        e.none(); // cursor
        e.vu(0); // hyperlinks
        e.vu(0); // graphics
        e.vu(0); // panes
        e.buf
    }

    /// A `PaneSurfacePatch` from `base` to `revision` that sets cell (0, 0) to `text`.
    fn patch_frame(base: u64, revision: u64, text: &str) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(u64::from(S_PATCH));
        e.str("b1");
        e.vu(7);
        e.vu(base);
        e.vu(revision);
        e.vu(1); // spans
        e.vu(0); // x
        e.vu(0); // y
        e.vu(1); // cells in the span
        put_cell(&mut e, text);
        e.vu(0); // panes
        e.none(); // cursor
        e.buf
    }

    /// One `CellData` with no colour, no style and no hyperlink.
    fn put_cell(e: &mut Enc, text: &str) {
        e.str(text);
        e.vu(0); // fg
        e.vu(0); // bg
        e.vu(0); // modifier
        e.bool(false); // skip
        e.none(); // hyperlink
    }

    /// One `Clipboard` frame whose data is the base64 text `data`.
    fn clipboard_frame(data: &str) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(u64::from(S_CLIPBOARD));
        e.str(data);
        e.buf
    }

    fn shutdown_frame(reason: Option<&str>) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(u64::from(S_SHUTDOWN));
        match reason {
            Some(text) => {
                e.some();
                e.str(text);
            }
            None => e.none(),
        }
        e.buf
    }

    /// The JSON body of a client hello payload.
    fn hello_body(payload: &[u8]) -> Value {
        let mut d = Dec::new(payload);
        assert_eq!(d.vu().unwrap(), u64::from(C_CONTROL));
        assert_eq!(d.str().unwrap(), HELLO_KIND);
        serde_json::from_str(&d.str().unwrap()).unwrap()
    }

    #[test]
    fn retry_waits_double_from_half_a_second_and_stop_at_five() {
        assert_eq!(retry_delay(0), Duration::from_millis(500));
        assert_eq!(retry_delay(1), Duration::from_secs(1));
        assert_eq!(retry_delay(3), Duration::from_secs(4));
        assert_eq!(retry_delay(4), Duration::from_secs(5));
        assert_eq!(retry_delay(40), Duration::from_secs(5));
        assert_eq!(retry_delay(u32::MAX), Duration::from_secs(5));
    }

    #[test]
    fn resolve_uses_the_given_socket_or_the_environment() {
        let socket = PathBuf::from("/s/given.sock");
        assert_eq!(
            resolve(&LinkTarget::Socket(socket.clone()), |_| None),
            Some(socket)
        );
        // Local needs HOME or one of the socket variables. Without them, no path.
        assert_eq!(resolve(&LinkTarget::Local, |_| None), None);
        let from_env = |key: &str| (key == "HERDR_SOCKET_PATH").then(|| "/s/a.sock".to_owned());
        assert_eq!(
            resolve(&LinkTarget::Local, from_env),
            Some(PathBuf::from("/s/a-client.sock"))
        );
    }

    #[test]
    fn take_latest_is_none_before_any_surface() {
        let (_link, handle, _rx) = start(LinkTarget::Socket(PathBuf::from("/nonexistent/x.sock")));
        assert!(handle.take_latest().is_none());
    }

    #[test]
    fn a_missing_socket_is_connecting_then_not_running() {
        let (_link, _handle, rx) = start(LinkTarget::Socket(PathBuf::from(
            "/nonexistent/herdr-client.sock",
        )));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        match next_state(&rx) {
            LinkState::NotRunning(why) => assert!(why.contains("not running"), "{why}"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn a_repeated_failure_is_not_sent_again() {
        let (_link, _handle, rx) = start(LinkTarget::Socket(PathBuf::from(
            "/nonexistent/herdr-client.sock",
        )));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        assert!(matches!(next_state(&rx), LinkState::NotRunning(_)));
        // The next attempt, 500 ms later, fails the same way: no new update.
        thread::sleep(Duration::from_millis(700));
        assert!(matches!(rx.try_recv(), Err(TryRecvError::Empty)));
    }

    #[test]
    fn a_socket_path_too_long_is_not_running_with_the_io_reason() {
        let long = PathBuf::from(format!("/tmp/{}", "a".repeat(120)));
        let (_link, _handle, rx) = start(LinkTarget::Socket(long));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        match next_state(&rx) {
            LinkState::NotRunning(why) => assert!(why.contains("endpoint socket"), "{why}"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn input_and_resize_before_a_connection_are_not_connected() {
        let (_link, handle, _rx) = start(LinkTarget::Socket(PathBuf::from(
            "/nonexistent/herdr-client.sock",
        )));
        let events = [InputEvent::Text("a".to_owned())];
        assert_eq!(
            handle.send_input("w1:p1", &events),
            Err(LinkSendError::NotConnected)
        );
        assert_eq!(handle.set_focus(true), Err(LinkSendError::NotConnected));
        let cell = CellPx {
            width: 9,
            height: 18,
        };
        assert_eq!(
            handle.resize(GridSize { cols: 40, rows: 8 }, cell),
            Err(LinkSendError::NotConnected)
        );
    }

    #[test]
    fn the_link_publishes_live_then_a_snapshot_and_a_surface() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            let snapshot = r#"{"boot_id":"b1","revision":1,"focused_pane_id":"w1:p1"}"#;
            put(&mut stream, &control(SNAPSHOT_KIND, snapshot));
            put(&mut stream, &surface_frame(1, 60, 12, "a"));
            thread::sleep(WAIT);
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        assert_eq!(next_state(&rx), live("0.9.3", false));
        match next(&rx) {
            LinkUpdate::Snapshot(s) => assert_eq!(s.focused_pane_id.as_deref(), Some("w1:p1")),
            other => panic!("{other:?}"),
        }
        next_surface(&rx);
        let surface = handle.take_latest().unwrap();
        assert_eq!(surface.revision, 1);
        assert_eq!(surface.size, SIZE);
        assert_eq!(surface.rows[0][0].text, "a");
    }

    #[test]
    fn a_clipboard_write_reaches_the_ui_as_one_update() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &clipboard_frame("aGk="));
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        assert_eq!(next_state(&rx), live("0.9.3", false));
        match next(&rx) {
            LinkUpdate::Clipboard(text) => assert_eq!(text, "hi"),
            other => panic!("{other:?}"),
        }
        assert!(rx.try_recv().is_err(), "one write, one update");
    }

    #[test]
    fn a_clipboard_payload_that_is_dropped_keeps_the_connection_up() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            // Not base64: dropped without an update or a state change.
            put(&mut stream, &clipboard_frame("!!"));
            put(&mut stream, &clipboard_frame("aGk="));
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        // Had the link ended, a state other than `Live` would come next.
        assert_eq!(next_state(&rx), live("0.9.3", false));
        match next(&rx) {
            LinkUpdate::Clipboard(text) => assert_eq!(text, "hi"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn a_version_contr_has_not_tested_is_live_with_the_flag_set() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.9"));
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        assert_eq!(next_state(&rx), live("0.9.9", true));
    }

    #[test]
    fn a_patch_is_applied_and_the_next_take_has_the_new_revision() {
        let (go_tx, go_rx) = mpsc::channel::<()>();
        let (_dir, path) = serve_each(1, move |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &surface_frame(1, 60, 12, "a"));
            go_rx.recv().unwrap();
            put(&mut stream, &patch_frame(1, 2, "Z"));
            thread::sleep(WAIT);
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        next_surface(&rx);
        assert_eq!(handle.take_latest().unwrap().revision, 1);
        go_tx.send(()).unwrap();
        next_surface(&rx);
        let patched = handle.take_latest().unwrap();
        assert_eq!(patched.revision, 2);
        assert_eq!(patched.rows[0][0].text, "Z");
        assert_eq!(patched.rows[0][1].text, "a");
    }

    #[test]
    fn two_surfaces_before_a_take_queue_one_notice_and_take_the_newest() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &surface_frame(1, 60, 12, "a"));
            put(&mut stream, &surface_frame(2, 60, 12, "b"));
            thread::sleep(WAIT);
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        next_surface(&rx);
        // Let the link read the second surface too: it must not queue a second notice.
        thread::sleep(Duration::from_millis(300));
        assert!(matches!(rx.try_recv(), Err(TryRecvError::Empty)));
        assert_eq!(handle.take_latest().unwrap().revision, 2);
        assert!(handle.take_latest().is_none());
    }

    #[test]
    fn a_stale_patch_reconnects_for_a_full_surface() {
        let (hello_tx, hello_rx) = mpsc::channel::<()>();
        let (_dir, path) = serve_each(2, move |mut stream, n| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            if n == 0 {
                put(&mut stream, &surface_frame(1, 60, 12, "a"));
                // Based on revision 99, which the link does not hold: out of sync.
                put(&mut stream, &patch_frame(99, 100, "X"));
            } else {
                hello_tx.send(()).unwrap();
                put(&mut stream, &surface_frame(5, 60, 12, "b"));
                thread::sleep(WAIT);
            }
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        assert_eq!(next_state(&rx), LinkState::Connecting);
        assert_eq!(next_state(&rx), live("0.9.3", false));
        next_surface(&rx);
        handle.take_latest();
        assert_eq!(next_state(&rx), LinkState::Connecting);
        assert_eq!(next_state(&rx), live("0.9.3", false));
        hello_rx.recv_timeout(WAIT).unwrap();
        next_surface(&rx);
        assert_eq!(handle.take_latest().unwrap().revision, 5);
    }

    #[test]
    fn shutdown_without_a_reason_is_not_running_herdr_stopped() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &shutdown_frame(None));
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        assert_eq!(
            next_state(&rx),
            LinkState::NotRunning("herdr stopped".to_owned())
        );
    }

    #[test]
    fn shutdown_with_a_reason_passes_the_reason_on() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &shutdown_frame(Some("server stop")));
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        assert_eq!(
            next_state(&rx),
            LinkState::NotRunning("server stop".to_owned())
        );
    }

    #[test]
    fn end_of_file_after_live_is_not_running() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        assert_eq!(
            next_state(&rx),
            LinkState::NotRunning("herdr closed the endpoint".to_owned())
        );
    }

    #[test]
    fn a_herdr_that_accepts_and_closes_at_once_is_retried_with_backoff() {
        let accepts = Arc::new(std::sync::atomic::AtomicUsize::new(0));
        let counted = Arc::clone(&accepts);
        let (_dir, path) = serve_each(8, move |mut stream, _| {
            counted.fetch_add(1, Ordering::SeqCst);
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            // The stream is dropped here: herdr closed the connection at once.
        });
        let (_link, _handle, _rx) = start(LinkTarget::Socket(path));
        thread::sleep(Duration::from_millis(1200));
        // Attempts at about 0 s, 0.5 s and 1.5 s (waits of 500 ms, then 1 s). A
        // fixed 500 ms wait would make three attempts by 1.2 s.
        let attempts = accepts.load(Ordering::SeqCst);
        assert!((1..=2).contains(&attempts), "{attempts} attempts in 1.2 s");
    }

    #[test]
    fn a_refused_welcome_is_incompatible_with_herdrs_reason() {
        let json = welcome_json(
            1,
            "0.9.3",
            r#","error":{"code":"invalid_hello","message":"bad hello"}"#,
        );
        let (_dir, path) = serve_each(1, move |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &control(WELCOME_KIND, &json));
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        match next_state(&rx) {
            LinkState::Incompatible(message) => {
                assert!(message.contains("invalid_hello"), "{message}");
                assert!(message.contains("bad hello"), "{message}");
            }
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn an_undecodable_frame_after_live_is_incompatible_with_the_version() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &[0xFF]); // a varint marker that does not exist
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        assert_eq!(
            next_state(&rx),
            LinkState::Incompatible(
                "herdr 0.9.3 sent a surface contr cannot read · update contr".to_owned()
            )
        );
    }

    #[test]
    fn an_oversized_frame_header_after_live_is_incompatible_too() {
        let too_long = u32::try_from(MAX_FRAME + 1).unwrap();
        let (_dir, path) = serve_each(1, move |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            stream.write_all(&too_long.to_le_bytes()).unwrap();
            thread::sleep(WAIT);
        });
        let (_link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        assert_eq!(
            next_state(&rx),
            LinkState::Incompatible(
                "herdr 0.9.3 sent a surface contr cannot read · update contr".to_owned()
            )
        );
    }

    #[test]
    fn input_and_focus_reach_the_server_in_order() {
        let (sent_tx, sent_rx) = mpsc::channel::<Vec<u8>>();
        let (_dir, path) = serve_each(1, move |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            for _ in 0..2 {
                sent_tx
                    .send(read_frame(&mut stream).unwrap().unwrap())
                    .unwrap();
            }
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        let events = [InputEvent::Text("ls".to_owned())];
        handle.send_input("w1:p1", &events).unwrap();
        handle.set_focus(true).unwrap();

        let input = sent_rx.recv_timeout(WAIT).unwrap();
        let mut d = Dec::new(&input);
        assert_eq!(d.vu().unwrap(), u64::from(C_PANE_INPUT));
        assert_eq!(
            input,
            ClientMsg::Input {
                pane_id: "w1:p1",
                events: &events,
            }
            .encode()
        );
        let focus = sent_rx.recv_timeout(WAIT).unwrap();
        assert_eq!(focus, ClientMsg::Focus(true).encode());
        assert_eq!(Dec::new(&focus).vu().unwrap(), u64::from(C_FOCUS));
    }

    #[test]
    fn a_full_outbox_refuses_input_with_full() {
        let (_dir, path) = serve_each(1, |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            // Never read: the writer blocks once the socket buffer is full.
            thread::sleep(WAIT);
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        let events = [InputEvent::Paste("x".repeat(32 * 1024))];
        let mut refused = None;
        for _ in 0..1_000 {
            if let Err(e) = handle.send_input("w1:p1", &events) {
                refused = Some(e);
                break;
            }
        }
        assert_eq!(refused, Some(LinkSendError::Full));
    }

    #[test]
    fn a_resize_is_stored_for_the_next_hello() {
        let (hello_tx, hello_rx) = mpsc::channel::<Value>();
        let (_dir, path) = serve_each(2, move |mut stream, n| {
            let hello = read_frame(&mut stream).unwrap().unwrap();
            if n == 0 {
                assert_eq!(hello_body(&hello)["surface_size"]["cols"], 60);
                put(&mut stream, &welcome("0.9.3"));
                // The resize is the first frame after the hello. Then close.
                read_frame(&mut stream).unwrap().unwrap();
            } else {
                hello_tx.send(hello_body(&hello)).unwrap();
                put(&mut stream, &welcome("0.9.3"));
                thread::sleep(WAIT);
            }
        });
        let (_link, handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        let size = GridSize { cols: 40, rows: 8 };
        handle.resize(size, CELL).unwrap();
        // The first connection closes, and the link reconnects with the new size.
        assert_eq!(
            next_state(&rx),
            LinkState::NotRunning("herdr closed the endpoint".to_owned())
        );
        let body = hello_rx.recv_timeout(WAIT).unwrap();
        assert_eq!(body["surface_size"]["cols"], 40);
        assert_eq!(body["surface_size"]["rows"], 8);
        assert_eq!(body["cell_width_px"], 8);
        assert_eq!(body["cell_height_px"], 16);
    }

    #[test]
    fn dropping_the_link_sends_a_detach() {
        let (detach_tx, detach_rx) = mpsc::channel::<Vec<u8>>();
        let (_dir, path) = serve_each(1, move |mut stream, _| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            let frame = read_frame(&mut stream).unwrap().unwrap();
            detach_tx.send(frame).unwrap();
        });
        let (link, _handle, rx) = start(LinkTarget::Socket(path));
        next_state(&rx);
        next_state(&rx);
        drop(link);
        let frame = detach_rx.recv_timeout(WAIT).unwrap();
        assert_eq!(frame, ClientMsg::Detach.encode());
        assert_eq!(Dec::new(&frame).vu().unwrap(), u64::from(C_DETACH));
    }
}
