//! The coordinator's board as the UI holds it. The bridge pane is the herdr pane whose pane token
//! `contr_board` names the session and the board revision. The board file of that session is read on
//! the background executor, and only when the session or the revision changes. Nothing here polls or
//! sets a timer: the entity reacts to `Herdr` notifications, which stop while the window is hidden.

use crate::board_model::{
    BRIDGE_TOKEN, BoardSnapshot, BridgeRef, board_path, parse_board, parse_bridge_token,
};
use crate::herdr::GlobalHerdr;
use contr_herdr::HerdrTree;
use gpui::{App, AppContext, Context, Entity, Global, Subscription, Task};
use std::ffi::OsString;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::sync::Arc;

/// The environment variable that replaces the board directory.
pub const BOARD_DIR_ENV: &str = "CONTR_BOARD_DIR";
/// The board directory below `$HOME` when `CONTR_BOARD_DIR` is not set.
const BOARD_DIR_UNDER_HOME: &str = ".local/state/herdr-board";
/// The largest board file that is read. A board is a few KB; a larger file is refused unparsed.
pub const MAX_BOARD_BYTES: u64 = 1 << 20;
/// The problem line when there is no board directory (no `HOME` and no override).
pub const NO_BOARD_DIR: &str = "no board dir";
/// The problem line when a board file names another session than the bridge token.
pub const OTHER_SESSION: &str = "board file is for another session";
/// The problem line when the board file is missing. The coordinator may still be writing it.
pub const NO_BOARD_FILE: &str = "no board file yet";
/// The problem line when the file exists but cannot be read. The OS error text is not shown.
pub const BOARD_UNREADABLE: &str = "board file unreadable";
/// The problem line when the board file is larger than `MAX_BOARD_BYTES`.
pub const BOARD_TOO_LARGE: &str = "board file too large";

/// The pane that carries a valid bridge token, and what the token names.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BridgePane {
    pub pane_id: String,
    pub bridge: BridgeRef,
}

/// The bridge pane in `tree`. A pane in the focused workspace wins; otherwise the first in tree
/// order. A token that does not parse is ignored.
pub fn find_bridge(tree: &HerdrTree) -> Option<BridgePane> {
    let mut first = None;
    for ws in &tree.workspaces {
        for pane in ws.tabs.iter().flat_map(|tab| &tab.panes) {
            let Some(bridge) = pane
                .tokens
                .get(BRIDGE_TOKEN)
                .and_then(|value| parse_bridge_token(value))
            else {
                continue;
            };
            let found = BridgePane {
                pane_id: pane.id.clone(),
                bridge,
            };
            if ws.focused {
                return Some(found);
            }
            first.get_or_insert(found);
        }
    }
    first
}

/// The board directory: `$CONTR_BOARD_DIR` when set and not empty, else `$HOME/.local/state/herdr-board`.
/// `None` when neither is set. Read from the environment once, in `Board::init`.
pub fn board_dir() -> Option<PathBuf> {
    board_dir_from(std::env::var_os(BOARD_DIR_ENV), std::env::var_os("HOME"))
}

fn board_dir_from(override_dir: Option<OsString>, home: Option<OsString>) -> Option<PathBuf> {
    if let Some(dir) = non_empty(override_dir) {
        return Some(PathBuf::from(dir));
    }
    non_empty(home).map(|home| PathBuf::from(home).join(BOARD_DIR_UNDER_HOME))
}

fn non_empty(value: Option<OsString>) -> Option<OsString> {
    value.filter(|value| !value.is_empty())
}

/// Reads and parses one board file. Runs on the background executor. The size is capped before the
/// bytes are parsed, and the error is the panel's line, so only that reaches the UI.
fn read_board(path: &Path) -> Result<BoardSnapshot, String> {
    let bytes = read_capped(path, MAX_BOARD_BYTES)?;
    parse_board(&bytes).map_err(|error| error.line())
}

/// Reads at most `cap` bytes. A file longer than `cap` is an error, never a truncated read.
fn read_capped(path: &Path, cap: u64) -> Result<Vec<u8>, String> {
    let file = std::fs::File::open(path).map_err(|error| io_problem(&error))?;
    let mut bytes = Vec::new();
    file.take(cap.saturating_add(1))
        .read_to_end(&mut bytes)
        .map_err(|error| io_problem(&error))?;
    if bytes.len() as u64 > cap {
        return Err(BOARD_TOO_LARGE.to_owned());
    }
    Ok(bytes)
}

