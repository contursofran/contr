//! Starts an isolated `herdr server` in a temporary directory and checks the
//! reader against it: the connection, the first tree, the agent status of a
//! reported agent, then the reconnect after the server restarts, also when
//! `herdr server stop` ends the server and a new server process starts. It never
//! touches your own herdr: the server gets its own HOME, config and sockets,
//! and its process is killed when the test ends.
//!
//! `CONTR_HERDR_BIN=$(which herdr) cargo test -p contr-herdr --test live -- --ignored --nocapture`

#![allow(clippy::unwrap_used, clippy::expect_used)]

use std::ffi::OsStr;
use std::fs::OpenOptions;
use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use async_channel::{Receiver, TryRecvError};
use contr_herdr::wire::request_line;
use contr_herdr::{AgentInfo, AgentState, ConnState, HerdrTree, HerdrUpdate, Target, spawn_reader};
use serde_json::{Value, json};
use tempfile::TempDir;
use tracing_subscriber::EnvFilter;

/// The isolated server and its directory. Dropping it kills the server and
/// waits for it, also when the test fails. The directory is removed after that.
struct Server {
    child: Child,
    dir: TempDir,
}

impl Drop for Server {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Server {
    /// Starts `bin server` with every path inside a new directory under `/tmp`.
    /// macOS limits socket paths to 104 bytes, so the default temp dir is too long.
    fn start(bin: &OsStr) -> Self {
        let dir = tempfile::Builder::new()
            .prefix("chl")
            .tempdir_in("/tmp")
            .expect("create the test directory");
        let home = dir.path();
        for sub in ["config", "state", "data", "cache"] {
            std::fs::create_dir_all(home.join(sub)).expect("create an XDG directory");
        }
        std::fs::write(home.join("config.toml"), "").expect("write the empty herdr config");
        let child = spawn_child(bin, home);
        Self { child, dir }
    }

    /// Kills the server and waits for it to exit. Its directory and sockets stay.
    fn stop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }

    /// Starts a new server on the same paths, so the reader's socket path is unchanged.
    fn restart(&mut self, bin: &OsStr) {
        self.stop();
        self.child = spawn_child(bin, self.dir.path());
    }

    /// Asks the server to stop with `herdr server stop`, as a user does, and
    /// waits up to ten seconds for its process to exit.
    fn stop_with_command(&mut self, bin: &OsStr) {
        let status = isolated(Command::new(bin), self.dir.path())
            .args(["server", "stop"])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .expect("run herdr server stop");
        assert!(status.success(), "herdr server stop failed: {status}");
        let deadline = Instant::now() + Duration::from_secs(10);
        while self.child.try_wait().expect("poll the server").is_none() {
            assert!(
                Instant::now() < deadline,
                "the server did not exit after herdr server stop"
            );
            thread::sleep(Duration::from_millis(20));
        }
    }

    /// Starts a new server process on the same paths after the old one exited.
    fn start_again(&mut self, bin: &OsStr) {
        self.child = spawn_child(bin, self.dir.path());
    }

    /// Waits until the server answers `ping` on `sock`. Fails after ten seconds,
    /// or at once when the server has exited.
    fn wait_until_answering(&mut self, sock: &Path) {
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            let err = match call(sock, "ping", json!({})) {
                Ok(_) => return,
                Err(err) => err,
            };
            assert!(
                self.child.try_wait().expect("poll the server").is_none(),
                "the isolated herdr server exited during startup"
            );
            assert!(
                Instant::now() < deadline,
                "the isolated herdr server did not answer ping: {err}"
            );
            thread::sleep(Duration::from_millis(20));
        }
    }
}

/// Starts `bin server` with its paths inside `home`, which must hold the empty config. Its output
/// is appended to `herdr.log` there, so a restart keeps the log of the earlier server.
fn spawn_child(bin: &OsStr, home: &Path) -> Child {
    let log = OpenOptions::new()
        .create(true)
        .append(true)
        .open(home.join("herdr.log"))
        .expect("open the server log");
    isolated(Command::new(bin), home)
        .arg("server")
        .stdin(Stdio::null())
        .stdout(Stdio::from(log.try_clone().expect("clone the server log")))
        .stderr(Stdio::from(log))
        .spawn()
        .expect("start the isolated herdr server")
}

/// `cmd` with an environment that holds only the paths inside `home`, so it
/// can reach no other herdr.
fn isolated(mut cmd: Command, home: &Path) -> Command {
    cmd.env_clear()
        .env("HOME", home)
        .env("XDG_CONFIG_HOME", home.join("config"))
        .env("XDG_STATE_HOME", home.join("state"))
        .env("XDG_DATA_HOME", home.join("data"))
        .env("XDG_CACHE_HOME", home.join("cache"))
        .env("XDG_RUNTIME_DIR", home)
        .env("TMPDIR", home)
        .env("HERDR_CONFIG_PATH", home.join("config.toml"))
        .env("HERDR_SOCKET_PATH", home.join("a.sock"))
        .env("HERDR_CLIENT_SOCKET_PATH", home.join("a-client.sock"))
        .env("SHELL", "/bin/sh")
        .env("TERM", "xterm-256color")
        .env("PATH", "/usr/bin:/bin:/usr/sbin:/sbin");
    cmd
}

