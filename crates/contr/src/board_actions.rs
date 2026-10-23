//! The actions that contr sends to the coordinator mod, and the queue that holds them. An answer,
//! done or dismiss waits `UNDO_MS` before it is written, so the owner can undo it. The other actions
//! are written at once. The mod takes each action once, by `seq`, and reports the highest `seq` it
//! took as `acted` in the board file.
//! Pure: no file reads or writes and no GPUI. The file writes are in the outbox entity.

use crate::board_model::{ActionFailure, NeedItem};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

/// The actions-file version this contr writes. The mod reads only this version.
pub const ACTIONS_VERSION: u32 = 1;
/// How long an answer, done or dismiss waits before it is written. Undo works in this window.
pub const UNDO_MS: u64 = 5_000;

/// The actions file for a session: `<dir>/<session>.actions.json`.
pub fn actions_path(dir: &Path, session: &str) -> PathBuf {
    dir.join(format!("{session}.actions.json"))
}

/// One action. The `do` field names the kind. The other fields depend on the kind.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "do", rename_all = "snake_case")]
pub enum Action {
    /// Picks option `option` of decision `key`. `label` is that option's text, which the mod checks.
    Answer {
        key: String,
        option: usize,
        label: String,
    },
    Done {
        key: String,
    },
    Dismiss {
        key: String,
    },
    /// Puts `key:` in the coordinator's prompt. Nothing is submitted.
    Chat {
        key: String,
    },
    ClearRows,
    ClearItems,
    WrapUp {
        on: bool,
    },
}

impl Action {
    /// The item that this action names. `None` for the section actions.
    pub fn key(&self) -> Option<&str> {
        match self {
            Action::Answer { key, .. }
            | Action::Done { key }
            | Action::Dismiss { key }
            | Action::Chat { key } => Some(key.as_str()),
            Action::ClearRows | Action::ClearItems | Action::WrapUp { .. } => None,
        }
    }

    /// `true` for an answer, done or dismiss. These wait the undo window before they are written.
    pub fn holds(&self) -> bool {
        matches!(
            self,
            Action::Answer { .. } | Action::Done { .. } | Action::Dismiss { .. }
        )
    }

    /// The answer that the item shows: the label, `done` or `dismissed`. `None` for other actions.
    pub fn answer_text(&self) -> Option<String> {
        match self {
            Action::Answer { label, .. } => Some(label.clone()),
            Action::Done { .. } => Some("done".to_owned()),
            Action::Dismiss { .. } => Some("dismissed".to_owned()),
            Action::Chat { .. }
            | Action::ClearRows
            | Action::ClearItems
            | Action::WrapUp { .. } => None,
        }
    }
}

/// One action with its sequence number and its time. `seq` grows by one for each action in a session.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Envelope {
    pub seq: u64,
    /// Wall-clock milliseconds since the Unix epoch.
    pub at: u64,
    #[serde(flatten)]
    pub action: Action,
}

/// The actions file. Field names are the schema's.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ActionsFile {
    pub version: u32,
    pub session: String,
    #[serde(default)]
    pub actions: Vec<Envelope>,
}

/// Parses an actions file. `None` for bad JSON, a missing version, or a version other than
/// `ACTIONS_VERSION`.
pub fn parse_actions(bytes: &[u8]) -> Option<ActionsFile> {
    let file: ActionsFile = serde_json::from_slice(bytes).ok()?;
    (file.version == ACTIONS_VERSION).then_some(file)
}

/// What contr has queued for one session. A held action can still be undone. A held action has no
/// `seq` yet: it gets one when it is written, so the `seq` order is the write order. A written
/// action stays in the file until the mod takes it, which the board file reports as `acted`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Outbox {
    session: String,
    next_seq: u64,
    /// Held actions in push order, each with its click time (ms) and the time (ms) at which it is
    /// written.
    held: Vec<(Action, u64, u64)>,
    /// Written actions, ascending by `seq`. A new write gets a `seq` above all of them, so it goes
    /// at the end.
    written: Vec<Envelope>,
    /// Failures with a `seq` up to this one are not shown. It starts at `acted`.
    seen_failed: u64,
}

