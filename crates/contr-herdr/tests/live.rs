//! Starts an isolated herdr daemon in a temporary directory and checks that the
//! reader connects and receives a tree. It never touches the owner's herdr.
//!
//! `CONTR_HERDR_BIN=$(which herdr) cargo test -p contr-herdr --test live -- --ignored --nocapture`
#![cfg(unix)]
#![allow(clippy::unwrap_used, clippy::expect_used)]

use std::fs::{self, DirBuilder, File};
use std::os::unix::fs::DirBuilderExt;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use contr_herdr::{ConnState, ConnectTarget, HerdrUpdate, spawn_reader};

/// Kills and reaps the isolated daemon and removes its directory, also when the test fails.
struct Daemon {
    child: Child,
    dir: PathBuf,
}

impl Drop for Daemon {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = fs::remove_dir_all(&self.dir);
    }
}

#[test]
#[ignore = "needs CONTR_HERDR_BIN; starts an isolated herdr"]
fn isolated_daemon_publishes_a_tree() {
    let bin = std::env::var_os("CONTR_HERDR_BIN").expect("set CONTR_HERDR_BIN to the herdr binary");
    // Socket paths must stay under 104 bytes, so the directory is short and in /tmp.
    let dir = PathBuf::from(format!("/tmp/contr-herdr-live-{}", std::process::id()));
    let _ = fs::remove_dir_all(&dir);
    DirBuilder::new()
        .mode(0o700)
        .create(&dir)
        .expect("create the test directory");
    for sub in ["config", "state", "data", "cache"] {
        fs::create_dir_all(dir.join(sub)).expect("create an XDG directory");
    }
    fs::write(dir.join("config.toml"), "").expect("write the empty herdr config");
    let client_sock = dir.join("a-client.sock");
    let log = File::create(dir.join("daemon.log")).expect("create the daemon log");

    let child = Command::new(&bin)
        .arg("server")
        .env_clear()
        .env("HOME", &dir)
        .env("XDG_CONFIG_HOME", dir.join("config"))
        .env("XDG_STATE_HOME", dir.join("state"))
        .env("XDG_DATA_HOME", dir.join("data"))
        .env("XDG_CACHE_HOME", dir.join("cache"))
        .env("XDG_RUNTIME_DIR", &dir)
        .env("TMPDIR", &dir)
        .env("HERDR_CONFIG_PATH", dir.join("config.toml"))
        .env("HERDR_SOCKET_PATH", dir.join("a.sock"))
        .env("HERDR_CLIENT_SOCKET_PATH", &client_sock)
        .env("SHELL", "/bin/sh")
        .env("TERM", "xterm-256color")
        .env("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
        .stdin(Stdio::null())
        .stdout(Stdio::from(log.try_clone().expect("clone the daemon log")))
        .stderr(Stdio::from(log))
        .spawn()
        .expect("start the isolated herdr server");
    let mut daemon = Daemon {
        child,
        dir: dir.clone(),
    };

    let deadline = Instant::now() + Duration::from_secs(10);
    while !client_sock.exists() {
        assert!(
            daemon.child.try_wait().unwrap().is_none(),
            "the daemon exited during startup"
        );
        assert!(
            Instant::now() < deadline,
            "the daemon socket never appeared"
        );
        thread::sleep(Duration::from_millis(20));
    }

    let (tx, rx) = async_channel::bounded(64);
    let reader = spawn_reader(ConnectTarget::Socket(client_sock), tx).expect("spawn the reader");
    let deadline = Instant::now() + Duration::from_secs(20);
    let mut connected = false;
    let mut tree_ok = false;
    while !tree_ok && Instant::now() < deadline {
        match rx.try_recv() {
            Ok(HerdrUpdate::State(ConnState::Connected { server_version })) => {
                println!("connected server_version={server_version}");
                connected = true;
            }
            Ok(HerdrUpdate::Tree(tree)) => {
                println!(
                    "tree rev={} workspaces={} tabs={} panes={}",
                    tree.revision,
                    tree.workspaces.len(),
                    tree.tab_count(),
                    tree.pane_count()
                );
                tree_ok = connected
                    && !tree.workspaces.is_empty()
                    && tree.tab_count() >= 1
                    && tree.pane_count() >= 1;
            }
            Ok(HerdrUpdate::State(state)) => println!("state {state:?}"),
            Err(_) => thread::sleep(Duration::from_millis(20)),
        }
    }

    assert!(connected, "the reader never reported Connected");
    assert!(
        tree_ok,
        "the reader never received a tree with a workspace, tab and pane"
    );
    drop(reader);
    drop(daemon);
}
