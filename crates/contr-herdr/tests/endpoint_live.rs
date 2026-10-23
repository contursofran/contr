//! Live check of the endpoint client against an isolated `herdr server`: the
//! handshake, a command's colour and bold reaching the surface, and a resize.
//!
//! Run with:
//! `CONTR_HERDR_BIN=$(command -v herdr) cargo test -p contr-herdr --test endpoint_live -- --ignored --nocapture`
//!
//! The server runs in a temp HOME with a cleared environment and its own
//! socket. It never touches the owner's herdr.

#![allow(clippy::unwrap_used, clippy::expect_used)]

use std::fs::File;
use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use contr_herdr::endpoint::conn::Conn;
use contr_herdr::endpoint::messages::{
    CellPx, ClientMsg, GridSize, InputEvent, KeyCode, SNAPSHOT_KIND, ServerMsg, ShellSnapshot,
    VersionCheck,
};
use contr_herdr::endpoint::surface::Surface;
use contr_herdr::wire::request_line;
use serde_json::{Value, json};

const WAIT: Duration = Duration::from_secs(5);
const POLL: Duration = Duration::from_millis(50);
const SIZE: GridSize = GridSize { cols: 60, rows: 12 };
const SMALL: GridSize = GridSize { cols: 40, rows: 8 };
const CELL: CellPx = CellPx {
    width: 8,
    height: 16,
};

/// A temp dir that holds the server's HOME, its sockets and its log. It is
/// under `/tmp` because a socket path must stay under 104 bytes on macOS.
struct Sandbox {
    dir: tempfile::TempDir,
}

impl Sandbox {
    fn new() -> Self {
        let dir = tempfile::Builder::new()
            .prefix("cl")
            .tempdir_in("/tmp")
            .unwrap();
        for sub in [
            "home",
            "xdg-config",
            "xdg-state",
            "xdg-data",
            "xdg-cache",
            "xdg-run",
            "tmp",
        ] {
            std::fs::create_dir_all(dir.path().join(sub)).unwrap();
        }
        std::fs::write(dir.path().join("config.toml"), "").unwrap();
        Self { dir }
    }

    fn path(&self, name: &str) -> PathBuf {
        self.dir.path().join(name)
    }

    fn api_socket(&self) -> PathBuf {
        self.path("a.sock")
    }

    /// herdr's endpoint socket for `a.sock`: `<stem>-client.sock`.
    fn endpoint_socket(&self) -> PathBuf {
        self.path("a-client.sock")
    }

    /// Starts `herdr server` with a cleared environment and waits until both
    /// sockets exist. The returned guard kills the server when it drops.
    fn start_server(&self, bin: &str) -> Server {
        let log = File::create(self.path("server.log")).unwrap();
        let child = Command::new(bin)
            .arg("server")
            .env_clear()
            .env("HOME", self.path("home"))
            .env("XDG_CONFIG_HOME", self.path("xdg-config"))
            .env("XDG_STATE_HOME", self.path("xdg-state"))
            .env("XDG_DATA_HOME", self.path("xdg-data"))
            .env("XDG_CACHE_HOME", self.path("xdg-cache"))
            .env("XDG_RUNTIME_DIR", self.path("xdg-run"))
            .env("TMPDIR", self.path("tmp"))
            .env("HERDR_CONFIG_PATH", self.path("config.toml"))
            .env("HERDR_SOCKET_PATH", self.api_socket())
            .env("SHELL", "/bin/sh")
            .env("TERM", "xterm-256color")
            .env("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
            .stdin(Stdio::null())
            .stdout(log.try_clone().unwrap())
            .stderr(log)
            .spawn()
            .unwrap();
        let server = Server { child };
        self.wait_for(&self.api_socket());
        self.wait_for(&self.endpoint_socket());
        server
    }

    fn wait_for(&self, path: &Path) {
        let deadline = Instant::now() + Duration::from_secs(10);
        while !path.exists() {
            assert!(
                Instant::now() < deadline,
                "{} did not appear; server log:\n{}",
                path.display(),
                self.log_tail()
            );
            thread::sleep(POLL);
        }
    }

    fn log_tail(&self) -> String {
        let text = std::fs::read_to_string(self.path("server.log")).unwrap_or_default();
        let lines: Vec<&str> = text.lines().collect();
        lines[lines.len().saturating_sub(20)..].join("\n")
    }

    /// One workspace, so the server has a focused pane. The request is a raw
    /// JSON line on the API socket of this isolated server.
    fn create_workspace(&self) {
        let mut stream = UnixStream::connect(self.api_socket()).unwrap();
        stream.set_read_timeout(Some(WAIT)).unwrap();
        let request = request_line("t", "workspace.create", json!({ "cwd": "/tmp" }));
        stream.write_all(request.as_bytes()).unwrap();
        let mut line = String::new();
        BufReader::new(stream).read_line(&mut line).unwrap();
        let reply: Value = serde_json::from_str(&line).unwrap();
        assert!(
            reply.get("result").is_some(),
            "workspace.create failed: {line}"
        );
    }
}

/// Kills and reaps the server on drop.
struct Server {
    child: Child,
}

impl Drop for Server {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// What the test has seen from the server so far.
#[derive(Default)]
struct Shown {
    snapshot: Option<ShellSnapshot>,
    surface: Option<Surface>,
}

impl Shown {
    fn focused_pane(&self) -> Option<String> {
        self.snapshot.as_ref()?.focused_pane_id.clone()
    }