/// `true` for an answer, done or dismiss for `key`.
fn is_answer_for(action: &Action, key: &str) -> bool {
    action.key() == Some(key) && action.answer_text().is_some()
}

impl Outbox {
    /// A queue for `session`, after the mod took actions up to `acted`. The entries of `existing`
    /// with a `seq` above `acted` are kept as written. An existing file of another session is
    /// ignored. The next `seq` follows the highest `seq` that is known. Failures up to `acted` are
    /// not shown.
    pub fn new(session: String, acted: u64, existing: Option<ActionsFile>) -> Self {
        let mut highest = acted;
        let mut written = Vec::new();
        if let Some(file) = existing.filter(|f| f.session == session) {
            for env in file.actions {
                highest = highest.max(env.seq);
                if env.seq > acted {
                    written.push(env);
                }
            }
        }
        written.sort_by_key(|env| env.seq);
        Self {
            session,
            next_seq: highest.saturating_add(1),
            held: Vec::new(),
            written,
            seen_failed: acted,
        }
    }

    pub fn session(&self) -> &str {
        self.session.as_str()
    }

    /// Queues `action` at `now_ms`. An answer, done or dismiss is held until `now_ms + UNDO_MS`
    /// and returns `None`: it gets its `seq` when it is written. Any other action is written at
    /// once and returns its `seq`.
    pub fn push(&mut self, action: Action, now_ms: u64) -> Option<u64> {
        if action.holds() {
            let release_at = now_ms.saturating_add(UNDO_MS);
            self.held.push((action, now_ms, release_at));
            return None;
        }
        let seq = self.take_seq();
        self.written.push(Envelope {
            seq,
            at: now_ms,
            action,
        });
        Some(seq)
    }

    /// Removes the held answer, done or dismiss for `key`, if it is the latest one for `key`. `true`
    /// if one was removed. An action that was already written can not be undone.
    pub fn undo(&mut self, key: &str) -> bool {
        let Some(pos) = self
            .held
            .iter()
            .rposition(|(action, _, _)| is_answer_for(action, key))
        else {
            return false;
        };
        self.held.remove(pos);
        true
    }

    /// Writes the held actions whose time has come (`release_at <= now_ms`), in push order. Each
    /// one gets the next `seq` now. `true` if any was written.
    pub fn release(&mut self, now_ms: u64) -> bool {
        let (due, kept): (Vec<_>, Vec<_>) = std::mem::take(&mut self.held)
            .into_iter()
            .partition(|(_, _, release_at)| *release_at <= now_ms);
        self.held = kept;
        self.write_held(due)
    }

    /// Writes every held action at once, in push order, for quit. `true` if any was written.
    pub fn release_all(&mut self) -> bool {
        let all = std::mem::take(&mut self.held);
        self.write_held(all)
    }

    /// Drops the written actions that the mod has taken, which are those with `seq <= acted`. `true`
    /// if any was dropped.
    pub fn ack(&mut self, acted: u64) -> bool {
        let before = self.written.len();
        self.written.retain(|env| env.seq > acted);
        self.written.len() != before
    }