/// One request on a fresh connection to `sock`. Returns the reply's `result`, or
/// the server's error object or the I/O error as text. The reader is the client
/// under test; this helper only sends the requests that set up the scene.
fn call(sock: &Path, method: &str, params: Value) -> Result<Value, String> {
    let mut stream = UnixStream::connect(sock).map_err(|e| e.to_string())?;
    stream
        .set_read_timeout(Some(Duration::from_secs(5)))
        .map_err(|e| e.to_string())?;
    stream
        .write_all(request_line("contr-live", method, params).as_bytes())
        .map_err(|e| e.to_string())?;
    let mut line = String::new();
    BufReader::new(stream)
        .read_line(&mut line)
        .map_err(|e| e.to_string())?;
    let reply: Value = serde_json::from_str(&line).map_err(|e| format!("{e}: {line}"))?;
    if let Some(error) = reply.get("error") {
        return Err(error.to_string());
    }
    reply
        .get("result")
        .cloned()
        .ok_or_else(|| format!("no result in {line}"))
}

/// Takes updates from `rx` until `want` accepts one, and returns what it
/// extracted. Updates it rejects are skipped. Fails after `within`.
fn wait_for<T>(
    rx: &Receiver<HerdrUpdate>,
    within: Duration,
    what: &str,
    mut want: impl FnMut(&HerdrUpdate) -> Option<T>,
) -> T {
    let deadline = Instant::now() + within;
    loop {
        match rx.try_recv() {
            Ok(update) => {
                if let Some(found) = want(&update) {
                    return found;
                }
            }
            Err(TryRecvError::Empty) => {
                assert!(
                    Instant::now() < deadline,
                    "timed out after {within:?} waiting for {what}"
                );
                thread::sleep(Duration::from_millis(20));
            }
            Err(TryRecvError::Closed) => panic!("the reader stopped while waiting for {what}"),
        }
    }
}

/// The agent of the first pane in `tree` that has one.
fn first_agent(tree: &HerdrTree) -> Option<&AgentInfo> {
    tree.workspaces
        .iter()
        .flat_map(|w| &w.tabs)
        .flat_map(|t| &t.panes)
        .find_map(|p| p.agent.as_ref())
}

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn isolated_server_reports_tree_agent_status_and_reconnects() {
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::new("contr_herdr=info"))
        .with_test_writer()
        .try_init();
    let bin = std::env::var_os("CONTR_HERDR_BIN").expect("set CONTR_HERDR_BIN to the herdr binary");
    let mut server = Server::start(&bin);
    let sock = server.dir.path().join("a.sock");
    server.wait_until_answering(&sock);

    // A fresh server has no workspace, and the reader only reports what exists.
    let created =
        call(&sock, "workspace.create", json!({ "cwd": "/tmp" })).expect("create a workspace");
    assert_eq!(created["type"], "workspace_created", "reply: {created}");

    let (tx, rx) = async_channel::bounded(64);
    let reader = spawn_reader(Target::Socket(sock.clone()), tx).expect("spawn the reader");

    let server_version = wait_for(
        &rx,
        Duration::from_secs(10),
        "Connected",
        |update| match update {
            HerdrUpdate::State(ConnState::Connected { server_version }) => {
                Some(server_version.clone())
            }
            _ => None,
        },
    );
    assert!(!server_version.is_empty(), "herdr reported no version");

    let tree = wait_for(
        &rx,
        Duration::from_secs(10),
        "a tree with one workspace, tab and pane",
        |update| match update {
            HerdrUpdate::Tree(tree)
                if tree.workspaces.len() == 1
                    && tree.tab_count() == 1
                    && tree.pane_count() == 1 =>
            {
                Some(Arc::clone(tree))
            }
            _ => None,
        },
    );
    let pane_id = tree.workspaces[0].tabs[0].panes[0].id.clone();
    assert!(
        first_agent(&tree).is_none(),
        "a plain shell pane has an agent"
    );

    for status in [AgentState::Working, AgentState::Blocked] {
        call(
            &sock,
            "pane.report_agent",
            json!({
                "pane_id": pane_id,
                "source": "contr-test",
                "agent": "claude",
                "state": status.label(),
            }),
        )
        .expect("report the agent");
        wait_for(
            &rx,
            Duration::from_secs(3),
            status.label(),
            |update| match update {
                HerdrUpdate::Tree(tree) => first_agent(tree)
                    .filter(|agent| agent.name == "claude" && agent.status == status)
                    .map(|_| ()),
                HerdrUpdate::State(_) => None,
            },
        );
    }

    // The server stops and the reader reports it. A new server on the same
    // paths brings the connection back.
    server.stop();
    wait_for(
        &rx,
        Duration::from_secs(10),
        "NotRunning after the server stopped",
        |update| match update {
            HerdrUpdate::State(ConnState::NotRunning { .. }) => Some(()),
            _ => None,
        },
    );
    server.restart(&bin);
    server.wait_until_answering(&sock);
    wait_for(
        &rx,
        Duration::from_secs(15),
        "Connected after the restart",
        |update| match update {
            HerdrUpdate::State(ConnState::Connected { .. }) => Some(()),
            _ => None,
        },
    );
    wait_for(
        &rx,
        Duration::from_secs(10),
        "a tree after the restart",
        |update| match update {
            HerdrUpdate::Tree(_) => Some(()),
            HerdrUpdate::State(_) => None,
        },
    );
    drop(reader);
}