/// The panel's line for an I/O failure. The OS text is not shown.
fn io_problem(error: &std::io::Error) -> String {
    if error.kind() == std::io::ErrorKind::NotFound {
        NO_BOARD_FILE
    } else {
        BOARD_UNREADABLE
    }
    .to_owned()
}

/// The coordinator's board for the bridge pane in the herdr tree.
pub struct Board {
    /// The bridge pane. `None` when herdr is down or no pane carries a valid token.
    pub bridge: Option<BridgePane>,
    /// The last board read that belongs to the bridge's session.
    pub snapshot: Option<Arc<BoardSnapshot>>,
    /// Why the shown board is not the file as it is now: a line for the panel.
    pub problem: Option<String>,
    /// Where board files are read from. `None` when there is no board directory.
    dir: Option<PathBuf>,
    /// The read in flight. A newer read drops the older one.
    reading: Option<Task<()>>,
    _observe: Subscription,
    /// Board reads started. Tests count them.
    #[cfg(test)]
    pub reads: usize,
}

/// The app-wide handle to the board, created by `Board::init`.
pub struct GlobalBoard(pub Entity<Board>);

impl Global for GlobalBoard {}

impl Board {
    /// Registers the board as a global, reading from `board_dir()`. Panics without `Herdr::init`
    /// first.
    pub fn init(cx: &mut App) {
        let dir = board_dir();
        let board = cx.new(|cx| Self::new(dir, cx));
        cx.set_global(GlobalBoard(board));
    }

    /// Follows the herdr connection and syncs once with its current tree. `dir` is where board files
    /// are read from. Panics without `Herdr::init` first.
    pub fn new(dir: Option<PathBuf>, cx: &mut Context<Self>) -> Self {
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let tree = herdr.read(cx).live.tree.clone();
        let observe = cx.observe(&herdr, |board, herdr, cx| {
            let tree = herdr.read(cx).live.tree.clone();
            board.sync(tree.as_deref(), cx);
        });
        let mut board = Self {
            bridge: None,
            snapshot: None,
            problem: None,
            dir,
            reading: None,
            _observe: observe,
            #[cfg(test)]
            reads: 0,
        };
        board.sync(tree.as_deref(), cx);
        board
    }

    /// Follows the bridge in `tree`. A new session or revision starts a read; the same one does not.
    fn sync(&mut self, tree: Option<&HerdrTree>, cx: &mut Context<Self>) {
        let before = self.shown();
        match tree.and_then(find_bridge) {
            None => {
                self.bridge = None;
                self.reading = None;
                self.snapshot = None;
                self.problem = None;
            }
            Some(next) => {
                let same_board = self
                    .bridge
                    .as_ref()
                    .is_some_and(|old| old.bridge == next.bridge);
                if !same_board {
                    self.start_read(&next.bridge.session, cx);
                }
                self.bridge = Some(next);
            }
        }
        if self.shown() != before {
            cx.notify();
        }
    }

    /// Starts reading the board of `session` in the background, dropping any read in flight. The
    /// snapshot stays only while it belongs to `session`.
    fn start_read(&mut self, session: &str, cx: &mut Context<Self>) {
        self.keep_snapshot_of(session);
        self.problem = None;
        let Some(dir) = self.dir.clone() else {
            self.reading = None;
            self.problem = Some(NO_BOARD_DIR.to_owned());
            return;
        };
        #[cfg(test)]
        {
            self.reads += 1;
        }
        let path = board_path(&dir, session);
        let session = session.to_owned();
        self.reading = Some(cx.spawn(async move |this, cx| {
            let result = cx
                .background_executor()
                .spawn(async move { read_board(&path) })
                .await;
            let _ = this.update(cx, |board, cx| board.loaded(&session, result, cx));
        }));
    }

    /// Applies a finished read of `session`'s board. A board of another session, or a read that
    /// failed, sets the problem and keeps the snapshot only if it belongs to `session`.
    fn loaded(
        &mut self,
        session: &str,
        result: Result<BoardSnapshot, String>,
        cx: &mut Context<Self>,
    ) {
        let before = self.shown();
        match result {
            Ok(snapshot) if snapshot.session == session => {
                self.snapshot = Some(Arc::new(snapshot));
                self.problem = None;
            }
            Ok(_) => {
                self.keep_snapshot_of(session);
                self.problem = Some(OTHER_SESSION.to_owned());
            }
            Err(line) => {
                self.keep_snapshot_of(session);
                self.problem = Some(line);
            }
        }
        if self.shown() != before {
            cx.notify();
        }
    }

