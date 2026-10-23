//! The board schema (v1) that the coordinator mod writes: the bridge token that names the session,
//! the board file it points to, and the status and label of each stage and needs-you item.
//! Pure: no file reads and no GPUI, so the rules are tested with literal values and a fixture.

use crate::status_icon::Status;
use contr_herdr::AgentMeta;
use serde::{Deserialize, Deserializer};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

/// The schema version this contr reads. A file or token of another version is not read.
pub const BOARD_VERSION: u32 = 1;
/// The pane token that marks the coordinator's pane.
pub const BRIDGE_TOKEN: &str = "contr_board";
/// The five stage columns, in order: plan, build, review, CI, you.
pub const STAGE_HEADS: [&str; 5] = ["plan", "build", "review", "CI", "you"];

/// The session and revision that a bridge token names.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BridgeRef {
    pub session: String,
    pub rev: u64,
}

/// Parses the bridge token `1 <session> <rev>` (single spaces). `None` for any other shape, another
/// version, a session outside `[A-Za-z0-9_-]{1,64}`, or a revision that is not digits.
pub fn parse_bridge_token(value: &str) -> Option<BridgeRef> {
    let parts: Vec<&str> = value.split(' ').collect();
    let [version, session, rev] = parts.as_slice() else {
        return None;
    };
    if digits(version)? != u64::from(BOARD_VERSION) || !is_session(session) {
        return None;
    }
    Some(BridgeRef {
        session: (*session).to_owned(),
        rev: digits(rev)?,
    })
}

/// A value made of ASCII digits only. Unlike `str::parse`, a sign is not accepted.
fn digits(value: &str) -> Option<u64> {
    if value.is_empty() || !value.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    value.parse().ok()
}

fn is_session(value: &str) -> bool {
    (1..=64).contains(&value.len())
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// The board file for a session: `<dir>/<session>.json`.
pub fn board_path(dir: &Path, session: &str) -> PathBuf {
    dir.join(format!("{session}.json"))
}

/// One board file. Field names are the schema's; unknown fields are ignored.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct BoardSnapshot {
    pub version: u32,
    pub session: String,
    pub rev: u64,
    /// The mod's exact summary line.
    #[serde(default)]
    pub summary: String,
    /// `true` shows the header label "keep going"; `false` shows "wrap up".
    #[serde(default)]
    pub wrap_up: bool,
    /// The open needs-you items: the "Needs you N" count.
    #[serde(default)]
    pub need_count: u32,
    /// The work section has finished or joined rows, so "clear done" shows.
    #[serde(default)]
    pub clear_rows: bool,
    /// The needs-you section has resolved items, so "clear done" shows.
    #[serde(default)]
    pub clear_items: bool,
    #[serde(default)]
    pub context: Option<ContextUse>,
    /// Work rows in the mod's slot order. contr never reorders them.
    #[serde(default)]
    pub rows: Vec<WorkRow>,
    /// Needs-you items in the mod's slot order. contr never reorders them.
    #[serde(default)]
    pub items: Vec<NeedItem>,
    /// #54: what the coordinator mod knows about each worker pane. Missing, null or not a list is
    /// empty. A bad entry is dropped; the rest of the board still reads.
    #[serde(default, deserialize_with = "lenient_agents")]
    pub agents: Vec<BoardAgent>,
    /// #10: `1` means the mod takes actions-file v1. Absent or `0`: contr's action buttons stay inert.
    #[serde(default)]
    pub actions: u32,
    /// #10: the highest action `seq` the mod has taken, whether it worked or failed.
    #[serde(default)]
    pub acted: u64,
    /// #10: the last failed actions, ascending by `seq`. contr shows each new one once.
    #[serde(default)]
    pub failed: Vec<ActionFailure>,
}

/// #10: one action that the mod could not take. `key` is the item it named, if any.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct ActionFailure {
    pub seq: u64,
    #[serde(default)]
    pub key: Option<String>,
    #[serde(default)]
    pub text: String,
}

