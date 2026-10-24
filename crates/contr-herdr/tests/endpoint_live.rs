//! Live check of the endpoint client against an isolated `herdr server`: the
//! handshake, a command's colour and bold reaching the surface, a resize, that
//! tab focus changes (`tab.create`, `tab.focus`) move the surface, and that a
//! wheel scrolls the history and a key returns to the live screen.
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
    VersionCheck, WheelDir,
};
use contr_herdr::endpoint::surface::{Cell, Rect, ScrollMetrics, Surface, SurfacePane};
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
    /// JSON line on the API socket of this isolated server. Returns the reply.
    fn create_workspace(&self) -> Value {
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
        reply
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

/// The pane id of the focused pane in the surface, once a surface has arrived.
fn surface_pane(shown: &Shown) -> Option<String> {
    shown
        .surface
        .as_ref()?
        .focused_pane()
        .map(|p| p.pane_id.clone())
}

/// The text of one row: each cell's text, in column order.
fn row_text(row: &[Cell]) -> String {
    row.iter().map(|cell| cell.text.as_str()).collect()
}

/// Whether a row of the surface reads exactly `line` once its padding is trimmed.
/// Only the focused pane's inner columns count: herdr paints its scrollbar in the
/// column after them.
fn shows(shown: &Shown, line: &str) -> bool {
    let Some(surface) = shown.surface.as_ref() else {
        return false;
    };
    let Some(pane) = surface.focused_pane() else {
        return false;
    };
    let width = usize::from(pane.inner_rect.width);
    surface
        .rows
        .iter()
        .any(|row| row_text(&row[..width.min(row.len())]).trim() == line)
}

/// The scroll position of the focused pane, once a surface has arrived.
fn focused_scroll(shown: &Shown) -> Option<ScrollMetrics> {
    shown.surface.as_ref()?.focused_pane()?.scroll
}

/// One wheel step at the pane's first cell.
fn wheel(dir: WheelDir, lines: u16) -> InputEvent {
    InputEvent::Wheel {
        dir,
        column: 0,
        row: 0,
        mods: 0,
        lines,
    }
}

#[test]
fn surface_pane_is_the_focused_pane_of_the_surface() {
    let pane = |id: &str, focused: bool| SurfacePane {
        pane_id: id.to_owned(),
        rect: Rect::default(),
        inner_rect: Rect::default(),
        focused,
        ..Default::default()
    };
    let mut shown = Shown::default();
    assert_eq!(surface_pane(&shown), None, "no surface yet");

    let surface = |panes: Vec<SurfacePane>| Surface {
        boot_id: "b1".to_owned(),
        projection_revision: 0,
        revision: 0,
        size: SIZE,
        rows: Vec::new(),
        cursor: None,
        panes,
    };
    shown.surface = Some(surface(vec![pane("w1:p1", false)]));
    assert_eq!(surface_pane(&shown), None, "a surface with no focused pane");

    shown.surface = Some(surface(vec![pane("w1:p1", false), pane("w1:p2", true)]));
    assert_eq!(surface_pane(&shown).as_deref(), Some("w1:p2"));
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

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn tab_focus_moves_the_endpoint_surface() {
    let bin = std::env::var("CONTR_HERDR_BIN").expect("set CONTR_HERDR_BIN to the herdr binary");
    let sandbox = Sandbox::new();
    let _server = sandbox.start_server(&bin);
    let reply = sandbox.create_workspace();
    println!("workspace.create reply: {reply}");
    let ws = reply["result"]["workspace"]["workspace_id"]
        .as_str()
        .expect("a workspace id in the reply")
        .to_owned();
    let first_tab = reply["result"]["tab"]["tab_id"]
        .as_str()
        .expect("a tab id in the reply")
        .to_owned();

    let mut conn = Conn::open(&sandbox.endpoint_socket(), SIZE, CELL, WAIT).unwrap();
    conn.set_read_timeout(Some(POLL)).unwrap();

    let mut shown = Shown::default();
    pump(
        &mut conn,
        &mut shown,
        "a surface with a focused pane",
        |s| surface_pane(s).is_some(),
    );
    let first_pane = surface_pane(&shown).expect("a focused pane");
    println!("first pane {first_pane} in tab {first_tab}");

    // tab.create sends focus: true, so the API focus change must move the surface.
    contr_herdr::command::create_tab(&sandbox.api_socket(), &ws, WAIT).unwrap();
    pump(&mut conn, &mut shown, "a surface of the new tab", |s| {
        surface_pane(s).is_some_and(|p| p != first_pane)
    });
    println!(
        "new tab pane {}",
        surface_pane(&shown).expect("a focused pane")
    );

    // The title-bar click path: tab.focus on the first tab moves the surface back.
    contr_herdr::command::focus_tab(&sandbox.api_socket(), &first_tab, WAIT).unwrap();
    pump(
        &mut conn,
        &mut shown,
        "the first tab's surface again",
        |s| surface_pane(s).as_deref() == Some(first_pane.as_str()),
    );

    conn.send(&ClientMsg::Detach).unwrap();
}

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn wheel_scrolls_history_and_a_key_returns_to_live() {
    let bin = std::env::var("CONTR_HERDR_BIN").expect("set CONTR_HERDR_BIN to the herdr binary");
    let sandbox = Sandbox::new();
    let _server = sandbox.start_server(&bin);
    sandbox.create_workspace();

    let mut conn = Conn::open(&sandbox.endpoint_socket(), SIZE, CELL, WAIT).unwrap();
    conn.set_read_timeout(Some(POLL)).unwrap();

    let mut shown = Shown::default();
    pump(&mut conn, &mut shown, "snapshot and a 60x12 surface", |s| {
        s.focused_pane().is_some() && s.size_is(SIZE)
    });
    let pane = shown.focused_pane().expect("a focused pane");

    // 500 lines push the top of the output into herdr's history. "500" is the
    // last output line, so it is on screen until the view scrolls.
    let events = [
        InputEvent::Text("seq 1 500".to_owned()),
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
    pump(&mut conn, &mut shown, "500 on screen", |s| shows(s, "500"));

    // A wheel step up moves the view into the history: "500" leaves the screen and
    // the pane reports an offset from the bottom.
    conn.send(&ClientMsg::Input {
        pane_id: &pane,
        events: &[wheel(WheelDir::Up, 20)],
    })
    .unwrap();
    pump(&mut conn, &mut shown, "the history above the screen", |s| {
        !shows(s, "500") && focused_scroll(s).is_some_and(|m| m.offset_from_bottom > 0)
    });
    let up = focused_scroll(&shown).expect("scroll metrics after the wheel");
    println!("wheel up 20 lines: {up:?}");

    // A wheel step down moves the view back by 5 lines. Plan: 15 rows above the
    // bottom. If herdr reports another number, the offset must still fall.
    let before = up.offset_from_bottom;
    conn.send(&ClientMsg::Input {
        pane_id: &pane,
        events: &[wheel(WheelDir::Down, 5)],
    })
    .unwrap();
    pump(&mut conn, &mut shown, "the offset to change", |s| {
        focused_scroll(s).is_some_and(|m| m.offset_from_bottom != before)
    });
    let after = focused_scroll(&shown)
        .expect("scroll metrics after the wheel")
        .offset_from_bottom;
    println!("wheel down 5 lines: offset {after} (was {before})");
    if after != 15 {
        println!("herdr reports offset {after}, not 15; checking only that it fell");
    }
    assert!(
        after < before,
        "a wheel down must move the view toward the bottom"
    );

    // A key returns to the live screen: "500" is back and the view is at the bottom.
    let events = [InputEvent::Key {
        code: KeyCode::Left,
        mods: 0,
        repeat: false,
    }];
    conn.send(&ClientMsg::Input {
        pane_id: &pane,
        events: &events,
    })
    .unwrap();
    pump(&mut conn, &mut shown, "the live screen again", |s| {
        shows(s, "500") && focused_scroll(s).is_none_or(|m| m.offset_from_bottom == 0)
    });
    println!("back to live: scroll {:?}", focused_scroll(&shown));

    conn.send(&ClientMsg::Detach).unwrap();
}