    fn size_is(&self, size: GridSize) -> bool {
        self.surface.as_ref().is_some_and(|s| s.size == size)
    }
}

/// Reads server messages into `shown` until `done` holds. Fails after `WAIT`.
fn pump(conn: &mut Conn, shown: &mut Shown, what: &str, done: impl Fn(&Shown) -> bool) {
    let deadline = Instant::now() + WAIT;
    while !done(shown) {
        assert!(Instant::now() < deadline, "no {what} within {WAIT:?}");
        match conn.recv().unwrap() {
            Some(ServerMsg::Control { kind, data }) if kind == SNAPSHOT_KIND => {
                shown.snapshot = Some(serde_json::from_str(&data).unwrap());
            }
            Some(ServerMsg::Surface(frame)) => shown.surface = Some(frame.0),
            Some(ServerMsg::Patch(patch)) => {
                let surface = shown.surface.as_mut().expect("a patch before any surface");
                surface
                    .apply_patch(patch)
                    .expect("a patch that does not continue the surface");
            }
            Some(ServerMsg::Shutdown(reason)) => panic!("herdr shut down: {reason:?}"),
            Some(ServerMsg::ShellError(message)) => panic!("herdr shell error: {message}"),
            _ => {}
        }
    }
}

/// The row and column where `RED ok` starts on screen.
fn find_red(surface: &Surface) -> Option<(usize, usize)> {
    for (y, row) in surface.rows.iter().enumerate() {
        for x in 0..row.len() {
            let word: String = row[x..].iter().take(6).map(|c| c.text.as_str()).collect();
            if word == "RED ok" {
                return Some((y, x));
            }
        }
    }
    None
}

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn handshake_colour_and_resize_against_an_isolated_herdr() {
    let bin = std::env::var("CONTR_HERDR_BIN").expect("set CONTR_HERDR_BIN to the herdr binary");
    let sandbox = Sandbox::new();
    let _server = sandbox.start_server(&bin);
    sandbox.create_workspace();

    let mut conn = Conn::open(&sandbox.endpoint_socket(), SIZE, CELL, WAIT).unwrap();
    println!(
        "herdr server_version={} generation={}",
        conn.welcome.server_version, conn.welcome.generation
    );
    if let VersionCheck::Untested(version) = &conn.version {
        println!("herdr {version} is not in the tested list");
    }
    assert_eq!(conn.welcome.generation, 1);
    conn.set_read_timeout(Some(POLL)).unwrap();

    let mut shown = Shown::default();
    pump(&mut conn, &mut shown, "snapshot and a 60x12 surface", |s| {
        s.focused_pane().is_some() && s.size_is(SIZE)
    });

    let pane = shown.focused_pane().expect("a focused pane");
    let events = [
        InputEvent::Text("printf '\\033[1;31mRED\\033[0m ok\\n'".to_owned()),
        InputEvent::Key {
            code: KeyCode::Enter,
            mods: 0,
            repeat: false,
        },
    ];
    conn.send(&ClientMsg::Input {
        pane_id: &pane,
        events: &events,
    })
    .unwrap();
    pump(&mut conn, &mut shown, "RED ok on screen", |s| {
        s.surface.as_ref().and_then(find_red).is_some()
    });

    let surface = shown.surface.as_ref().unwrap();
    let (y, x) = find_red(surface).unwrap();
    let red = &surface.rows[y][x];
    println!(
        "RED ok at row {y}, column {x}: fg={:#010x} attrs={:#06x}",
        red.fg, red.attrs
    );
    assert_eq!(red.fg, 0x0100_0001, "indexed colour 1 (red)");
    assert_eq!(red.attrs & 1, 1, "bold");

    conn.send(&ClientMsg::Resize {
        size: SMALL,
        cell: CELL,
    })
    .unwrap();
    pump(
        &mut conn,
        &mut shown,
        "a 40x8 surface after the resize",
        |s| s.size_is(SMALL),
    );
    conn.send(&ClientMsg::Detach).unwrap();
}
