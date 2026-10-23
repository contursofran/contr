//! The queue of actions that contr sends to the coordinator mod, as the UI holds it. One `Outbox` serves
//! the session of the board. Its file is written on the background executor, through a `.tmp` file and a
//! rename, and writes run one after another. A held answer, done or dismiss is released by an executor
//! timer, and only while such an action is held. Nothing here is polled.

use crate::board::{Board, GlobalBoard};
use crate::board_actions::{Action, ActionsFile, Outbox, actions_path, parse_actions};
use crate::board_model::{ActionFailure, BoardSnapshot};
use crate::status_message::GlobalStatusMessage;
use gpui::{App, AppContext, Context, Entity, Global, Subscription, Task};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// The longest status line for a failed write. The line is cut to this many characters.
const MAX_ERROR_CHARS: usize = 60;

/// The actions that contr has queued for the board's session.
pub struct BoardOutbox {
    /// The queue. `None` until a board of a session that takes actions is read, and after a board of
    /// another session replaces it.
    outbox: Option<Outbox>,
    /// `true` while the latest board shows the session of `outbox` and that session takes actions.
    live: bool,
    /// The board directory, where the actions file goes. `None` when there is none.
    dir: Option<PathBuf>,
    /// The last write. A new write waits for it, so writes never overlap.
    writing: Option<Task<()>>,
    /// Wakes when the earliest held action is due. `None` while nothing is held.
    timer: Option<Task<()>>,
    /// The read of the existing actions file, and the session it is for. It ends in `adopt`.
    loading: Option<(String, Task<()>)>,
    _observe: Subscription,
    /// Writes started. Tests count them.
    #[cfg(test)]
    pub writes: usize,
    /// Reads of an existing actions file started. Tests count them.
    #[cfg(test)]
    pub reads: usize,
}

/// The app-wide handle to the queue, created by `BoardOutbox::init`.
pub struct GlobalBoardOutbox(pub Entity<BoardOutbox>);

impl Global for GlobalBoardOutbox {}

impl BoardOutbox {
    /// Registers the queue as a global and writes the held actions when the app quits. Panics without
    /// `Board::init` first.
    pub fn init(cx: &mut App) {
        let board = cx.global::<GlobalBoard>().0.clone();
        let outbox = cx.new(|cx| Self::new(&board, cx));
        let on_quit = outbox.clone();
        cx.on_app_quit(move |cx| {
            let pending = on_quit.update(cx, |outbox, cx| outbox.release_for_quit(cx));
            async move {
                if let Some(pending) = pending {
                    pending.await;
                }
            }
        })
        .detach();
        cx.set_global(GlobalBoardOutbox(outbox));
    }

    fn new(board: &Entity<Board>, cx: &mut Context<Self>) -> Self {
        let dir = board.read(cx).dir().map(Path::to_path_buf);
        let observe = cx.observe(board, |this, board, cx| this.observe_board(&board, cx));
        let mut this = Self {
            outbox: None,
            live: false,
            dir,
            writing: None,
            timer: None,
            loading: None,
            _observe: observe,
            #[cfg(test)]
            writes: 0,
            #[cfg(test)]
            reads: 0,
        };
        this.observe_board(board, cx);
        this
    }

    /// `true` when an action can be sent: the latest board shows the queued session and that session
    /// takes actions.
    pub fn can_act(&self) -> bool {
        self.live && self.outbox.is_some()
    }

    /// Queues `action`. Returns `false`, and queues nothing, when the board takes no actions. An
    /// answer, done or dismiss waits `UNDO_MS` before it is written. Any other action is written now.
    pub fn send(&mut self, action: Action, cx: &mut Context<Self>) -> bool {
        if !self.can_act() {
            return false;
        }
        let now = now_ms();
        let written = self
            .outbox
            .as_mut()
            .is_some_and(|outbox| outbox.push(action, now).is_some());
        if written {
            self.save(cx);
        }
        self.arm_timer(now, cx);
        cx.notify();
        true
    }

    /// Removes the held answer, done or dismiss for `key`. `false` when none is held: the action was
    /// written already, or it never existed.
    pub fn undo(&mut self, key: &str, cx: &mut Context<Self>) -> bool {
        let removed = self.outbox.as_mut().is_some_and(|outbox| outbox.undo(key));
        if removed {
            self.arm_timer(now_ms(), cx);
            cx.notify();
        }
        removed
    }