/// Reads `agents` leniently. A list keeps each entry that reads as a `BoardAgent` and drops the
/// others. Any other value (a string, a number, null) reads as no entries.
fn lenient_agents<'de, D>(deserializer: D) -> Result<Vec<BoardAgent>, D::Error>
where
    D: Deserializer<'de>,
{
    let value = serde_json::Value::deserialize(deserializer)?;
    let serde_json::Value::Array(entries) = value else {
        return Ok(Vec::new());
    };
    Ok(entries
        .into_iter()
        .filter_map(|entry| serde_json::from_value::<BoardAgent>(entry).ok())
        .collect())
}

/// #54: one worker as the coordinator mod knows it. Blank or missing `name`, `model` and `role`
/// mean not known; the agents list then shows the herdr tab label and the agent kind.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct BoardAgent {
    /// The herdr pane id of the worker.
    pub pane_id: String,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub model: Option<String>,
    #[serde(default)]
    pub role: Option<String>,
}

/// The agents list's data per pane id. `name`, `model` and `role` come from `agents` (the first
/// entry for a pane wins). `link` is the `label` of the first work row in slot order with that
/// `pane_id` that is not `finished` and has no `joined`. A pane in neither gets no entry.
pub fn agent_meta(board: &BoardSnapshot) -> BTreeMap<String, AgentMeta> {
    let mut map: BTreeMap<String, AgentMeta> = BTreeMap::new();
    for agent in &board.agents {
        map.entry(agent.pane_id.clone()).or_insert(AgentMeta {
            name: agent.name.clone(),
            model: agent.model.clone(),
            role: agent.role.clone(),
            link: None,
        });
    }
    for row in &board.rows {
        if row.finished || row.joined.is_some() {
            continue;
        }
        let Some(pane_id) = &row.pane_id else {
            continue;
        };
        let meta = map.entry(pane_id.clone()).or_default();
        if meta.link.is_none() {
            meta.link = Some(row.label.clone());
        }
    }
    map
}

/// Context use of the coordinator. The panel does not draw it in #9.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
pub struct ContextUse {
    pub tokens: u64,
    pub limit: u64,
}

/// One work row: an issue or a pull request, with five stages.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct WorkRow {
    /// The stable slot key.
    pub key: String,
    #[serde(default)]
    pub label: String,
    #[serde(default)]
    pub title: String,
    /// Plan, build, review, CI, you. A missing stage reads as `Stage::None`; see `stage`.
    #[serde(default)]
    pub stages: Vec<Stage>,
    #[serde(default)]
    pub who: Who,
    /// The task this slot joined, drawn as `joined #641` and struck through.
    #[serde(default)]
    pub joined: Option<String>,
    /// Nothing runs or waits on this row: drawn dim.
    #[serde(default)]
    pub finished: bool,
    /// #10: the sentence for the answer card.
    #[serde(default)]
    pub now: Option<String>,
    /// #10: the open needs-you item this row waits on.
    #[serde(default)]
    pub item: Option<String>,
    /// #10: the worker pane that "Focus tab" moves to.
    #[serde(default)]
    pub pane_id: Option<String>,
    /// #10: the pull request that "Open PR" opens.
    #[serde(default)]
    pub url: Option<String>,
    /// #11: the absolute path of the worker's RESULT.md.
    #[serde(default)]
    pub report: Option<String>,
    /// #11: the worker that wrote the report.
    #[serde(default)]
    pub report_by: Option<String>,
}

impl WorkRow {
    /// The stage at `index`: 0 is plan and 4 is you. A missing stage reads as `Stage::None`.
    pub fn stage(&self, index: usize) -> Stage {
        self.stages.get(index).copied().unwrap_or(Stage::None)
    }
}