/// Reports `status` for an agent in `pane_id` and waits until a tree with that
/// status reaches the reader.
fn report_and_wait(sock: &Path, rx: &Receiver<HerdrUpdate>, pane_id: &str, status: AgentState) {
    call(
        sock,
        "pane.report_agent",
        json!({
            "pane_id": pane_id,
            "source": "contr-test",
            "agent": "claude",
            "state": status.label(),
        }),
    )
    .expect("report the agent");
    wait_for(
        rx,
        Duration::from_secs(5),
        status.label(),
        |update| match update {
            HerdrUpdate::Tree(tree) => first_agent(tree)
                .filter(|agent| agent.name == "claude" && agent.status == status)
                .map(|_| ()),
            HerdrUpdate::State(_) => None,
        },
    );
}

/// The id of the first pane in the server's tree. Creates a workspace first
/// when the server has none.
fn first_pane(sock: &Path) -> String {
    let pane = |result: &Value| {
        result["snapshot"]["panes"][0]["pane_id"]
            .as_str()
            .map(str::to_owned)
    };
    let snapshot = call(sock, "session.snapshot", json!({})).expect("take a snapshot");
    if let Some(id) = pane(&snapshot) {
        return id;
    }
    let created =
        call(sock, "workspace.create", json!({ "cwd": "/tmp" })).expect("create a workspace");
    assert_eq!(created["type"], "workspace_created", "reply: {created}");
    let snapshot = call(sock, "session.snapshot", json!({})).expect("take a snapshot");
    pane(&snapshot).unwrap_or_else(|| panic!("no pane after workspace.create: {snapshot}"))
}

/// `herdr server stop` ends the server, then a new server process starts on
/// the same paths. The reader reports that herdr is gone, connects to the new
/// server and passes on a status change made there. With `visible` false the
/// reader runs as for a hidden window, with no refresh timer.
fn stop_and_new_server_reach_the_reader(visible: bool) {
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::new("contr_herdr=info"))
        .with_test_writer()
        .try_init();
    let bin = std::env::var_os("CONTR_HERDR_BIN").expect("set CONTR_HERDR_BIN to the herdr binary");
    let mut server = Server::start(&bin);
    let sock = server.dir.path().join("a.sock");
    server.wait_until_answering(&sock);
    let pane_id = first_pane(&sock);

    let (tx, rx) = async_channel::bounded(64);
    let reader = spawn_reader(Target::Socket(sock.clone()), tx).expect("spawn the reader");
    reader.set_visible(visible);
    wait_for(&rx, Duration::from_secs(10), "Connected", |update| {
        matches!(update, HerdrUpdate::State(ConnState::Connected { .. })).then_some(())
    });
    report_and_wait(&sock, &rx, &pane_id, AgentState::Idle);

    server.stop_with_command(&bin);
    wait_for(
        &rx,
        Duration::from_secs(10),
        "NotRunning after herdr server stop",
        |update| matches!(update, HerdrUpdate::State(ConnState::NotRunning { .. })).then_some(()),
    );
    server.start_again(&bin);
    server.wait_until_answering(&sock);
    wait_for(
        &rx,
        Duration::from_secs(15),
        "Connected to the new server",
        |update| matches!(update, HerdrUpdate::State(ConnState::Connected { .. })).then_some(()),
    );
    let pane_id = first_pane(&sock);
    report_and_wait(&sock, &rx, &pane_id, AgentState::Working);
    drop(reader);
}

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn server_stop_and_a_new_server_reach_a_shown_reader() {
    stop_and_new_server_reach_the_reader(true);
}

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn server_stop_and_a_new_server_reach_a_hidden_reader() {
    stop_and_new_server_reach_the_reader(false);
}