    /// The answer text for `key` and `true` when it can still be undone. See `Outbox::pending_answer`.
    pub fn pending_answer(&self, key: &str) -> Option<(String, bool)> {
        self.outbox.as_ref()?.pending_answer(key)
    }

    /// Follows `board`. A board of the queued session applies its report. A board of another session
    /// replaces the queue, after the held actions of the old one are written.
    fn observe_board(&mut self, board: &Entity<Board>, cx: &mut Context<Self>) {
        let Some(snapshot) = board.read(cx).snapshot.clone() else {
            self.live = false;
            self.loading = None;
            cx.notify();
            return;
        };
        let takes = snapshot.actions >= 1 && self.dir.is_some();
        if self
            .outbox
            .as_ref()
            .is_some_and(|outbox| outbox.session() == snapshot.session)
        {
            self.live = takes;
            if takes {
                self.apply(&snapshot, cx);
            }
        } else {
            self.live = false;
            self.drop_outbox(cx);
            if !takes {
                self.loading = None;
            } else if !self.reading(&snapshot.session) {
                self.load(snapshot, cx);
            }
        }
        cx.notify();
    }

    /// `true` while the read of `session`'s actions file is in flight. The read is not started again.
    fn reading(&self, session: &str) -> bool {
        self.outbox.is_none()
            && self
                .loading
                .as_ref()
                .is_some_and(|(loading, _)| loading == session)
    }

    /// Ends the queue of another session. Its held actions are written first, so none is lost.
    fn drop_outbox(&mut self, cx: &mut Context<Self>) {
        if self.outbox.as_mut().is_some_and(Outbox::release_all) {
            self.save(cx);
        }
        self.outbox = None;
        self.timer = None;
    }

    /// Reads the existing actions file of the snapshot's session in the background, then adopts it.
    fn load(&mut self, snapshot: Arc<BoardSnapshot>, cx: &mut Context<Self>) {
        let Some(dir) = self.dir.clone() else {
            return;
        };
        #[cfg(test)]
        {
            self.reads += 1;
        }
        let path = actions_path(&dir, &snapshot.session);
        let session = snapshot.session.clone();
        let read = cx.spawn(async move |this, cx| {
            let existing = cx
                .background_executor()
                .spawn(async move { read_existing(&path) })
                .await;
            let _ = this.update(cx, |this, cx| this.adopt(snapshot, existing, cx));
        });
        self.loading = Some((session, read));
    }

    /// Creates the queue from the file that was read, then applies the snapshot's report.
    fn adopt(
        &mut self,
        snapshot: Arc<BoardSnapshot>,
        existing: Option<ActionsFile>,
        cx: &mut Context<Self>,
    ) {
        // The read is finished, so its handle goes.
        self.loading = None;
        self.outbox = Some(Outbox::new(
            snapshot.session.clone(),
            snapshot.acted,
            existing,
        ));
        self.live = true;
        self.apply(&snapshot, cx);
        cx.notify();
    }

    /// Applies the mod's report to the queue: the actions it took are dropped, and each new failure is
    /// shown once.
    fn apply(&mut self, snapshot: &BoardSnapshot, cx: &mut Context<Self>) {
        let Some(outbox) = self.outbox.as_mut() else {
            return;
        };
        let acked = outbox.ack(snapshot.acted);
        let lines: Vec<String> = outbox
            .new_failures(&snapshot.failed)
            .into_iter()
            .map(failure_line)
            .collect();
        if acked {
            self.save(cx);
        }
        if !lines.is_empty() {
            show_status(lines.join("; "), cx);
        }
    }

    /// Writes the whole queue in the background. It starts after the write before it.
    fn save(&mut self, cx: &mut Context<Self>) {
        let (Some(outbox), Some(dir)) = (self.outbox.as_ref(), self.dir.as_deref()) else {
            return;
        };
        let path = actions_path(dir, outbox.session());
        let file = outbox.file();
        #[cfg(test)]
        {
            self.writes += 1;
        }
        let after = self.writing.take();
        self.writing = Some(cx.spawn(async move |this, cx| {
            if let Some(after) = after {
                after.await;
            }
            let result = cx
                .background_executor()
                .spawn(async move { write_file(&path, &file) })
                .await;
            if let Err(error) = result {
                let _ = this.update(cx, |_, cx| show_status(save_error_line(&error), cx));
            }
        }));
    }