    /// Drops the snapshot unless it belongs to `session`.
    fn keep_snapshot_of(&mut self, session: &str) {
        let same = self
            .snapshot
            .as_ref()
            .is_some_and(|snapshot| snapshot.session == session);
        if !same {
            self.snapshot = None;
        }
    }

    /// The board directory that board files are read from, `None` when there is none.
    pub fn dir(&self) -> Option<&Path> {
        self.dir.as_deref()
    }

    /// What the views read. A change in any of these notifies.
    fn shown(
        &self,
    ) -> (
        Option<BridgePane>,
        Option<Arc<BoardSnapshot>>,
        Option<String>,
    ) {
        (
            self.bridge.clone(),
            self.snapshot.clone(),
            self.problem.clone(),
        )
    }

    /// Sets what the views read, as a change from herdr or a read would. For view tests.
    #[cfg(test)]
    pub(crate) fn set(
        &mut self,
        snapshot: Option<BoardSnapshot>,
        problem: Option<String>,
        bridge: Option<BridgePane>,
        cx: &mut Context<Self>,
    ) {
        self.snapshot = snapshot.map(Arc::new);
        self.problem = problem;
        self.bridge = bridge;
        cx.notify();
    }
}

/// Registers a board that reads no files, for tests that build a frame or a panel. Needs
/// `GlobalHerdr` first.
#[cfg(test)]
pub(crate) fn init_detached(cx: &mut App) {
    let board = cx.new(|cx| Board::new(None, cx));
    cx.set_global(GlobalBoard(board));
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board_model::BOARD_VERSION;
    use crate::herdr::Herdr;
    use crate::visibility::Visibility;
    use contr_herdr::{AgentState, HerdrUpdate, LiveHerdr, Pane, Tab, Workspace};
    use gpui::TestAppContext;
    use std::cell::Cell;
    use std::collections::BTreeMap;
    use std::rc::Rc;

    const SESSION: &str = "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f";
    const FIXTURE: &str = include_str!("board_fixture_v1.json");

    fn pane(id: &str, token: Option<&str>) -> Pane {
        Pane {
            id: id.to_owned(),
            label: None,
            cwd: None,
            focused: false,
            agent: None,
            tokens: token
                .map(|value| BTreeMap::from([(BRIDGE_TOKEN.to_owned(), value.to_owned())]))
                .unwrap_or_default(),
        }
    }

    fn workspace(id: &str, focused: bool, panes: Vec<Pane>) -> Workspace {
        Workspace {
            id: id.to_owned(),
            label: id.to_owned(),
            status: AgentState::Idle,
            focused,
            tabs: vec![Tab {
                id: format!("{id}:t1"),
                label: "t1".to_owned(),
                status: AgentState::Idle,
                focused: false,
                panes,
            }],
        }
    }

    fn tree(workspaces: Vec<Workspace>) -> HerdrTree {
        HerdrTree {
            version: "0.9.3".to_owned(),
            focused_pane_id: None,
            workspaces,
        }
    }

    /// One focused workspace with one pane `w1:p1` that carries `token`.
    fn coordinator(token: &str) -> HerdrTree {
        tree(vec![workspace(
            "w1",
            true,
            vec![pane("w1:p1", Some(token))],
        )])
    }

    fn token(rev: u64) -> String {
        format!("{BOARD_VERSION} {SESSION} {rev}")
    }

    /// A board on a detached herdr connection that is shown, reading from `dir`. Returns the herdr
    /// entity, to push trees into, and the board.
    fn setup(cx: &mut TestAppContext, dir: Option<PathBuf>) -> (Entity<Herdr>, Entity<Board>) {
        cx.update(|app| {
            let herdr = app.new(|cx| Herdr::detached(LiveHerdr::default(), cx));
            app.set_global(GlobalHerdr(herdr.clone()));
            let board = app.new(|cx| Board::new(dir, cx));
            (herdr, board)
        })
    }

    /// A board on a detached herdr connection gated by `visibility`, which the caller keeps alive.
    fn setup_on(
        cx: &mut TestAppContext,
        dir: Option<PathBuf>,
        visibility: &Entity<Visibility>,
    ) -> (Entity<Herdr>, Entity<Board>) {
        cx.update(|app| {
            let herdr = app.new(|cx| Herdr::detached_on(LiveHerdr::default(), visibility, cx));
            app.set_global(GlobalHerdr(herdr.clone()));
            let board = app.new(|cx| Board::new(dir, cx));
            (herdr, board)
        })
    }

    /// Pushes `tree` into herdr and lets the background reads finish.
    fn push(cx: &mut TestAppContext, herdr: &Entity<Herdr>, tree: HerdrTree) {
        cx.update(|app| {
            herdr.update(app, |herdr, cx| {
                herdr.apply(HerdrUpdate::Tree(Arc::new(tree)), cx)
            })
        });
        cx.run_until_parked();
    }

    fn read<T>(cx: &TestAppContext, board: &Entity<Board>, f: impl FnOnce(&Board) -> T) -> T {
        cx.read(|app| f(board.read(app)))
    }

    /// Counts the notifications `board` sends. Keep the returned subscription alive.
    fn count_notifications(
        cx: &mut TestAppContext,
        board: &Entity<Board>,
    ) -> (Rc<Cell<u32>>, Subscription) {
        let count = Rc::new(Cell::new(0));
        let seen = count.clone();
        let sub = cx.update(|app| app.observe(board, move |_, _| seen.set(seen.get() + 1)));
        (count, sub)
    }

    /// A board directory whose `<SESSION>.json` holds `text`.
    fn board_dir_with(text: &[u8]) -> tempfile::TempDir {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(board_path(dir.path(), SESSION), text).unwrap();
        dir
    }

    #[test]
    fn a_tree_without_the_token_has_no_bridge() {
        assert_eq!(
            find_bridge(&tree(vec![workspace(
                "w1",
                true,
                vec![pane("w1:p1", None)]
            )])),
            None
        );
    }

    #[test]
    fn a_bad_token_is_ignored() {
        let bad = tree(vec![workspace(
            "w1",
            true,
            vec![
                pane("w1:p1", Some("2 s1 1")),
                pane("w1:p2", Some("1 s1")),
                pane("w1:p3", Some("1 s1 1")),
            ],
        )]);
        let found = find_bridge(&bad).unwrap();
        assert_eq!(found.pane_id, "w1:p3");
        assert_eq!(found.bridge.session, "s1");
    }

    #[test]
    fn the_pane_in_the_focused_workspace_wins() {
        let two = tree(vec![
            workspace("w1", false, vec![pane("w1:p1", Some("1 s1 1"))]),
            workspace("w2", true, vec![pane("w2:p1", Some("1 s2 5"))]),
        ]);
        let found = find_bridge(&two).unwrap();
        assert_eq!(found.pane_id, "w2:p1");
        assert_eq!(
            found.bridge,
            BridgeRef {
                session: "s2".into(),
                rev: 5
            }
        );
    }

    #[test]
    fn with_no_focused_workspace_the_first_in_tree_order_wins() {
        let two = tree(vec![
            workspace("w1", false, vec![pane("w1:p1", Some("1 s1 1"))]),
            workspace("w2", false, vec![pane("w2:p1", Some("1 s2 5"))]),
        ]);
        assert_eq!(find_bridge(&two).unwrap().pane_id, "w1:p1");
    }

    #[test]
    fn board_dir_prefers_the_override_then_home() {
        assert_eq!(
            board_dir_from(Some("/x".into()), Some("/h".into())),
            Some(PathBuf::from("/x"))
        );
        assert_eq!(
            board_dir_from(Some("".into()), Some("/h".into())),
            Some(home_board_dir("/h"))
        );
        assert_eq!(
            board_dir_from(None, Some("home".into())),
            Some(home_board_dir("home"))
        );
        assert_eq!(board_dir_from(None, Some("".into())), None);
        assert_eq!(board_dir_from(None, None), None);
    }

    fn home_board_dir(home: &str) -> PathBuf {
        Path::new(home).join(".local/state/herdr-board")
    }

    #[test]
    fn read_capped_refuses_a_file_over_the_cap() {
        let dir = board_dir_with(b"0123456789");
        let path = board_path(dir.path(), SESSION);
        assert_eq!(read_capped(&path, 9).unwrap_err(), BOARD_TOO_LARGE);
        assert_eq!(read_capped(&path, 10).unwrap(), b"0123456789");
    }

    #[test]
    fn read_capped_reports_a_missing_file_calmly() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(
            read_capped(&board_path(dir.path(), SESSION), 10).unwrap_err(),
            NO_BOARD_FILE
        );
    }

    #[test]
    fn read_capped_reports_an_unreadable_file_without_the_os_text() {
        // A directory opens, but reading it fails: an error that is not "not found".
        let dir = tempfile::tempdir().unwrap();
        let path = board_path(dir.path(), SESSION);
        std::fs::create_dir(&path).unwrap();
        assert_eq!(read_capped(&path, 10).unwrap_err(), BOARD_UNREADABLE);
    }

    #[gpui::test]
    fn a_token_reads_the_board_once_and_shows_its_rows(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));

        let (rows, problem, reads, pane_id) = read(cx, &board, |b| {
            (
                b.snapshot.as_ref().map(|s| s.rows.len()),
                b.problem.clone(),
                b.reads,
                b.bridge.as_ref().map(|p| p.pane_id.clone()),
            )
        });
        assert_eq!(rows, Some(3));
        assert_eq!(problem, None);
        assert_eq!(reads, 1);
        assert_eq!(pane_id.as_deref(), Some("w1:p1"));
    }

    #[gpui::test]
    fn dir_is_the_directory_the_board_was_given(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        let shown = read(cx, &board, |b| b.dir().map(Path::to_path_buf));
        assert_eq!(shown, Some(dir.path().to_path_buf()));
    }

    #[gpui::test]
    fn dir_is_none_without_a_board_directory(cx: &mut TestAppContext) {
        let (_herdr, board) = setup(cx, None);
        assert!(read(cx, &board, |b| b.dir().is_none()));
    }

    #[gpui::test]
    fn nothing_is_read_while_the_window_is_hidden(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let visibility = cx.update(|app| app.new(|_| Visibility { visible: false }));
        let (herdr, board) = setup_on(cx, Some(dir.path().to_path_buf()), &visibility);
        push(cx, &herdr, coordinator(&token(42)));
        assert_eq!(
            read(cx, &board, |b| b.reads),
            0,
            "hidden changes are not read"
        );

        cx.update(|app| visibility.update(app, |v, cx| v.set(true, cx)));
        cx.run_until_parked();
        let (reads, rows) = read(cx, &board, |b| {
            (b.reads, b.snapshot.as_ref().map(|s| s.rows.len()))
        });
        assert_eq!(reads, 1, "showing the window reads the board once");
        assert_eq!(rows, Some(3));
    }

    #[gpui::test]
    fn a_new_revision_reads_again_and_the_same_one_does_not(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));
        assert_eq!(read(cx, &board, |b| b.reads), 1);

        push(cx, &herdr, coordinator(&token(42))); // identical tree: herdr does not notify
        assert_eq!(read(cx, &board, |b| b.reads), 1);

        let with_other_pane = tree(vec![workspace(
            "w1",
            true,
            vec![pane("w1:p1", Some(&token(42))), pane("w1:p9", None)],
        )]);
        push(cx, &herdr, with_other_pane); // herdr notifies, the bridge is the same
        assert_eq!(read(cx, &board, |b| b.reads), 1);

        push(cx, &herdr, coordinator(&token(43)));
        assert_eq!(read(cx, &board, |b| b.reads), 2);
    }

    #[gpui::test]
    fn a_bridge_pane_that_moves_with_the_same_session_and_rev_does_not_read(
        cx: &mut TestAppContext,
    ) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));

        let moved = tree(vec![workspace(
            "w1",
            true,
            vec![pane("w1:p2", Some(&token(42)))],
        )]);
        push(cx, &herdr, moved);
        let (reads, pane_id) = read(cx, &board, |b| {
            (b.reads, b.bridge.as_ref().map(|p| p.pane_id.clone()))
        });
        assert_eq!(reads, 1);
        assert_eq!(pane_id.as_deref(), Some("w1:p2"));
    }

    #[gpui::test]
    fn removing_the_token_clears_the_board(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));
        assert!(read(cx, &board, |b| b.snapshot.is_some()));

        push(
            cx,
            &herdr,
            tree(vec![workspace("w1", true, vec![pane("w1:p1", None)])]),
        );
        let (bridge, snapshot, problem) = read(cx, &board, |b| {
            (b.bridge.clone(), b.snapshot.clone(), b.problem.clone())
        });
        assert_eq!(bridge, None);
        assert_eq!(snapshot, None);
        assert_eq!(problem, None);
    }

    #[gpui::test]
    fn a_board_of_another_session_is_a_problem_and_not_shown(cx: &mut TestAppContext) {
        let dir = board_dir_with(br#"{"version": 1, "session": "other", "rev": 1}"#);
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(1)));

        let (snapshot, problem) = read(cx, &board, |b| (b.snapshot.clone(), b.problem.clone()));
        assert_eq!(snapshot, None);
        assert_eq!(problem.as_deref(), Some(OTHER_SESSION));
    }

    #[gpui::test]
    fn a_v2_file_keeps_the_last_good_board_and_shows_the_version_line(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));

        std::fs::write(
            board_path(dir.path(), SESSION),
            format!(r#"{{"version": 2, "session": "{SESSION}", "rev": 43}}"#),
        )
        .unwrap();
        push(cx, &herdr, coordinator(&token(43)));

        let (rows, problem) = read(cx, &board, |b| {
            (b.snapshot.as_ref().map(|s| s.rows.len()), b.problem.clone())
        });
        assert_eq!(rows, Some(3));
        assert_eq!(problem.as_deref(), Some("board v2 · this contr reads v1"));
    }

    #[gpui::test]
    fn a_new_session_drops_the_old_board_when_its_own_read_fails(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));
        assert!(read(cx, &board, |b| b.snapshot.is_some()));

        // The other session has no board file, so its read fails and the old board must go.
        push(cx, &herdr, coordinator("1 other-session 1"));
        let (snapshot, problem) = read(cx, &board, |b| (b.snapshot.clone(), b.problem.clone()));
        assert_eq!(snapshot, None);
        assert!(problem.is_some());
    }

    #[gpui::test]
    fn a_missing_file_is_a_problem_read_once_and_not_retried(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(1)));

        let (reads, snapshot, problem) = read(cx, &board, |b| {
            (b.reads, b.snapshot.clone(), b.problem.clone())
        });
        assert_eq!(reads, 1);
        assert_eq!(snapshot, None);
        assert_eq!(problem.as_deref(), Some(NO_BOARD_FILE));
    }

    #[gpui::test]
    fn an_unreadable_file_is_a_calm_problem(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir(board_path(dir.path(), SESSION)).unwrap();
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(1)));

        let (snapshot, problem) = read(cx, &board, |b| (b.snapshot.clone(), b.problem.clone()));
        assert_eq!(snapshot, None);
        assert_eq!(problem.as_deref(), Some(BOARD_UNREADABLE));
    }

    #[gpui::test]
    fn a_truncated_file_is_a_problem(cx: &mut TestAppContext) {
        let dir = board_dir_with(&FIXTURE.as_bytes()[..FIXTURE.len() / 2]);
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));

        let (snapshot, problem) = read(cx, &board, |b| (b.snapshot.clone(), b.problem.clone()));
        assert_eq!(snapshot, None);
        assert!(problem.unwrap().starts_with("board unreadable: "));
    }

    #[gpui::test]
    fn no_board_dir_is_a_problem_and_nothing_is_read(cx: &mut TestAppContext) {
        let (herdr, board) = setup(cx, None);
        push(cx, &herdr, coordinator(&token(42)));

        let (reads, bridged, problem) = read(cx, &board, |b| {
            (b.reads, b.bridge.is_some(), b.problem.clone())
        });
        assert_eq!(reads, 0);
        assert!(bridged);
        assert_eq!(problem.as_deref(), Some(NO_BOARD_DIR));
    }

    #[gpui::test]
    fn an_unchanged_bridge_does_not_notify_the_board(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        push(cx, &herdr, coordinator(&token(42)));
        let (count, _sub) = count_notifications(cx, &board);

        // An unrelated pane changes the tree, so herdr notifies, but the bridge and the board do not.
        push(
            cx,
            &herdr,
            tree(vec![workspace(
                "w1",
                true,
                vec![pane("w1:p1", Some(&token(42))), pane("w1:p9", None)],
            )]),
        );
        assert_eq!(count.get(), 0);
    }

    #[gpui::test]
    fn the_first_token_notifies_for_the_bridge_and_again_for_the_read(cx: &mut TestAppContext) {
        let dir = board_dir_with(FIXTURE.as_bytes());
        let (herdr, board) = setup(cx, Some(dir.path().to_path_buf()));
        let (count, _sub) = count_notifications(cx, &board);
        push(cx, &herdr, coordinator(&token(42)));
        assert_eq!(
            count.get(),
            2,
            "one notify for the bridge, one for the snapshot"
        );
    }
}