/// One stage cell. Any other string is `Unknown`, which draws as not started.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Stage {
    Done,
    Run,
    Wait,
    None,
    Ask,
    Todo,
    Block,
    #[serde(other)]
    Unknown,
}

/// The `who` text of a work row and whose it is. A missing `who` is an empty `Text`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct Who {
    #[serde(default)]
    pub text: String,
    #[serde(default)]
    pub kind: WhoKind,
}

/// Whose a `who` text is about. `you` is the owner's turn. Any other string is `Other`.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum WhoKind {
    You,
    Dim,
    #[default]
    Text,
    #[serde(other)]
    Other,
}

/// One needs-you item: a decision, a to-do or news.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct NeedItem {
    /// The stable slot key, such as `D7` or `T12`.
    pub key: String,
    pub kind: ItemKind,
    #[serde(default)]
    pub title: String,
    /// `false`: resolved, struck through until "clear done". A missing `open` is `true`.
    #[serde(default = "yes")]
    pub open: bool,
    /// #10: the task the item belongs to.
    #[serde(default)]
    pub task: Option<String>,
    /// #10: the full question.
    #[serde(default)]
    pub text: Option<String>,
    /// #10: the pane that asked.
    #[serde(default)]
    pub from: Option<String>,
    /// #10: when the item was raised, in Unix milliseconds.
    #[serde(default)]
    pub at: Option<u64>,
    /// #10: the option lines, in order.
    #[serde(default)]
    pub options: Vec<String>,
    /// #10: the index into `options` that is recommended, drawn as the filled button.
    #[serde(default)]
    pub recommended: Option<usize>,
    /// #10: a to-do's link.
    #[serde(default)]
    pub link: Option<String>,
    /// #10: how a resolved item ended, in one line.
    #[serde(default)]
    pub answer: Option<String>,
}

/// The serde default of `open`: an item without it is open.
fn yes() -> bool {
    true
}

/// `ask` is a decision, `do` a to-do, `know` news. Any other string is `Other`, which reads as news.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ItemKind {
    Ask,
    Do,
    Know,
    #[serde(other)]
    Other,
}

/// Why a board file was not read.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BoardError {
    /// The file's `version` is not `BOARD_VERSION`.
    Version(u32),
    /// Not JSON, not UTF-8, or missing `version` or a required field. The text is serde's message.
    Json(String),
}

impl BoardError {
    /// The one-line message for the panel.
    pub fn line(&self) -> String {
        match self {
            BoardError::Version(v) => format!("board v{v} · this contr reads v{BOARD_VERSION}"),
            BoardError::Json(e) => format!(
                "board unreadable: {}",
                e.chars().take(60).collect::<String>()
            ),
        }
    }
}

/// Only the version, read first, so a board of another version is `Version` whatever its body.
#[derive(Deserialize)]
struct VersionProbe {
    version: u32,
}

/// Parses a board file. The version is checked before the rest of the body.
pub fn parse_board(bytes: &[u8]) -> Result<BoardSnapshot, BoardError> {
    let probe: VersionProbe =
        serde_json::from_slice(bytes).map_err(|e| BoardError::Json(e.to_string()))?;
    if probe.version != BOARD_VERSION {
        return Err(BoardError::Version(probe.version));
    }
    serde_json::from_slice(bytes).map_err(|e| BoardError::Json(e.to_string()))
}

/// The icon for a stage cell.
pub fn stage_status(s: Stage) -> Status {
    match s {
        Stage::Done => Status::Done,
        Stage::Run => Status::Working,
        Stage::Wait => Status::Waiting,
        Stage::None | Stage::Unknown => Status::NotStarted,
        Stage::Ask => Status::Decide,
        Stage::Todo => Status::Do,
        Stage::Block => Status::Blocked,
    }
}