    /// The failures with a `seq` above the last one seen, in the order given. Each failure is
    /// returned once. A later call returns only the failures that are newer.
    pub fn new_failures<'a>(&mut self, failed: &'a [ActionFailure]) -> Vec<&'a ActionFailure> {
        let fresh: Vec<&'a ActionFailure> =
            failed.iter().filter(|f| f.seq > self.seen_failed).collect();
        if let Some(top) = fresh.iter().map(|f| f.seq).max() {
            self.seen_failed = top;
        }
        fresh
    }

    /// The actions file to write: the written actions, in `seq` order.
    pub fn file(&self) -> ActionsFile {
        ActionsFile {
            version: ACTIONS_VERSION,
            session: self.session.clone(),
            actions: self.written.clone(),
        }
    }

    /// The answer text for `key` and whether it can still be undone. This is the latest answer,
    /// done or dismiss for `key`, held or written. A chat does not hide it.
    pub fn pending_answer(&self, key: &str) -> Option<(String, bool)> {
        let (action, held) = self.latest_answer(key)?;
        Some((action.answer_text()?, held))
    }

    /// The earliest time (ms) at which a held action is written, or `None` when nothing is held.
    pub fn next_release(&self) -> Option<u64> {
        self.held.iter().map(|(_, _, release_at)| *release_at).min()
    }

    /// The latest answer, done or dismiss, for `key`, with `true` when it is still held. A held
    /// answer is newer than a written one: a written action was pushed before every held action,
    /// because a later push is due later.
    fn latest_answer(&self, key: &str) -> Option<(&Action, bool)> {
        self.held
            .iter()
            .rev()
            .map(|(action, _, _)| action)
            .find(|action| is_answer_for(action, key))
            .map(|action| (action, true))
            .or_else(|| {
                self.written
                    .iter()
                    .filter(|env| is_answer_for(&env.action, key))
                    .max_by_key(|env| env.seq)
                    .map(|env| (&env.action, false))
            })
    }

    /// The next `seq`. Each call gives a larger `seq` than the one before.
    fn take_seq(&mut self) -> u64 {
        let seq = self.next_seq;
        self.next_seq = seq.saturating_add(1);
        seq
    }

    /// Writes the due held actions in the order given. Each one gets the next `seq`. `true` if
    /// `due` was not empty.
    fn write_held(&mut self, due: Vec<(Action, u64, u64)>) -> bool {
        let any = !due.is_empty();
        for (action, at, _) in due {
            let seq = self.take_seq();
            self.written.push(Envelope { seq, at, action });
        }
        any
    }
}

/// The options of a decision, in card order: the recommended one first, then the others in order.
/// Each entry is the option's index in `options`, its text, and whether it is the recommended one.
/// A `recommended` that is out of range marks none.
pub fn card_options(item: &NeedItem) -> Vec<(usize, &str, bool)> {
    let rec = item.recommended.filter(|&r| r < item.options.len());
    let mut out = Vec::with_capacity(item.options.len());
    out.extend(rec.and_then(|r| item.options.get(r).map(|text| (r, text.as_str(), true))));
    out.extend(
        item.options
            .iter()
            .enumerate()
            .filter(|(i, _)| Some(*i) != rec)
            .map(|(i, text)| (i, text.as_str(), false)),
    );
    out
}

/// The item after `key` in slot order that `is_open` accepts. If there is none, the first other
/// accepted item. If there is none of those either, `None`. This is the mod's "next" rule.
pub fn next_open<'a>(
    items: &'a [NeedItem],
    key: &str,
    is_open: impl Fn(&NeedItem) -> bool,
) -> Option<&'a NeedItem> {
    let after = items
        .iter()
        .position(|it| it.key == key)
        .map_or(0, |p| p + 1);
    let other_open = |it: &&NeedItem| it.key != key && is_open(it);
    items
        .get(after..)
        .unwrap_or_default()
        .iter()
        .find(other_open)
        .or_else(|| items.iter().find(other_open))
}