    /// Sets the timer for the earliest held action, counted from `now`. No timer while nothing is held.
    fn arm_timer(&mut self, now: u64, cx: &mut Context<Self>) {
        let due = self.outbox.as_ref().and_then(Outbox::next_release);
        self.timer = due.map(|due| {
            // The due time is wall-clock, so the wait is the wall-clock gap. The executor timer counts
            // that gap from now. A jump of the wall clock changes the wait of this one action only.
            let wait = Duration::from_millis(due.saturating_sub(now));
            cx.spawn(async move |this, cx| {
                cx.background_executor().timer(wait).await;
                let _ = this.update(cx, |this, cx| this.release_due(due, cx));
            })
        });
    }

    /// Writes the held actions that are due at `due`, then waits for the next one.
    fn release_due(&mut self, due: u64, cx: &mut Context<Self>) {
        let released = self
            .outbox
            .as_mut()
            .is_some_and(|outbox| outbox.release(due));
        if released {
            self.save(cx);
            cx.notify();
        }
        self.arm_timer(due, cx);
    }

    /// Writes every held action at once, for quit. Returns the last write, which quit waits for.
    fn release_for_quit(&mut self, cx: &mut Context<Self>) -> Option<Task<()>> {
        if self.outbox.as_mut().is_some_and(Outbox::release_all) {
            self.save(cx);
        }
        self.timer = None;
        self.writing.take()
    }
}

/// The current wall-clock time in milliseconds since the Unix epoch. `0` before the epoch.
pub(crate) fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |elapsed| {
            u64::try_from(elapsed.as_millis()).unwrap_or(u64::MAX)
        })
}

/// The status line for one failed action: `"{key} not sent: {text}"`, or `"not sent: {text}"` without a key.
fn failure_line(failure: &ActionFailure) -> String {
    match failure.key.as_deref().filter(|key| !key.is_empty()) {
        Some(key) => format!("{key} not sent: {}", failure.text),
        None => format!("not sent: {}", failure.text),
    }
}

/// The status line for a failed write, cut to `MAX_ERROR_CHARS` characters.
fn save_error_line(error: &std::io::Error) -> String {
    format!("actions not saved: {error}")
        .chars()
        .take(MAX_ERROR_CHARS)
        .collect()
}

/// Shows `text` in the status bar. Does nothing when the status message is not registered.
fn show_status(text: String, cx: &mut App) {
    if let Some(message) = cx
        .try_global::<GlobalStatusMessage>()
        .map(|global| global.0.clone())
    {
        message.update(cx, |message, cx| message.show(text, cx));
    }
}

/// Reads the actions file at `path`. Runs on the background executor. A missing, unreadable or
/// unknown file is `None`, and the queue starts empty.
fn read_existing(path: &Path) -> Option<ActionsFile> {
    parse_actions(&std::fs::read(path).ok()?)
}