/// The icon for a needs-you item. A resolved item is `Done` whatever its kind.
pub fn item_status(it: &NeedItem) -> Status {
    if !it.open {
        return Status::Done;
    }
    match it.kind {
        ItemKind::Ask => Status::Decide,
        ItemKind::Do => Status::Do,
        ItemKind::Know | ItemKind::Other => Status::Fyi,
    }
}

/// The kind word for a needs-you item. A resolved item reads `done` whatever its kind.
pub fn item_kind_label(it: &NeedItem) -> &'static str {
    if !it.open {
        return "done";
    }
    match it.kind {
        ItemKind::Ask => "decide",
        ItemKind::Do => "do",
        ItemKind::Know | ItemKind::Other => "fyi",
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    const FIXTURE: &str = include_str!("board_fixture_v1.json");

    fn fixture() -> BoardSnapshot {
        parse_board(FIXTURE.as_bytes()).unwrap()
    }

    fn row(json: &str) -> WorkRow {
        serde_json::from_str(json).unwrap()
    }

    fn item(json: &str) -> NeedItem {
        serde_json::from_str(json).unwrap()
    }

    #[test]
    fn fixture_parses_with_its_header() {
        let board = fixture();
        assert_eq!(board.version, 1);
        assert_eq!(board.session, "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f");
        assert_eq!(board.rev, 42);
        assert_eq!(board.summary, "1 of 2 workers busy · 3 need you");
        assert!(!board.wrap_up);
        assert_eq!(board.need_count, 3);
        assert!(board.clear_rows);
        assert!(board.clear_items);
        assert_eq!(
            board.context,
            Some(ContextUse {
                tokens: 120_000,
                limit: 300_000
            })
        );
    }

    #[test]
    fn fixture_keeps_row_and_item_order() {
        let board = fixture();
        let rows: Vec<&str> = board.rows.iter().map(|r| r.key.as_str()).collect();
        assert_eq!(rows, ["owner/name#640", "owner/name#641", "owner/name#642"]);
        let items: Vec<&str> = board.items.iter().map(|i| i.key.as_str()).collect();
        assert_eq!(items, ["T12", "D7", "N4", "D6"]);
    }

    #[test]
    fn fixture_working_row_reads_its_who_and_stages() {
        let board = fixture();
        let working = &board.rows[0];
        assert_eq!(working.title, "sort bags");
        assert_eq!(working.who.text, "b-1151 5m");
        assert_eq!(working.who.kind, WhoKind::Text);
        assert_eq!(working.stage(0), Stage::Done);
        assert_eq!(working.stage(1), Stage::Run);
        assert!(!working.finished);
        assert_eq!(working.pane_id.as_deref(), Some("w1:p4"));
        assert_eq!(working.report_by.as_deref(), Some("b-1151"));
    }

    #[test]
    fn fixture_you_row_has_a_todo_stage_and_you_who() {
        let board = fixture();
        let you = &board.rows[1];
        assert_eq!(you.who.kind, WhoKind::You);
        assert_eq!(you.who.text, "your test");
        assert_eq!(you.stage(4), Stage::Todo);
        assert_eq!(you.item.as_deref(), Some("T12"));
    }

    #[test]
    fn fixture_joined_row_has_no_stages() {
        let board = fixture();
        let joined = &board.rows[2];
        assert_eq!(joined.joined.as_deref(), Some("#641"));
        assert!(joined.finished);
        assert!(joined.stages.is_empty());
        assert_eq!(joined.stage(0), Stage::None);
    }

    #[test]
    fn stage_past_the_row_reads_none() {
        let board = fixture();
        assert_eq!(board.rows[0].stages.len(), 5);
        assert_eq!(board.rows[0].stage(7), Stage::None);
    }

    #[test]
    fn unknown_stage_string_reads_unknown_and_draws_not_started() {
        let r = row(r#"{"key":"k","stages":["done","bogus"]}"#);
        assert_eq!(r.stage(1), Stage::Unknown);
        assert_eq!(stage_status(r.stage(1)), Status::NotStarted);
    }

    #[test]
    fn stage_strings_parse_to_their_variants() {
        let r = row(r#"{"key":"k","stages":["done","run","wait","none","ask","todo","block"]}"#);
        let got: Vec<Stage> = (0..7).map(|i| r.stage(i)).collect();
        assert_eq!(
            got,
            [
                Stage::Done,
                Stage::Run,
                Stage::Wait,
                Stage::None,
                Stage::Ask,
                Stage::Todo,
                Stage::Block,
            ]
        );
    }

    #[test]
    fn unknown_who_kind_reads_other() {
        let r = row(r#"{"key":"k","who":{"text":"x","kind":"odd"}}"#);
        assert_eq!(r.who.kind, WhoKind::Other);
        assert_eq!(r.who.text, "x");
    }

    #[test]
    fn unknown_item_kind_reads_other_and_draws_as_fyi() {
        let it = item(r#"{"key":"N9","kind":"odd"}"#);
        assert_eq!(it.kind, ItemKind::Other);
        assert_eq!(item_status(&it), Status::Fyi);
        assert_eq!(item_kind_label(&it), "fyi");
    }

    #[test]
    fn missing_optional_fields_take_their_defaults() {
        let r = row(r#"{"key":"k"}"#);
        assert_eq!(r.label, "");
        assert_eq!(r.title, "");
        assert!(r.stages.is_empty());
        assert_eq!(r.who, Who::default());
        assert_eq!(r.joined, None);
        assert!(!r.finished);
        assert_eq!(r.report, None);

        let it = item(r#"{"key":"D1","kind":"ask"}"#);
        assert!(it.open);
        assert_eq!(it.title, "");
        assert!(it.options.is_empty());
        assert_eq!(it.recommended, None);
    }

    #[test]
    fn unknown_top_level_and_row_fields_are_ignored() {
        let bytes = br#"{
            "version": 1,
            "session": "s1",
            "rev": 7,
            "future": {"x": [1, 2]},
            "rows": [{"key": "k", "extra": true}]
        }"#;
        let board = parse_board(bytes).unwrap();
        assert_eq!(board.rev, 7);
        assert_eq!(board.rows.len(), 1);
        assert_eq!(board.rows[0].key, "k");
    }

    #[test]
    fn version_two_is_a_version_error_whatever_the_body() {
        let bytes = br#"{"version": 2, "rows": "not a list"}"#;
        let err = parse_board(bytes).unwrap_err();
        assert_eq!(err, BoardError::Version(2));
        assert_eq!(err.line(), "board v2 · this contr reads v1");
    }

    #[test]
    fn missing_version_is_a_json_error() {
        let bytes = br#"{"session": "s1", "rev": 7}"#;
        assert!(matches!(parse_board(bytes), Err(BoardError::Json(_))));
    }

    #[test]
    fn a_truncated_board_is_a_json_error() {
        let cut = &FIXTURE.as_bytes()[..FIXTURE.len() / 2];
        assert!(matches!(parse_board(cut), Err(BoardError::Json(_))));
    }

    #[test]
    fn non_json_is_a_json_error() {
        let err = parse_board(b"not json").unwrap_err();
        assert!(matches!(err, BoardError::Json(_)));
        assert!(err.line().starts_with("board unreadable: "));
    }

    #[test]
    fn json_error_line_cuts_the_message_to_sixty_chars() {
        let long = BoardError::Json("x".repeat(100));
        assert_eq!(long.line(), format!("board unreadable: {}", "x".repeat(60)));
        let wide = BoardError::Json("é".repeat(100));
        assert_eq!(wide.line(), format!("board unreadable: {}", "é".repeat(60)));
    }

    #[test]
    fn bridge_token_v1_shape_parses() {
        assert_eq!(
            parse_bridge_token("1 abc-DEF_9 42"),
            Some(BridgeRef {
                session: "abc-DEF_9".to_owned(),
                rev: 42,
            })
        );
    }

    #[test]
    fn bridge_token_session_of_sixty_four_chars_parses() {
        let session = "a".repeat(64);
        let token = format!("1 {session} 0");
        assert_eq!(parse_bridge_token(&token).map(|b| b.session), Some(session));
    }

    #[test]
    fn bridge_token_rejects_bad_shapes_versions_sessions_and_revs() {
        let bad = [
            "2 abc 1",
            "1 ../x 1",
            "1 a/b 1",
            "1 abc",
            "1 abc -1",
            "1 abc +1",
            "1 abc 1 extra",
            "1  abc 1",
            "1 abc 1 ",
            "1 abc ",
            " 1 abc 1",
            "1 abc 18446744073709551616",
            "",
        ];
        for token in bad {
            assert_eq!(parse_bridge_token(token), None, "token {token:?}");
        }
        let long = format!("1 {} 1", "a".repeat(65));
        assert_eq!(parse_bridge_token(&long), None);
    }

    #[test]
    fn board_path_joins_the_dir_and_the_session_json() {
        assert_eq!(
            board_path(Path::new("dir"), "s1"),
            PathBuf::from("dir").join("s1.json")
        );
    }

    #[test]
    fn every_stage_maps_to_its_status() {
        let table = [
            (Stage::Done, Status::Done),
            (Stage::Run, Status::Working),
            (Stage::Wait, Status::Waiting),
            (Stage::None, Status::NotStarted),
            (Stage::Ask, Status::Decide),
            (Stage::Todo, Status::Do),
            (Stage::Block, Status::Blocked),
            (Stage::Unknown, Status::NotStarted),
        ];
        for (stage, status) in table {
            assert_eq!(stage_status(stage), status, "stage {stage:?}");
        }
    }

    #[test]
    fn open_items_map_to_their_status_and_label() {
        let ask = item(r#"{"key":"D7","kind":"ask"}"#);
        assert_eq!(item_status(&ask), Status::Decide);
        assert_eq!(item_kind_label(&ask), "decide");

        let todo = item(r#"{"key":"T12","kind":"do"}"#);
        assert_eq!(item_status(&todo), Status::Do);
        assert_eq!(item_kind_label(&todo), "do");

        let know = item(r#"{"key":"N4","kind":"know"}"#);
        assert_eq!(item_status(&know), Status::Fyi);
        assert_eq!(item_kind_label(&know), "fyi");
    }

    #[test]
    fn resolved_item_maps_to_done_whatever_its_kind() {
        let resolved = item(r#"{"key":"D6","kind":"ask","open":false,"answer":"keep it"}"#);
        assert_eq!(item_status(&resolved), Status::Done);
        assert_eq!(item_kind_label(&resolved), "done");
    }

    #[test]
    fn fixture_reads_its_agents() {
        let board = fixture();
        assert_eq!(board.agents.len(), 2);
        assert_eq!(board.agents[0].pane_id, "w1:p4");
        assert_eq!(board.agents[1].model, None);
    }

    #[test]
    fn a_board_without_agents_reads_none() {
        let board = parse_board(br#"{"version": 1, "session": "s", "rev": 1}"#).unwrap();
        assert!(board.agents.is_empty());
        assert!(agent_meta(&board).is_empty());
    }

    #[test]
    fn an_agent_without_a_pane_id_is_dropped_and_the_others_stay() {
        let board = parse_board(
            br#"{"version": 1, "session": "s", "rev": 1, "agents": [
                {"name": "b-1"},
                {"pane_id": "p1", "name": "kept"}
            ]}"#,
        )
        .unwrap();
        assert_eq!(board.agents.len(), 1);
        assert_eq!(board.agents[0].pane_id, "p1");
        assert_eq!(board.agents[0].name.as_deref(), Some("kept"));
    }

    #[test]
    fn an_agent_with_a_null_pane_id_is_dropped() {
        let board = parse_board(
            br#"{"version": 1, "session": "s", "rev": 1, "agents": [
                {"pane_id": null, "name": "b-1"},
                {"pane_id": "p1"}
            ]}"#,
        )
        .unwrap();
        assert_eq!(board.agents.len(), 1);
        assert_eq!(board.agents[0].pane_id, "p1");
    }

    #[test]
    fn wrong_typed_agent_entries_are_dropped() {
        let board = parse_board(
            br#"{"version": 1, "session": "s", "rev": 1, "agents": [
                {"pane_id": "p0", "name": 5},
                {"pane_id": "p1", "model": ["haiku"]},
                "not an object",
                {"pane_id": "p2", "role": "builder"}
            ]}"#,
        )
        .unwrap();
        assert_eq!(board.agents.len(), 1);
        assert_eq!(board.agents[0].pane_id, "p2");
        assert_eq!(board.agents[0].role.as_deref(), Some("builder"));
    }

    #[test]
    fn rows_and_items_still_read_when_an_agent_is_bad() {
        let board = parse_board(
            br##"{"version": 1, "session": "s", "rev": 1, "summary": "2 open", "need_count": 1,
                "rows": [{"key": "k", "label": "#7", "pane_id": "w1:p9"}],
                "items": [{"key": "D1", "kind": "ask", "open": true, "title": "Pick one"}],
                "agents": [{"name": "b-1"}, {"pane_id": 3}]}"##,
        )
        .unwrap();
        assert!(board.agents.is_empty());
        assert_eq!(board.summary, "2 open");
        assert_eq!(board.need_count, 1);
        assert_eq!(board.rows.len(), 1);
        assert_eq!(board.rows[0].label, "#7");
        assert_eq!(board.items.len(), 1);
        assert_eq!(board.items[0].title, "Pick one");
        // The row still links its pane, with no agent entry.
        assert_eq!(
            agent_meta(&board).get("w1:p9"),
            Some(&AgentMeta {
                link: Some("#7".into()),
                ..Default::default()
            })
        );
    }

    #[test]
    fn agents_as_a_string_reads_as_an_empty_list() {
        let board = parse_board(
            br##"{"version": 1, "session": "s", "rev": 1, "summary": "ok", "agents": "x",
                "rows": [{"key": "k", "label": "#7"}]}"##,
        )
        .unwrap();
        assert!(board.agents.is_empty());
        assert_eq!(board.summary, "ok");
        assert_eq!(board.rows.len(), 1);
    }

    #[test]
    fn agents_as_null_reads_as_an_empty_list() {
        let board = parse_board(
            br##"{"version": 1, "session": "s", "rev": 1, "summary": "ok", "agents": null,
                "rows": [{"key": "k", "label": "#7"}]}"##,
        )
        .unwrap();
        assert!(board.agents.is_empty());
        assert_eq!(board.summary, "ok");
        assert_eq!(board.rows.len(), 1);
    }

    #[test]
    fn null_agent_fields_read_as_not_known() {
        let board = parse_board(
            br#"{"version": 1, "session": "s", "rev": 1, "agents": [
                {"pane_id": "p1", "name": null, "model": "  ", "role": null}
            ]}"#,
        )
        .unwrap();
        let meta = agent_meta(&board);
        // A blank string is kept as given; the agents list treats it as missing.
        assert_eq!(
            meta.get("p1"),
            Some(&AgentMeta {
                name: None,
                model: Some("  ".into()),
                role: None,
                link: None,
            })
        );
    }

    #[test]
    fn agent_meta_takes_name_model_and_role_from_agents() {
        let meta = agent_meta(&fixture());
        assert_eq!(meta.len(), 2);
        assert_eq!(
            meta.get("w1:p4"),
            Some(&AgentMeta {
                name: Some("b-1151".into()),
                model: Some("haiku 5.5".into()),
                role: Some("builder".into()),
                link: Some("#640".into()),
            })
        );
        assert_eq!(
            meta.get("w1:p2"),
            Some(&AgentMeta {
                name: Some("r-1152".into()),
                model: None,
                role: Some("reviewer".into()),
                link: Some("#641".into()),
            })
        );
    }

    #[test]
    fn agent_meta_links_the_open_row_of_the_pane() {
        let board = parse_board(
            br##"{"version": 1, "session": "s", "rev": 1, "rows": [
                {"key": "k", "label": "#7", "pane_id": "w1:p9"},
                {"key": "j", "label": "#8"}
            ]}"##,
        )
        .unwrap();
        let meta = agent_meta(&board);
        assert_eq!(
            meta.get("w1:p9"),
            Some(&AgentMeta {
                link: Some("#7".into()),
                ..Default::default()
            })
        );
        // A row without a pane id gives no entry.
        assert_eq!(meta.len(), 1);
    }

    #[test]
    fn agent_meta_skips_finished_and_joined_rows() {
        let board = parse_board(
            br##"{"version": 1, "session": "s", "rev": 1, "rows": [
                {"key": "a", "label": "#1", "pane_id": "p1", "finished": true},
                {"key": "b", "label": "#2", "pane_id": "p1", "joined": "#9"},
                {"key": "c", "label": "#3", "pane_id": "p1"},
                {"key": "d", "label": "#5", "pane_id": "p1"},
                {"key": "e", "label": "#4", "pane_id": "p2", "finished": true}
            ]}"##,
        )
        .unwrap();
        let meta = agent_meta(&board);
        // The first open row in slot order wins the link.
        assert_eq!(
            meta.get("p1"),
            Some(&AgentMeta {
                link: Some("#3".into()),
                ..Default::default()
            })
        );
        assert_eq!(meta.get("p2"), None);
    }

    #[test]
    fn agent_meta_first_entry_for_a_pane_wins() {
        let board = parse_board(
            br#"{"version": 1, "session": "s", "rev": 1, "agents": [
                {"pane_id": "p1", "name": "first"},
                {"pane_id": "p1", "name": "second", "role": "planner"}
            ]}"#,
        )
        .unwrap();
        let meta = agent_meta(&board);
        assert_eq!(
            meta.get("p1"),
            Some(&AgentMeta {
                name: Some("first".into()),
                ..Default::default()
            })
        );
    }

    #[test]
    fn fixture_declares_the_action_fields() {
        let board = fixture();
        assert_eq!(board.actions, 1);
        assert_eq!(board.acted, 0);
        assert!(board.failed.is_empty());
    }

    #[test]
    fn old_board_without_action_fields_reads_them_as_defaults() {
        let board = parse_board(br#"{"version": 1, "session": "s1", "rev": 7}"#).unwrap();
        assert_eq!(board.actions, 0);
        assert_eq!(board.acted, 0);
        assert!(board.failed.is_empty());
    }

    #[test]
    fn failed_entries_read_their_seq_key_and_text() {
        let bytes = br#"{
            "version": 1, "session": "s1", "rev": 7, "acted": 16,
            "failed": [
                {"seq": 15, "key": "D5", "text": "D5 is not open"},
                {"seq": 16}
            ]
        }"#;
        let board = parse_board(bytes).unwrap();
        assert_eq!(board.acted, 16);
        assert_eq!(
            board.failed,
            [
                ActionFailure {
                    seq: 15,
                    key: Some("D5".to_owned()),
                    text: "D5 is not open".to_owned(),
                },
                ActionFailure {
                    seq: 16,
                    key: None,
                    text: String::new(),
                },
            ]
        );
    }

    #[test]
    fn failed_entry_without_seq_is_a_json_error() {
        let bytes = br#"{"version": 1, "session": "s1", "rev": 7, "failed": [{"key": "D5"}]}"#;
        assert!(matches!(parse_board(bytes), Err(BoardError::Json(_))));
    }
}