/// How long ago `at_ms` was, for a card: `now` under a minute, then `5m`, `2h` and `3d`. A time in
/// the future reads as `now`.
pub fn ago(now_ms: u64, at_ms: u64) -> String {
    let secs = now_ms.saturating_sub(at_ms) / 1_000;
    match secs {
        0..60 => "now".to_owned(),
        60..3_600 => format!("{}m", secs / 60),
        3_600..86_400 => format!("{}h", secs / 3_600),
        _ => format!("{}d", secs / 86_400),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    fn answer(key: &str, option: usize, label: &str) -> Action {
        Action::Answer {
            key: key.to_owned(),
            option,
            label: label.to_owned(),
        }
    }

    fn done(key: &str) -> Action {
        Action::Done {
            key: key.to_owned(),
        }
    }

    fn env(seq: u64, at: u64, action: Action) -> Envelope {
        Envelope { seq, at, action }
    }

    fn seqs(file: &ActionsFile) -> Vec<u64> {
        file.actions.iter().map(|e| e.seq).collect()
    }

    fn outbox(acted: u64, existing: Option<ActionsFile>) -> Outbox {
        Outbox::new("s1".to_owned(), acted, existing)
    }

    fn item(json: &str) -> NeedItem {
        serde_json::from_str(json).unwrap()
    }

    fn file_of(session: &str, actions: Vec<Envelope>) -> ActionsFile {
        ActionsFile {
            version: ACTIONS_VERSION,
            session: session.to_owned(),
            actions,
        }
    }

    #[test]
    fn each_action_serializes_to_its_contract_json() {
        let table = [
            (
                answer("D7", 1, "two lines"),
                r#"{"do":"answer","key":"D7","option":1,"label":"two lines"}"#,
            ),
            (done("T12"), r#"{"do":"done","key":"T12"}"#),
            (
                Action::Dismiss {
                    key: "T13".to_owned(),
                },
                r#"{"do":"dismiss","key":"T13"}"#,
            ),
            (
                Action::Chat {
                    key: "D8".to_owned(),
                },
                r#"{"do":"chat","key":"D8"}"#,
            ),
            (Action::ClearRows, r#"{"do":"clear_rows"}"#),
            (Action::ClearItems, r#"{"do":"clear_items"}"#),
            (Action::WrapUp { on: true }, r#"{"do":"wrap_up","on":true}"#),
        ];
        for (action, json) in table {
            assert_eq!(serde_json::to_string(&action).unwrap(), json);
            assert_eq!(serde_json::from_str::<Action>(json).unwrap(), action);
        }
    }

    #[test]
    fn envelope_puts_seq_and_at_before_the_action_fields() {
        let e = env(17, 1_761_234_567_890, answer("D7", 1, "two lines"));
        let json = r#"{"seq":17,"at":1761234567890,"do":"answer","key":"D7","option":1,"label":"two lines"}"#;
        assert_eq!(serde_json::to_string(&e).unwrap(), json);
        assert_eq!(serde_json::from_str::<Envelope>(json).unwrap(), e);
    }

    #[test]
    fn envelope_of_a_unit_action_parses() {
        let json = r#"{"seq":22,"at":1761234572000,"do":"clear_items"}"#;
        let e: Envelope = serde_json::from_str(json).unwrap();
        assert_eq!(e, env(22, 1_761_234_572_000, Action::ClearItems));
    }

    #[test]
    fn actions_file_round_trips() {
        let file = file_of(
            "s1",
            vec![
                env(17, 1, answer("D7", 1, "two lines")),
                env(18, 2, done("T12")),
            ],
        );
        let bytes = serde_json::to_vec(&file).unwrap();
        assert_eq!(parse_actions(&bytes), Some(file));
    }

    #[test]
    fn parse_actions_reads_a_file_without_actions_as_empty() {
        let got = parse_actions(br#"{"version":1,"session":"s1"}"#);
        assert_eq!(got, Some(file_of("s1", vec![])));
    }

    #[test]
    fn parse_actions_rejects_version_two() {
        let bytes = br#"{"version":2,"session":"s1","actions":[]}"#;
        assert_eq!(parse_actions(bytes), None);
    }

    #[test]
    fn parse_actions_rejects_bad_json_and_a_missing_version() {
        assert_eq!(parse_actions(b"not json"), None);
        assert_eq!(parse_actions(br#"{"session":"s1","actions":[]}"#), None);
    }

    #[test]
    fn parse_actions_rejects_an_unknown_action_kind() {
        let bytes = br#"{"version":1,"session":"s1","actions":[{"seq":1,"at":1,"do":"explode"}]}"#;
        assert_eq!(parse_actions(bytes), None);
    }

    #[test]
    fn actions_path_is_the_session_file_in_the_board_dir() {
        assert_eq!(
            actions_path(Path::new("dir"), "s1"),
            PathBuf::from("dir").join("s1.actions.json")
        );
    }

    #[test]
    fn key_holds_and_answer_text_follow_the_kind() {
        let table: [(Action, Option<&str>, bool, Option<&str>); 7] = [
            (
                answer("D7", 1, "two lines"),
                Some("D7"),
                true,
                Some("two lines"),
            ),
            (done("T12"), Some("T12"), true, Some("done")),
            (
                Action::Dismiss {
                    key: "T13".to_owned(),
                },
                Some("T13"),
                true,
                Some("dismissed"),
            ),
            (
                Action::Chat {
                    key: "D8".to_owned(),
                },
                Some("D8"),
                false,
                None,
            ),
            (Action::ClearRows, None, false, None),
            (Action::ClearItems, None, false, None),
            (Action::WrapUp { on: true }, None, false, None),
        ];
        for (action, key, holds, text) in table {
            assert_eq!(action.key(), key, "key of {action:?}");
            assert_eq!(action.holds(), holds, "holds {action:?}");
            assert_eq!(action.answer_text().as_deref(), text, "text of {action:?}");
        }
    }

    #[test]
    fn push_holds_an_answer_without_a_seq_and_writes_a_wrap_up_at_once() {
        let mut q = outbox(0, None);
        assert_eq!(q.push(answer("D7", 1, "two lines"), 1_000), None);
        assert_eq!(q.push(Action::WrapUp { on: true }, 1_000), Some(1));
        assert_eq!(
            q.file().actions,
            [env(1, 1_000, Action::WrapUp { on: true })]
        );
        assert_eq!(q.next_release(), Some(1_000 + UNDO_MS));
    }

    #[test]
    fn an_undone_answer_takes_no_seq() {
        let mut q = outbox(0, None);
        assert_eq!(q.push(answer("D7", 1, "two lines"), 0), None);
        assert!(q.undo("D7"));
        assert_eq!(q.push(Action::ClearRows, 0), Some(1));
    }

    #[test]
    fn release_waits_for_the_undo_window() {
        let mut q = outbox(0, None);
        q.push(answer("D7", 1, "two lines"), 1_000);
        assert!(!q.release(1_000 + UNDO_MS - 1));
        assert!(q.file().actions.is_empty());
        assert!(q.release(1_000 + UNDO_MS));
        assert_eq!(
            q.file().actions,
            [env(1, 1_000, answer("D7", 1, "two lines"))]
        );
        assert_eq!(q.next_release(), None);
        assert!(!q.release(u64::MAX));
    }

    #[test]
    fn a_held_action_gets_its_seq_after_the_written_ones() {
        let mut q = outbox(0, None);
        assert_eq!(q.push(done("T12"), 0), None);
        assert_eq!(q.push(Action::ClearRows, 0), Some(1));
        assert!(q.release(UNDO_MS));
        assert_eq!(
            q.file().actions,
            [env(1, 0, Action::ClearRows), env(2, 0, done("T12"))]
        );
    }

    #[test]
    fn released_actions_get_their_seqs_in_push_order() {
        let mut q = outbox(0, None);
        assert_eq!(q.push(done("T12"), 0), None);
        assert_eq!(q.push(Action::ClearRows, 0), Some(1));
        assert_eq!(q.push(answer("D7", 1, "two lines"), 10), None);
        assert!(q.release(UNDO_MS + 10));
        assert_eq!(
            q.file().actions,
            [
                env(1, 0, Action::ClearRows),
                env(2, 0, done("T12")),
                env(3, 10, answer("D7", 1, "two lines")),
            ]
        );
    }

    #[test]
    fn a_held_answer_gets_a_seq_above_the_action_the_mod_took_meanwhile() {
        let mut q = outbox(0, None);
        assert_eq!(q.push(answer("D7", 1, "two lines"), 0), None);
        assert_eq!(q.push(Action::WrapUp { on: true }, 1_000), Some(1));
        // The mod takes the wrap up and reports `acted` = 1 before the answer is written.
        assert!(q.ack(1));
        assert_eq!(q.pending_answer("D7"), Some(("two lines".to_owned(), true)));
        assert!(q.release(UNDO_MS));
        let file = q.file();
        assert_eq!(file.actions, [env(2, 0, answer("D7", 1, "two lines"))]);
        assert!(file.actions[0].seq > 1);
        assert_eq!(
            q.pending_answer("D7"),
            Some(("two lines".to_owned(), false))
        );
    }

    #[test]
    fn undo_removes_the_latest_held_answer_for_the_key() {
        let mut q = outbox(0, None);
        q.push(answer("D7", 1, "two lines"), 0);
        q.push(done("D7"), 0);
        assert!(q.undo("D7"));
        assert_eq!(q.pending_answer("D7"), Some(("two lines".to_owned(), true)));
        assert!(q.release(UNDO_MS));
        assert_eq!(seqs(&q.file()), [1]);
    }

    #[test]
    fn undo_of_a_newer_held_answer_leaves_the_written_one_pending() {
        let mut q = outbox(0, None);
        q.push(answer("D7", 1, "two lines"), 0);
        assert!(q.release(UNDO_MS));
        assert_eq!(q.push(done("D7"), UNDO_MS), None);
        assert_eq!(q.pending_answer("D7"), Some(("done".to_owned(), true)));
        assert!(q.undo("D7"));
        assert_eq!(
            q.pending_answer("D7"),
            Some(("two lines".to_owned(), false))
        );
    }

    #[test]
    fn undo_removes_only_a_held_answer() {
        let mut q = outbox(0, None);
        q.push(answer("D7", 1, "two lines"), 0);
        assert_eq!(q.pending_answer("D7"), Some(("two lines".to_owned(), true)));
        assert!(q.undo("D7"));
        assert_eq!(q.pending_answer("D7"), None);
        assert!(!q.undo("D7"));
        assert!(!q.release_all());
    }

    #[test]
    fn undo_is_false_after_the_answer_is_written() {
        let mut q = outbox(0, None);
        q.push(answer("D8", 0, "one line"), 0);
        assert!(q.release(UNDO_MS));
        assert!(!q.undo("D8"));
        assert_eq!(seqs(&q.file()), [1]);
        assert_eq!(q.pending_answer("D8"), Some(("one line".to_owned(), false)));
    }

    #[test]
    fn undo_is_false_for_an_unknown_key() {
        let mut q = outbox(0, None);
        q.push(done("T12"), 0);
        assert!(!q.undo("T99"));
        assert_eq!(q.next_release(), Some(UNDO_MS));
    }

    #[test]
    fn release_all_writes_every_held_action_for_quit() {
        let mut q = outbox(0, None);
        q.push(done("T12"), 0);
        q.push(answer("D7", 1, "two lines"), 0);
        assert!(q.release_all());
        assert_eq!(seqs(&q.file()), [1, 2]);
        assert!(!q.release_all());
        assert_eq!(q.next_release(), None);
    }

    #[test]
    fn new_keeps_existing_entries_above_acted_and_continues_the_seq() {
        let existing = file_of(
            "s1",
            vec![env(16, 1, done("T11")), env(18, 2, Action::ClearRows)],
        );
        let mut q = outbox(17, Some(existing));
        assert_eq!(q.session(), "s1");
        assert_eq!(q.file().version, ACTIONS_VERSION);
        assert_eq!(seqs(&q.file()), [18]);
        assert_eq!(q.push(Action::ClearItems, 0), Some(19));
    }

    #[test]
    fn new_continues_after_the_highest_existing_seq() {
        let existing = file_of("s1", vec![env(40, 1, Action::ClearRows)]);
        let mut q = outbox(17, Some(existing));
        assert_eq!(q.push(Action::ClearItems, 0), Some(41));
    }

    #[test]
    fn new_without_a_file_starts_after_acted() {
        let mut q = outbox(17, None);
        assert_eq!(q.push(Action::WrapUp { on: false }, 0), Some(18));
    }

    #[test]
    fn new_ignores_an_existing_file_of_another_session() {
        let existing = file_of("other", vec![env(30, 1, Action::ClearRows)]);
        let mut q = outbox(17, Some(existing));
        assert!(q.file().actions.is_empty());
        assert_eq!(q.push(Action::ClearItems, 0), Some(18));
    }

    #[test]
    fn ack_drops_the_taken_actions_only() {
        let existing = file_of(
            "s1",
            vec![
                env(16, 1, Action::ClearRows),
                env(17, 2, Action::ClearItems),
                env(18, 3, Action::WrapUp { on: true }),
                env(19, 4, Action::WrapUp { on: false }),
            ],
        );
        let mut q = outbox(15, Some(existing));
        assert_eq!(seqs(&q.file()), [16, 17, 18, 19]);
        assert!(q.ack(18));
        assert_eq!(seqs(&q.file()), [19]);
        assert!(!q.ack(18));
    }

    #[test]
    fn new_failures_returns_each_failure_once() {
        let mut q = outbox(16, None);
        let failed = [
            ActionFailure {
                seq: 16,
                key: Some("D5".to_owned()),
                text: "D5 is not open".to_owned(),
            },
            ActionFailure {
                seq: 17,
                key: Some("D7".to_owned()),
                text: "D7 is not open".to_owned(),
            },
        ];
        let got: Vec<u64> = q.new_failures(&failed).iter().map(|f| f.seq).collect();
        assert_eq!(got, [17]);
        assert!(q.new_failures(&failed).is_empty());

        let later = [ActionFailure {
            seq: 18,
            key: None,
            text: "stale".to_owned(),
        }];
        let mut all = failed.to_vec();
        all.extend(later);
        let got: Vec<u64> = q.new_failures(&all).iter().map(|f| f.seq).collect();
        assert_eq!(got, [18]);
    }

    #[test]
    fn pending_answer_gives_the_label_and_the_undoable_flag() {
        let mut q = outbox(0, None);
        assert_eq!(q.pending_answer("D7"), None);
        q.push(answer("D7", 1, "two lines"), 0);
        assert_eq!(q.pending_answer("D7"), Some(("two lines".to_owned(), true)));
        q.push(
            Action::Chat {
                key: "D7".to_owned(),
            },
            0,
        );
        assert_eq!(q.pending_answer("D7"), Some(("two lines".to_owned(), true)));
        q.push(done("D7"), 0);
        assert_eq!(q.pending_answer("D7"), Some(("done".to_owned(), true)));
    }

    #[test]
    fn card_options_puts_the_recommended_one_first_with_original_indices() {
        let it = item(
            r#"{"key":"D7","kind":"ask","options":["one line","two lines","three"],"recommended":1}"#,
        );
        assert_eq!(
            card_options(&it),
            [
                (1, "two lines", true),
                (0, "one line", false),
                (2, "three", false),
            ]
        );
    }

    #[test]
    fn card_options_keeps_the_order_when_the_first_one_is_recommended() {
        let it = item(r#"{"key":"D7","kind":"ask","options":["a","b"],"recommended":0}"#);
        assert_eq!(card_options(&it), [(0, "a", true), (1, "b", false)]);
    }

    #[test]
    fn card_options_marks_none_without_a_recommended_option() {
        let it = item(r#"{"key":"D7","kind":"ask","options":["a","b"]}"#);
        assert_eq!(card_options(&it), [(0, "a", false), (1, "b", false)]);
    }

    #[test]
    fn card_options_marks_none_when_recommended_is_out_of_range() {
        let it = item(r#"{"key":"D7","kind":"ask","options":["a","b"],"recommended":2}"#);
        assert_eq!(card_options(&it), [(0, "a", false), (1, "b", false)]);
    }

    #[test]
    fn card_options_of_an_item_without_options_is_empty() {
        let it = item(r#"{"key":"D7","kind":"ask","recommended":0}"#);
        assert!(card_options(&it).is_empty());
    }

    /// Slot order: D7 open, T12 open, D6 resolved, N4 open.
    fn slot_items() -> Vec<NeedItem> {
        vec![
            item(r#"{"key":"D7","kind":"ask","open":true}"#),
            item(r#"{"key":"T12","kind":"do","open":true}"#),
            item(r#"{"key":"D6","kind":"ask","open":false}"#),
            item(r#"{"key":"N4","kind":"know","open":true}"#),
        ]
    }

    fn key_of(it: Option<&NeedItem>) -> Option<&str> {
        it.map(|i| i.key.as_str())
    }

    #[test]
    fn next_open_picks_the_next_open_item_after_the_key() {
        let items = slot_items();
        assert_eq!(key_of(next_open(&items, "D7", |it| it.open)), Some("T12"));
        assert_eq!(key_of(next_open(&items, "T12", |it| it.open)), Some("N4"));
        assert_eq!(key_of(next_open(&items, "D6", |it| it.open)), Some("N4"));
    }

    #[test]
    fn next_open_wraps_to_the_first_other_open_item() {
        let items = slot_items();
        assert_eq!(key_of(next_open(&items, "N4", |it| it.open)), Some("D7"));
    }

    #[test]
    fn next_open_is_none_when_no_other_item_is_open() {
        let items = [
            item(r#"{"key":"D7","kind":"ask","open":true}"#),
            item(r#"{"key":"D6","kind":"ask","open":false}"#),
        ];
        assert_eq!(key_of(next_open(&items, "D7", |it| it.open)), None);
        assert_eq!(key_of(next_open(&[], "D7", |it| it.open)), None);
    }

    #[test]
    fn next_open_uses_the_given_open_rule() {
        let items = slot_items();
        // A pending answer counts as not open, so N4 is skipped and the wrap finds D7.
        let rule = |it: &NeedItem| it.open && it.key != "N4";
        assert_eq!(key_of(next_open(&items, "T12", rule)), Some("D7"));
    }

    #[test]
    fn next_open_for_a_key_that_is_not_listed_takes_the_first_open_item() {
        let items = slot_items();
        assert_eq!(key_of(next_open(&items, "X9", |it| it.open)), Some("D7"));
    }

    #[test]
    fn ago_reads_seconds_minutes_hours_and_days() {
        const NOW: u64 = 1_761_234_567_890;
        assert_eq!(ago(NOW, NOW - 30_000), "now");
        assert_eq!(ago(NOW, NOW - 59_999), "now");
        assert_eq!(ago(NOW, NOW - 60_000), "1m");
        assert_eq!(ago(NOW, NOW - 5 * 60_000), "5m");
        assert_eq!(ago(NOW, NOW - 2 * 3_600_000), "2h");
        assert_eq!(ago(NOW, NOW - 3 * 86_400_000), "3d");
    }

    #[test]
    fn ago_reads_a_future_time_as_now() {
        assert_eq!(ago(1_000, 6_000), "now");
    }
}