/// Writes `file` to `path` through `<path>.tmp` and a rename, creating the directory when it is missing.
/// Runs on the background executor, and serializes there too.
fn write_file(path: &Path, file: &ActionsFile) -> std::io::Result<()> {
    let bytes = serde_json::to_vec(file).map_err(std::io::Error::other)?;
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    let mut tmp = path.as_os_str().to_owned();
    tmp.push(".tmp");
    let tmp = PathBuf::from(tmp);
    let result = std::fs::write(&tmp, &bytes).and_then(|()| std::fs::rename(&tmp, path));
    if result.is_err() {
        // A failed write or rename leaves no `.tmp` behind. The next write would replace it anyway.
        let _ = std::fs::remove_file(&tmp);
    }
    result
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board_actions::UNDO_MS;
    use crate::board_model::parse_board;
    use crate::herdr::{GlobalHerdr, Herdr};
    use crate::status_message::{MESSAGE_TTL, StatusMessage};
    use contr_herdr::LiveHerdr;
    use gpui::TestAppContext;

    const SESSION: &str = "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f";
    const OTHER: &str = "other-session";

    /// A board of `session` that takes `actions`, with `acted` taken and no failures.
    fn snapshot(session: &str, actions: u32, acted: u64) -> BoardSnapshot {
        snapshot_with(session, actions, acted, "")
    }

    /// A board of `session` with `failed` as its JSON list entries.
    fn snapshot_with(session: &str, actions: u32, acted: u64, failed: &str) -> BoardSnapshot {
        let json = format!(
            r#"{{"version":1,"session":"{session}","rev":1,"actions":{actions},"acted":{acted},"failed":[{failed}]}}"#
        );
        parse_board(json.as_bytes()).unwrap()
    }

    /// Registers herdr, the status line, a board that reads from `dir` and the queue. Returns the
    /// board, to show boards in, and the queue.
    fn setup(
        cx: &mut TestAppContext,
        dir: Option<PathBuf>,
    ) -> (Entity<Board>, Entity<BoardOutbox>) {
        cx.update(|app| {
            let herdr = app.new(|cx| Herdr::detached(LiveHerdr::default(), cx));
            app.set_global(GlobalHerdr(herdr));
            StatusMessage::init(app);
            let board = app.new(|cx| Board::new(dir, cx));
            app.set_global(GlobalBoard(board.clone()));
            BoardOutbox::init(app);
            let outbox = app.global::<GlobalBoardOutbox>().0.clone();
            (board, outbox)
        })
    }

    /// Sets what the board shows, as a read would, and lets the queue react.
    fn show(cx: &mut TestAppContext, board: &Entity<Board>, snapshot: Option<BoardSnapshot>) {
        cx.update(|app| board.update(app, |b, cx| b.set(snapshot, None, None, cx)));
        cx.run_until_parked();
    }

    /// Runs what is due, then moves the test clock by `by` and runs what that makes due.
    fn advance(cx: &mut TestAppContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    fn send(cx: &mut TestAppContext, outbox: &Entity<BoardOutbox>, action: Action) -> bool {
        cx.update(|app| outbox.update(app, |o, cx| o.send(action, cx)))
    }

    fn undo(cx: &mut TestAppContext, outbox: &Entity<BoardOutbox>, key: &str) -> bool {
        cx.update(|app| outbox.update(app, |o, cx| o.undo(key, cx)))
    }

    fn read<T>(
        cx: &TestAppContext,
        outbox: &Entity<BoardOutbox>,
        f: impl FnOnce(&BoardOutbox) -> T,
    ) -> T {
        cx.read(|app| f(outbox.read(app)))
    }

    fn writes(cx: &TestAppContext, outbox: &Entity<BoardOutbox>) -> usize {
        read(cx, outbox, |o| o.writes)
    }

    fn reads(cx: &TestAppContext, outbox: &Entity<BoardOutbox>) -> usize {
        read(cx, outbox, |o| o.reads)
    }

    fn timer_is_set(cx: &TestAppContext, outbox: &Entity<BoardOutbox>) -> bool {
        read(cx, outbox, |o| o.timer.is_some())
    }

    fn session_of(cx: &TestAppContext, outbox: &Entity<BoardOutbox>) -> Option<String> {
        read(cx, outbox, |o| {
            o.outbox.as_ref().map(|queue| queue.session().to_owned())
        })
    }

    fn pending(
        cx: &TestAppContext,
        outbox: &Entity<BoardOutbox>,
        key: &str,
    ) -> Option<(String, bool)> {
        read(cx, outbox, |o| o.pending_answer(key))
    }

    fn status(cx: &TestAppContext) -> Option<String> {
        cx.read(|app| {
            app.global::<GlobalStatusMessage>()
                .0
                .read(app)
                .text
                .as_ref()
                .map(|text| text.to_string())
        })
    }

    /// The actions file of `session` in `dir`, if one was written.
    fn file_of(dir: &Path, session: &str) -> Option<ActionsFile> {
        parse_actions(&std::fs::read(actions_path(dir, session)).ok()?)
    }

    /// The `seq` values in the file of `session`, in file order. Empty when there is no file.
    fn seqs_of(dir: &Path, session: &str) -> Vec<u64> {
        file_of(dir, session)
            .map(|file| file.actions.iter().map(|env| env.seq).collect())
            .unwrap_or_default()
    }

    fn answer(key: &str) -> Action {
        Action::Answer {
            key: key.to_owned(),
            option: 1,
            label: "two lines".to_owned(),
        }
    }

    fn wrap_up(on: bool) -> Action {
        Action::WrapUp { on }
    }

    #[gpui::test]
    fn a_board_that_takes_no_actions_sends_nothing_and_writes_nothing(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 0, 0)));

        assert!(!read(cx, &outbox, |o| o.can_act()));
        assert!(!send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();
        assert_eq!(writes(cx, &outbox), 0);
        assert!(file_of(dir.path(), SESSION).is_none());
    }

    #[gpui::test]
    fn a_board_without_a_directory_takes_no_actions(cx: &mut TestAppContext) {
        let (board, outbox) = setup(cx, None);
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));

        assert!(!read(cx, &outbox, |o| o.can_act()));
        assert!(!send(cx, &outbox, wrap_up(true)));
    }

    #[gpui::test]
    fn a_missing_or_inert_board_sends_nothing_but_a_held_answer_is_still_written(
        cx: &mut TestAppContext,
    ) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, answer("D7")));

        show(cx, &board, None);
        assert!(!send(cx, &outbox, wrap_up(true)));
        show(cx, &board, Some(snapshot(SESSION, 0, 0)));
        assert!(!read(cx, &outbox, |o| o.can_act()));

        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1]);
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(read(cx, &outbox, |o| o.can_act()));
    }

    #[gpui::test]
    fn wrap_up_is_written_at_once_with_the_next_seq(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 4)));

        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();
        let file = file_of(dir.path(), SESSION).unwrap();
        assert_eq!(file.actions.len(), 1);
        assert_eq!(file.actions[0].seq, 5);
        assert_eq!(file.actions[0].action, wrap_up(true));
        assert_eq!(writes(cx, &outbox), 1);
        assert!(!timer_is_set(cx, &outbox), "nothing is held, so no timer");
    }

    #[gpui::test]
    fn an_answer_is_written_only_after_the_undo_window(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));

        assert!(send(cx, &outbox, answer("D7")));
        advance(cx, Duration::from_millis(UNDO_MS - 1));
        assert!(file_of(dir.path(), SESSION).is_none());
        assert_eq!(writes(cx, &outbox), 0);
        assert!(timer_is_set(cx, &outbox));

        advance(cx, Duration::from_millis(1));
        let file = file_of(dir.path(), SESSION).unwrap();
        assert_eq!(file.actions[0].seq, 1);
        assert_eq!(file.actions[0].action, answer("D7"));
        assert_eq!(writes(cx, &outbox), 1);
        assert!(!timer_is_set(cx, &outbox));
    }

    #[gpui::test]
    fn undo_before_the_release_keeps_the_answer_out_of_the_file(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, answer("D7")));
        assert_eq!(
            pending(cx, &outbox, "D7"),
            Some(("two lines".to_owned(), true))
        );

        assert!(undo(cx, &outbox, "D7"));
        assert!(
            !undo(cx, &outbox, "D7"),
            "a second undo has nothing to remove"
        );
        assert!(
            !undo(cx, &outbox, "D9"),
            "an unknown key has nothing to remove"
        );
        advance(cx, Duration::from_millis(UNDO_MS));
        assert!(file_of(dir.path(), SESSION).is_none());
        assert_eq!(writes(cx, &outbox), 0);
        assert_eq!(pending(cx, &outbox, "D7"), None);
    }

    #[gpui::test]
    fn a_written_answer_can_no_longer_be_undone(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, answer("D7")));
        advance(cx, Duration::from_millis(UNDO_MS));

        assert_eq!(
            pending(cx, &outbox, "D7"),
            Some(("two lines".to_owned(), false))
        );
        assert!(!undo(cx, &outbox, "D7"));
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1]);
    }

    #[gpui::test]
    fn two_quick_sends_leave_the_newer_file_on_disk(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));

        assert!(send(cx, &outbox, wrap_up(true)));
        assert!(send(cx, &outbox, wrap_up(false)));
        cx.run_until_parked();
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1, 2]);
        assert_eq!(writes(cx, &outbox), 2);
    }

    #[gpui::test]
    fn a_new_acted_rewrites_the_file_without_the_taken_actions(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1]);

        show(cx, &board, Some(snapshot(SESSION, 1, 1)));
        assert_eq!(seqs_of(dir.path(), SESSION), Vec::<u64>::new());
        assert_eq!(writes(cx, &outbox), 2);
    }

    #[gpui::test]
    fn a_report_that_changes_nothing_writes_nothing(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();

        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert_eq!(writes(cx, &outbox), 1);
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1]);
    }

    #[gpui::test]
    fn a_new_failure_is_shown_and_its_answer_is_dropped(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, answer("D7")));
        advance(cx, Duration::from_millis(UNDO_MS));

        let failed = r#"{"seq":1,"key":"D7","text":"D7 is not open"}"#;
        show(cx, &board, Some(snapshot_with(SESSION, 1, 1, failed)));
        assert_eq!(status(cx).as_deref(), Some("D7 not sent: D7 is not open"));
        assert_eq!(pending(cx, &outbox, "D7"), None);
        assert_eq!(seqs_of(dir.path(), SESSION), Vec::<u64>::new());
    }

    #[gpui::test]
    fn a_failure_without_a_key_is_shown_without_one(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, Action::ClearRows));
        cx.run_until_parked();

        let failed = r#"{"seq":1,"text":"nothing to clear"}"#;
        show(cx, &board, Some(snapshot_with(SESSION, 1, 1, failed)));
        assert_eq!(status(cx).as_deref(), Some("not sent: nothing to clear"));
    }

    #[gpui::test]
    fn a_failure_is_shown_once(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();

        let failed = r#"{"seq":1,"key":"D7","text":"D7 is not open"}"#;
        show(cx, &board, Some(snapshot_with(SESSION, 1, 1, failed)));
        assert!(status(cx).is_some());
        advance(cx, MESSAGE_TTL);
        assert_eq!(status(cx), None);

        show(cx, &board, Some(snapshot_with(SESSION, 1, 1, failed)));
        assert_eq!(status(cx), None, "the same failure is not shown again");
    }

    #[gpui::test]
    fn a_session_change_starts_a_new_queue_and_leaves_the_old_file(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();
        let before = std::fs::read(actions_path(dir.path(), SESSION)).unwrap();

        show(cx, &board, Some(snapshot(OTHER, 1, 0)));
        assert_eq!(session_of(cx, &outbox).as_deref(), Some(OTHER));
        assert!(send(cx, &outbox, wrap_up(false)));
        cx.run_until_parked();

        assert_eq!(seqs_of(dir.path(), OTHER), vec![1]);
        assert_eq!(
            std::fs::read(actions_path(dir.path(), SESSION)).unwrap(),
            before
        );
    }

    #[gpui::test]
    fn a_held_answer_is_written_to_its_own_session_when_the_session_changes(
        cx: &mut TestAppContext,
    ) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, answer("D7")));

        show(cx, &board, Some(snapshot(OTHER, 1, 0)));
        cx.run_until_parked();
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1]);
        assert_eq!(
            file_of(dir.path(), SESSION).unwrap().actions[0].action,
            answer("D7")
        );
        assert!(
            file_of(dir.path(), OTHER).is_none(),
            "nothing is written for the new session yet"
        );
    }

    #[gpui::test]
    fn a_board_of_another_session_that_takes_no_actions_leaves_no_queue(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        show(cx, &board, Some(snapshot(OTHER, 0, 0)));

        assert_eq!(session_of(cx, &outbox), None);
        assert!(!read(cx, &outbox, |o| o.can_act()));
    }

    #[gpui::test]
    fn an_existing_file_keeps_the_actions_above_acted_and_the_seq_goes_on(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let existing = format!(
            r#"{{"version":1,"session":"{SESSION}","actions":[{{"seq":1,"at":1,"do":"wrap_up","on":true}},{{"seq":3,"at":2,"do":"clear_rows"}}]}}"#
        );
        std::fs::write(actions_path(dir.path(), SESSION), existing).unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 1)));
        assert_eq!(
            seqs_of(dir.path(), SESSION),
            vec![1, 3],
            "nothing is written on adopt"
        );

        assert!(send(cx, &outbox, Action::ClearItems));
        cx.run_until_parked();
        assert_eq!(seqs_of(dir.path(), SESSION), vec![3, 4]);
    }

    #[gpui::test]
    fn a_write_error_is_shown_cut_to_sixty_characters(cx: &mut TestAppContext) {
        let parent = tempfile::tempdir().unwrap();
        // The board directory is a file, so the actions directory can not be made.
        let blocked = parent.path().join("not-a-dir");
        std::fs::write(&blocked, b"x").unwrap();
        let (board, outbox) = setup(cx, Some(blocked));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));

        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();
        let line = status(cx).unwrap();
        assert!(line.starts_with("actions not saved: "), "{line}");
        assert!(line.chars().count() <= MAX_ERROR_CHARS);
    }

    #[gpui::test]
    fn a_failed_rename_leaves_no_tmp_file(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        // The actions file is a directory, so the rename of the `.tmp` file onto it fails.
        std::fs::create_dir(actions_path(dir.path(), SESSION)).unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));

        assert!(send(cx, &outbox, wrap_up(true)));
        cx.run_until_parked();
        let line = status(cx).unwrap();
        assert!(line.starts_with("actions not saved: "), "{line}");
        let mut tmp = actions_path(dir.path(), SESSION).into_os_string();
        tmp.push(".tmp");
        assert!(!PathBuf::from(tmp).exists(), "the .tmp file is removed");
    }

    #[gpui::test]
    fn a_snapshot_of_the_same_session_during_a_read_does_not_read_again(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        // Two snapshots of one session before the read of its file finishes.
        for _ in 0..2 {
            cx.update(|app| {
                board.update(app, |b, cx| {
                    b.set(Some(snapshot(SESSION, 1, 0)), None, None, cx)
                })
            });
        }
        cx.run_until_parked();

        assert_eq!(reads(cx, &outbox), 1, "the read is not started again");
        assert_eq!(session_of(cx, &outbox), Some(SESSION.to_owned()));
        assert!(read(cx, &outbox, |o| o.can_act()));
    }

    #[gpui::test]
    fn a_session_that_returns_after_its_queue_was_dropped_is_read_again(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        show(cx, &board, Some(snapshot(OTHER, 1, 0)));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));

        assert_eq!(
            reads(cx, &outbox),
            3,
            "each change of session reads its file"
        );
        assert_eq!(session_of(cx, &outbox), Some(SESSION.to_owned()));
        assert!(read(cx, &outbox, |o| o.can_act()));
    }

    #[gpui::test]
    fn quit_writes_the_held_actions_at_once(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (board, outbox) = setup(cx, Some(dir.path().to_path_buf()));
        show(cx, &board, Some(snapshot(SESSION, 1, 0)));
        assert!(send(cx, &outbox, answer("D7")));

        // The quit hook awaits this write. The test runs it to the end instead.
        let _last_write = cx.update(|app| outbox.update(app, |o, cx| o.release_for_quit(cx)));
        cx.run_until_parked();
        assert_eq!(seqs_of(dir.path(), SESSION), vec![1]);
        assert_eq!(writes(cx, &outbox), 1);
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(
            writes(cx, &outbox),
            1,
            "the held answer is not written twice"
        );
    }

    #[test]
    fn a_long_write_error_is_cut_to_sixty_characters() {
        let error = std::io::Error::other("x".repeat(100));
        let line = save_error_line(&error);
        assert_eq!(line.chars().count(), MAX_ERROR_CHARS);
        assert!(line.starts_with("actions not saved: x"));
    }

    #[test]
    fn a_failure_line_drops_the_key_when_it_is_missing_or_empty() {
        let keyed = ActionFailure {
            seq: 1,
            key: Some("D7".to_owned()),
            text: "D7 is not open".to_owned(),
        };
        let blank = ActionFailure {
            key: Some(String::new()),
            ..keyed.clone()
        };
        assert_eq!(failure_line(&keyed), "D7 not sent: D7 is not open");
        assert_eq!(failure_line(&blank), "not sent: D7 is not open");
    }
}
