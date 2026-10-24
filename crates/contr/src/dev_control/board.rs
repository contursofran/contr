//! The board panel's part of the dev state, and its clicks. The `board` section reads the panel's
//! model: the tab, the header texts, the pick, the work rows, the needs-you rows, the answer card
//! and the Report tab. A click on a board control first focuses the panel, as a real mouse down
//! does. Then it calls the same path that the control's own handler calls.

use crate::board_card::{BoardPick, names_on_board};
use crate::board_model::{
    BoardSnapshot, NeedItem, STAGE_HEADS, WhoKind, WorkRow, item_kind_label, stage_status,
};
use crate::board_panel::{PanelTab, empty_text, summary_text, wrap_text};
use crate::board_report::ReportView;
use crate::dev_protocol::{DevError, code};
use crate::frame::FrameParts;
use gpui::{App, ClickEvent, Entity, KeyboardClickEvent, Window};
use serde_json::{Value, json};

/// The fixed controls of the board panel, in the order of the Contract.
const CONTROLS: [&str; 7] = [
    "board-tab-board",
    "board-tab-report",
    "board-wrap",
    "work-clear",
    "needs-clear",
    "report-back",
    "report-open",
];

/// The card buttons whose id does not depend on their position. An option button is
/// `card-opt-<position>`.
const CARD_BUTTONS: [&str; 9] = [
    "card-chat",
    "card-done",
    "card-link",
    "card-dismiss",
    "card-undo",
    "card-next",
    "card-focus",
    "card-report",
    "card-pr",
];

/// The `board` section of the state. Without a snapshot the header shows the line, and the work
/// and needs-you lists are empty.
pub(crate) fn state(parts: &FrameParts, _window: &Window, cx: &mut App) -> Value {
    let tab = parts.board_panel.read(cx).tab();
    let can_act = parts.board_panel.read(cx).can_act(cx);
    let (bridge, problem, snapshot) = {
        let panel = parts.board_panel.read(cx);
        let shown = panel.board().read(cx);
        (
            shown.bridge.is_some(),
            shown.problem.clone(),
            shown.snapshot.clone(),
        )
    };
    let problem = problem.as_deref();
    let Some(snapshot) = snapshot.as_deref() else {
        let line = empty_text(bridge, problem);
        return json!({
            "tab": tab_name(tab),
            "line": line,
            "summary": null,
            "wrap": null,
            "can_act": can_act,
            "pick": null,
            "work": [],
            "needs": {"count": 0, "rows": []},
            "card": Value::Null,
            "report": Value::Null,
        });
    };
    let (pick, items) = {
        let panel = parts.board_panel.read(cx);
        (panel.shown_pick(snapshot), panel.shown_items(snapshot, cx))
    };
    let card = match tab {
        PanelTab::Board => card_state(parts, cx),
        PanelTab::Report => Value::Null,
    };
    let report = match tab {
        PanelTab::Report => report_state(parts, cx),
        PanelTab::Board => Value::Null,
    };
    json!({
        "tab": tab_name(tab),
        "line": null,
        "summary": summary_text(&snapshot.summary, problem),
        "wrap": wrap_text(snapshot.wrap_up),
        "can_act": can_act,
        "pick": pick_value(pick.as_ref()),
        "work": snapshot
            .rows
            .iter()
            .map(|row| work_row(row, pick.as_ref()))
            .collect::<Vec<_>>(),
        "needs": {
            "count": snapshot.need_count,
            "rows": items
                .iter()
                .map(|item| need_row(item, pick.as_ref()))
                .collect::<Vec<_>>(),
        },
        "card": card,
        "report": report,
    })
}

/// The tab's name in the state.
fn tab_name(tab: PanelTab) -> &'static str {
    match tab {
        PanelTab::Board => "board",
        PanelTab::Report => "report",
    }
}

/// The `card` section: what the card draws, with each drawn button, its label and whether it is
/// live. The card is drawn on the Board tab only, so this is read then.
fn card_state(parts: &FrameParts, cx: &mut App) -> Value {
    let card = parts.board_panel.update(cx, |panel, cx| panel.dev_card(cx));
    let buttons: Vec<Value> = card
        .buttons
        .into_iter()
        .map(|button| {
            json!({
                "id": button.id,
                "label": button.label,
                "live": button.click.is_some(),
            })
        })
        .collect();
    json!({
        "face": card.face,
        "key": card.key,
        "label": card.label,
        "buttons": buttons,
    })
}

/// The `report` section: what the Report tab draws for the row it follows. The tab is shown only
/// then, so this is read on it.
fn report_state(parts: &FrameParts, cx: &App) -> Value {
    let panel = parts.board_panel.read(cx);
    let report = panel.report().read(cx).dev_report();
    json!({
        "row": report.row,
        "title": report.title,
        "by": report.by,
        "problem": report.problem,
        "lines": report.lines,
    })
}

/// The pick as the state shows it: its kind and its key, or `null`.
fn pick_value(pick: Option<&BoardPick>) -> Value {
    match pick {
        Some(BoardPick::Row(key)) => json!({"kind": "row", "key": key}),
        Some(BoardPick::Item(key)) => json!({"kind": "item", "key": key}),
        None => Value::Null,
    }
}

/// One work row, as its section draws it: the label, the title, who holds it, the five stages, the
/// join, and whether it is finished or picked.
fn work_row(row: &WorkRow, pick: Option<&BoardPick>) -> Value {
    let stages: Vec<&str> = (0..STAGE_HEADS.len())
        .map(|index| stage_status(row.stage(index)).slug())
        .collect();
    json!({
        "id": format!("work:{}", row.key),
        "label": row.label,
        "title": row.title,
        "who": row.who.text,
        "who_you": row.who.kind == WhoKind::You,
        "stages": stages,
        "joined": row.joined,
        "finished": row.finished,
        "picked": matches!(pick, Some(BoardPick::Row(key)) if *key == row.key),
    })
}

/// One needs-you row, as its section draws it. `item` is as shown: an item with an answer queued
/// reads as resolved.
fn need_row(item: &NeedItem, pick: Option<&BoardPick>) -> Value {
    json!({
        "id": format!("need:{}", item.key),
        "key": item.key,
        "kind": item_kind_label(item),
        "title": item.title,
        "open": item.open,
        "picked": matches!(pick, Some(BoardPick::Item(key)) if *key == item.key),
    })
}

/// The controls of the board panel, with whether each is live. A control is live when the panel is
/// open and the control has its state: a clear needs the tab it sits on and an action it can take,
/// and the report actions need the Report tab and a report to show.
pub(crate) fn controls(parts: &FrameParts, cx: &App) -> Vec<(&'static str, bool)> {
    CONTROLS
        .iter()
        .map(|&id| (id, live(id, parts, cx)))
        .collect()
}

/// Whether the fixed control `id` is live now. An id that is not fixed is not live.
fn live(id: &str, parts: &FrameParts, cx: &App) -> bool {
    if !parts.frame.read(cx).panel_open() {
        return false;
    }
    let panel = parts.board_panel.read(cx);
    let on_board_tab = panel.tab() == PanelTab::Board;
    let on_report_tab = panel.tab() == PanelTab::Report;
    let acting = panel.can_act(cx);
    let snapshot = shown_snapshot(parts, cx);
    match id {
        "board-tab-board" | "board-tab-report" => true,
        "board-wrap" => acting && snapshot.is_some(),
        "work-clear" => on_board_tab && acting && snapshot.is_some_and(|shown| shown.clear_rows),
        "needs-clear" => on_board_tab && acting && snapshot.is_some_and(|shown| shown.clear_items),
        "report-back" => on_report_tab && snapshot.is_some(),
        "report-open" => on_report_tab && snapshot.is_some() && panel.report().read(cx).can_open(),
        _ => false,
    }
}

/// The snapshot the panel shows, if any.
fn shown_snapshot<'a>(parts: &FrameParts, cx: &'a App) -> Option<&'a BoardSnapshot> {
    parts
        .board_panel
        .read(cx)
        .board()
        .read(cx)
        .snapshot
        .as_deref()
}

/// A click on a board panel control. `None` when `id` is not one of its controls: the fixed ids,
/// the card buttons, `work:<key>` and `need:<key>`.
pub(crate) fn click(
    id: &str,
    parts: &FrameParts,
    window: &mut Window,
    cx: &mut App,
) -> Option<Result<(), DevError>> {
    let owned = CONTROLS.contains(&id) || pick_of(id).is_some() || card_id(id);
    owned.then(|| click_control(id, parts, window, cx))
}

/// Whether `id` names a card button, drawn or not.
fn card_id(id: &str) -> bool {
    CARD_BUTTONS.contains(&id) || id.starts_with("card-opt-")
}

/// A click on an id that `click` owns. A closed panel has no controls on screen, so the click is
/// `disabled`. Otherwise the panel takes the focus first, as a real mouse down does, and then the
/// control runs.
fn click_control(
    id: &str,
    parts: &FrameParts,
    window: &mut Window,
    cx: &mut App,
) -> Result<(), DevError> {
    if !parts.frame.read(cx).panel_open() {
        return Err(DevError::new(code::DISABLED, "the board panel is closed"));
    }
    let focus = parts.board_panel.read(cx).focus_handle().clone();
    window.focus(&focus, cx);
    if let Some(pick) = pick_of(id) {
        return pick_on_board(id, pick, parts, cx);
    }
    if card_id(id) {
        return click_card(id, parts, window, cx);
    }
    if !live(id, parts, cx) {
        return Err(DevError::new(code::DISABLED, format!("{id} is not live")));
    }
    let panel = &parts.board_panel;
    match id {
        "board-tab-board" => panel.update(cx, |panel, cx| panel.open_board_tab(cx)),
        "board-tab-report" => panel.update(cx, |panel, cx| panel.open_report_tab(cx)),
        "board-wrap" => panel.update(cx, |panel, cx| panel.send_wrap(cx)),
        "work-clear" => panel.update(cx, |panel, cx| panel.send_clear(true, cx)),
        "needs-clear" => panel.update(cx, |panel, cx| panel.send_clear(false, cx)),
        "report-back" => report(parts, cx).update(cx, |report, cx| report.back(cx)),
        "report-open" => report(parts, cx).update(cx, |report, cx| report.open_in_editor(cx)),
        _ => return Err(not_on_board(id)),
    }
    Ok(())
}

/// The Report tab's view, as a handle of its own.
fn report(parts: &FrameParts, cx: &App) -> Entity<ReportView> {
    parts.board_panel.read(cx).report().clone()
}

/// A click on a card button: the button's own click, made by the card face, is called with a
/// keyboard click event, as the panel's own `on_click` would be. A button the card draws dim is
/// `disabled`. An id the card does not draw now (another face, or the Report tab) is `unknown_id`.
fn click_card(
    id: &str,
    parts: &FrameParts,
    window: &mut Window,
    cx: &mut App,
) -> Result<(), DevError> {
    let card = parts.board_panel.update(cx, |panel, cx| panel.dev_card(cx));
    let Some(button) = card.buttons.into_iter().find(|button| button.id == id) else {
        return Err(DevError::new(
            code::UNKNOWN_ID,
            format!("{id} is not on the answer card"),
        ));
    };
    let Some(click) = button.click else {
        return Err(DevError::new(code::DISABLED, format!("{id} is not live")));
    };
    click(
        &ClickEvent::Keyboard(KeyboardClickEvent::default()),
        window,
        cx,
    );
    Ok(())
}

/// The pick that a `work:<key>` or `need:<key>` id names. `None` for any other id.
fn pick_of(id: &str) -> Option<BoardPick> {
    if let Some(key) = id.strip_prefix("work:") {
        return Some(BoardPick::Row(key.to_owned()));
    }
    id.strip_prefix("need:")
        .map(|key| BoardPick::Item(key.to_owned()))
}

/// A click on `work:<key>` or `need:<key>`: picks the row or the item, as a click on it does. The
/// pick must name something on the board, and the Board tab must be shown.
fn pick_on_board(
    id: &str,
    pick: BoardPick,
    parts: &FrameParts,
    cx: &mut App,
) -> Result<(), DevError> {
    let on_board = shown_snapshot(parts, cx).is_some_and(|shown| names_on_board(shown, &pick));
    if !on_board {
        return Err(not_on_board(id));
    }
    if parts.board_panel.read(cx).tab() != PanelTab::Board {
        return Err(DevError::new(code::DISABLED, "the Board tab is not shown"));
    }
    parts
        .board_panel
        .update(cx, |panel, cx| panel.set_pick(pick, cx));
    Ok(())
}

/// The error for an id that no control or row of the board has.
fn not_on_board(id: &str) -> DevError {
    DevError::new(code::UNKNOWN_ID, format!("{id} is not on the board"))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::agents_list::FocusFn;
    use crate::board::{GlobalBoard, init_detached};
    use crate::board_actions::{Action, UNDO_MS};
    use crate::board_outbox::BoardOutbox;
    use crate::board_panel::BoardPanel;
    use crate::board_panel::OpenFn;
    use crate::board_panel::test_support::{
        bridge, fixture, ignored_focus, ignored_open, queued, status,
    };
    use crate::dev_control::dispatch::handle;
    use crate::dev_protocol::{Reply, Request};
    use crate::frame::{Frame, parts};
    use crate::selection::Selection;
    use crate::status_bar;
    use crate::title_bar::TitleBarEvent;
    use crate::title_bar::tests::{herdr_in, sample_tree};
    use crate::visibility::Visibility;
    use contr_herdr::LiveHerdr;
    use gpui::{AppContext, Entity, TestAppContext, VisualTestContext, px, size};
    use std::cell::RefCell;
    use std::path::Path;
    use std::rc::Rc;
    use std::sync::Arc;
    use std::time::Duration;

    /// The arguments that a Focus or Open PR call recorded, in order.
    type Calls = Rc<RefCell<Vec<String>>>;

    /// A recorder for a Focus or Open PR call.
    fn calls() -> Calls {
        Rc::new(RefCell::new(Vec::new()))
    }

    /// A Focus call that records its pane in `log`.
    fn focus_into(log: Calls) -> FocusFn {
        Rc::new(move |pane: String, _: &mut App| log.borrow_mut().push(pane))
    }

    /// An Open PR call that records its URL in `log`.
    fn open_into(log: Calls) -> OpenFn {
        Rc::new(move |url: String, _: &mut App| log.borrow_mut().push(url))
    }

    /// A herdr state with the sample tree, so a row's pane is in a tab.
    fn live_with_tree() -> LiveHerdr {
        LiveHerdr {
            tree: Some(Arc::new(sample_tree())),
            ..LiveHerdr::default()
        }
    }

    /// A frame in a window of the reference size, with the board set to `snapshot` on the bridge
    /// pane, or to no board when `snapshot` is `None`. With `queue_dir`, the board takes actions
    /// and writes them there. Without it, the board takes none.
    fn frame_with<'a>(
        cx: &'a mut TestAppContext,
        snapshot: Option<BoardSnapshot>,
        queue_dir: Option<&Path>,
    ) -> (Entity<Frame>, &'a mut VisualTestContext) {
        frame_with_actions(
            cx,
            snapshot,
            queue_dir,
            LiveHerdr::default(),
            ignored_focus(),
            ignored_open(),
        )
    }

    /// `frame_with`, with the herdr `live` state and the card's Focus and Open PR calls given.
    fn frame_with_actions<'a>(
        cx: &'a mut TestAppContext,
        snapshot: Option<BoardSnapshot>,
        queue_dir: Option<&Path>,
        live: LiveHerdr,
        focus: FocusFn,
        open: OpenFn,
    ) -> (Entity<Frame>, &'a mut VisualTestContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        cx.update(BoardPanel::bind_keys);
        herdr_in(cx, live);
        match queue_dir {
            Some(dir) => cx.update(|app| {
                let board = app.new(|cx| crate::board::Board::new(Some(dir.to_path_buf()), cx));
                app.set_global(GlobalBoard(board));
                BoardOutbox::init(app);
            }),
            None => cx.update(init_detached),
        }
        cx.update(|app| status_bar::init_detached(app, None));
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        let has_bridge = snapshot.is_some().then(bridge);
        cx.update(|app| {
            board.update(app, |board, cx| board.set(snapshot, None, has_bridge, cx));
        });
        let (frame, cx) = cx
            .add_window_view(move |window, cx| Frame::with_panel_actions(window, focus, open, cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        (frame, cx)
    }

    /// Sends one request to the window, as the dev control does on the UI thread.
    fn send(cx: &mut VisualTestContext, request: Request) -> Reply {
        cx.update(|window, app| handle(request, window, app))
    }

    /// Clicks `id`, then lets the queue write what the click sent, as the app's background work does.
    fn click_id(cx: &mut VisualTestContext, id: &str) -> Reply {
        let reply = send(cx, Request::Click { id: id.into() });
        cx.run_until_parked();
        reply
    }

    fn ok(reply: Reply) -> Value {
        match reply {
            Reply::Ok(result) => result,
            Reply::Err(error) => panic!("expected a result, got {}: {}", error.code, error.message),
        }
    }

    fn err(reply: Reply) -> DevError {
        match reply {
            Reply::Err(error) => error,
            Reply::Ok(result) => panic!("expected an error, got {result}"),
        }
    }

    /// The `board` section of the state.
    fn board_state(cx: &mut VisualTestContext) -> Value {
        ok(send(cx, Request::State))["board"].clone()
    }

    /// Whether the control `id` is live, as the state lists it.
    fn live_in(cx: &mut VisualTestContext, id: &str) -> Option<Value> {
        ok(send(cx, Request::State))["controls"]
            .as_array()
            .unwrap()
            .iter()
            .find(|control| control["id"] == id)
            .map(|control| control["live"].clone())
    }

    /// The `card` section of the state.
    fn card_state(cx: &mut VisualTestContext) -> Value {
        board_state(cx)["card"].clone()
    }

    /// The card's buttons as `(id, label, live)`, in the order the card draws them.
    fn card_buttons(card: &Value) -> Vec<(&str, &str, bool)> {
        card["buttons"]
            .as_array()
            .unwrap()
            .iter()
            .map(|button| {
                (
                    button["id"].as_str().unwrap(),
                    button["label"].as_str().unwrap(),
                    button["live"].as_bool().unwrap(),
                )
            })
            .collect()
    }

    /// Moves the test clock by `by`, after what is due has run, and runs what that makes due.
    fn advance(cx: &mut VisualTestContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    /// Closes the board panel, as the title bar's panel toggle does.
    fn close_panel(cx: &mut VisualTestContext, frame: &Entity<Frame>) {
        let title_bar = cx.update(|_, app| parts(frame, app).title_bar);
        title_bar.update(cx, |_, cx| cx.emit(TitleBarEvent::TogglePanel));
        cx.run_until_parked();
    }

    #[gpui::test]
    fn the_board_section_shows_the_header_and_the_sections(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let board = board_state(cx);
        assert_eq!(board["tab"], "board");
        assert_eq!(board["line"], Value::Null);
        assert_eq!(board["summary"], "1 of 2 workers busy · 3 need you");
        assert_eq!(board["wrap"], "wrap up");
        assert_eq!(board["can_act"], false, "no queue, no actions");
        assert_eq!(board["needs"]["count"], 3);
        // The pick falls back to the first open item, and the card shows it.
        assert_eq!(board["pick"], json!({"kind": "item", "key": "T12"}));
        assert_eq!(board["card"]["face"], "item");
        assert_eq!(board["card"]["key"], "T12");
        assert_eq!(board["report"], Value::Null, "the Report tab is not shown");
    }

    #[gpui::test]
    fn the_work_rows_show_their_text_stages_and_join(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let work = board_state(cx)["work"].clone();
        assert_eq!(work.as_array().unwrap().len(), 3);
        assert_eq!(
            work[0],
            json!({
                "id": "work:owner/name#640",
                "label": "#640",
                "title": "sort bags",
                "who": "b-1151 5m",
                "who_you": false,
                "stages": ["done", "working", "not-started", "not-started", "not-started"],
                "joined": null,
                "finished": false,
                "picked": false,
            })
        );
        assert_eq!(work[1]["who_you"], true);
        assert_eq!(
            work[1]["stages"],
            json!(["done", "done", "done", "done", "do"])
        );
        assert_eq!(work[2]["joined"], "#641");
        assert_eq!(work[2]["finished"], true);
    }

    #[gpui::test]
    fn the_needs_rows_show_their_kind_and_state(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let rows = board_state(cx)["needs"]["rows"].clone();
        let kinds: Vec<(&str, &str, bool)> = rows
            .as_array()
            .unwrap()
            .iter()
            .map(|row| {
                (
                    row["id"].as_str().unwrap(),
                    row["kind"].as_str().unwrap(),
                    row["open"].as_bool().unwrap(),
                )
            })
            .collect();
        assert_eq!(
            kinds,
            vec![
                ("need:T12", "do", true),
                ("need:D7", "decide", true),
                ("need:N4", "fyi", true),
                ("need:D6", "done", false),
            ]
        );
        assert_eq!(rows[0]["title"], "Test the bag drag");
        assert_eq!(
            rows[0]["picked"], true,
            "the pick falls on the first open item"
        );
    }

    #[gpui::test]
    fn the_board_state_has_no_snapshot_line_without_a_board(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, None, None);
        let board = board_state(cx);
        assert_eq!(board["line"], "no coordinator board");
        assert_eq!(board["summary"], Value::Null);
        assert_eq!(board["wrap"], Value::Null);
        assert_eq!(board["pick"], Value::Null);
        assert_eq!(board["work"], json!([]));
        assert_eq!(board["needs"], json!({"count": 0, "rows": []}));
        assert_eq!(live_in(cx, "board-wrap"), Some(json!(false)));
    }

    #[gpui::test]
    fn a_click_on_a_work_row_picks_it(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let state = ok(click_id(cx, "work:owner/name#640"));
        assert_eq!(
            state["board"]["pick"],
            json!({"kind": "row", "key": "owner/name#640"})
        );
        assert_eq!(state["board"]["work"][0]["picked"], true);
        assert_eq!(state["board"]["needs"]["rows"][0]["picked"], false);
        assert_eq!(state["focus"], "panel", "the click focuses the panel first");
    }

    #[gpui::test]
    fn a_click_on_a_needs_row_picks_the_item(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let state = ok(click_id(cx, "need:D7"));
        assert_eq!(state["board"]["pick"], json!({"kind": "item", "key": "D7"}));
        assert_eq!(state["board"]["needs"]["rows"][1]["picked"], true);
        assert_eq!(state["board"]["needs"]["rows"][0]["picked"], false);
    }

    #[gpui::test]
    fn an_unknown_row_or_item_is_unknown_id(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        assert_eq!(err(click_id(cx, "work:nope")).code, code::UNKNOWN_ID);
        assert_eq!(err(click_id(cx, "need:Z9")).code, code::UNKNOWN_ID);
    }

    #[gpui::test]
    fn a_row_or_item_without_a_board_is_unknown_id(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, None, None);
        assert_eq!(
            err(click_id(cx, "work:owner/name#640")).code,
            code::UNKNOWN_ID
        );
        assert_eq!(err(click_id(cx, "need:T12")).code, code::UNKNOWN_ID);
    }

    #[gpui::test]
    fn the_report_tab_switches_and_the_board_tab_brings_the_pick_back(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        ok(click_id(cx, "work:owner/name#640"));
        let state = ok(click_id(cx, "board-tab-report"));
        assert_eq!(state["board"]["tab"], "report");
        let state = ok(click_id(cx, "board-tab-board"));
        assert_eq!(state["board"]["tab"], "board");
        assert_eq!(
            state["board"]["pick"],
            json!({"kind": "row", "key": "owner/name#640"}),
            "the pick is kept"
        );
    }

    #[gpui::test]
    fn the_rows_are_disabled_on_the_report_tab(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        ok(click_id(cx, "board-tab-report"));
        assert_eq!(
            err(click_id(cx, "work:owner/name#640")).code,
            code::DISABLED
        );
        assert_eq!(err(click_id(cx, "need:T12")).code, code::DISABLED);
        assert_eq!(live_in(cx, "work-clear"), Some(json!(false)));
        assert_eq!(live_in(cx, "needs-clear"), Some(json!(false)));
        assert_eq!(live_in(cx, "board-tab-report"), Some(json!(true)));
    }

    #[gpui::test]
    fn the_board_controls_are_disabled_while_the_panel_is_closed(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (frame, cx) = frame_with(cx, Some(fixture()), Some(dir.path()));
        close_panel(cx, &frame);
        for id in [
            "board-tab-board",
            "board-wrap",
            "work:owner/name#640",
            "need:T12",
        ] {
            assert_eq!(err(click_id(cx, id)).code, code::DISABLED, "{id}");
        }
        assert_eq!(live_in(cx, "board-tab-board"), Some(json!(false)));
        assert_eq!(queued(dir.path()), None, "nothing is sent");
    }

    #[gpui::test]
    fn the_wrap_click_queues_wrap_up_and_shows_the_status(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_frame, cx) = frame_with(cx, Some(fixture()), Some(dir.path()));
        assert_eq!(live_in(cx, "board-wrap"), Some(json!(true)));
        let state = ok(click_id(cx, "board-wrap"));
        assert_eq!(state["focus"], "panel");
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, Action::WrapUp { on: true })])
        );
        assert_eq!(status(cx).as_deref(), Some("wrap up sent"));
    }

    #[gpui::test]
    fn the_wrap_click_on_a_wrapping_board_keeps_going(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.wrap_up = true;
        let (_frame, cx) = frame_with(cx, Some(snapshot), Some(dir.path()));
        assert_eq!(board_state(cx)["wrap"], "keep going");
        ok(click_id(cx, "board-wrap"));
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, Action::WrapUp { on: false })])
        );
        assert_eq!(status(cx).as_deref(), Some("keep going sent"));
    }

    #[gpui::test]
    fn the_wrap_click_is_disabled_without_a_queue_and_still_focuses_the_panel(
        cx: &mut TestAppContext,
    ) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        assert_eq!(live_in(cx, "board-wrap"), Some(json!(false)));
        assert_eq!(err(click_id(cx, "board-wrap")).code, code::DISABLED);
        // A real mouse down on a dim control still focuses the panel.
        assert_eq!(ok(send(cx, Request::State))["focus"], "panel");
    }

    #[gpui::test]
    fn the_clears_queue_their_actions_when_the_board_has_resolved_items(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_frame, cx) = frame_with(cx, Some(fixture()), Some(dir.path()));
        ok(click_id(cx, "needs-clear"));
        assert_eq!(queued(dir.path()), Some(vec![(1, Action::ClearItems)]));
        assert_eq!(status(cx).as_deref(), Some("clear done sent"));
        ok(click_id(cx, "work-clear"));
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, Action::ClearItems), (2, Action::ClearRows)])
        );
    }

    #[gpui::test]
    fn a_clear_with_nothing_to_clear_is_disabled(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.clear_rows = false;
        snapshot.clear_items = false;
        let (_frame, cx) = frame_with(cx, Some(snapshot), Some(dir.path()));
        assert_eq!(live_in(cx, "work-clear"), Some(json!(false)));
        assert_eq!(live_in(cx, "needs-clear"), Some(json!(false)));
        assert_eq!(err(click_id(cx, "work-clear")).code, code::DISABLED);
        assert_eq!(err(click_id(cx, "needs-clear")).code, code::DISABLED);
        assert_eq!(queued(dir.path()), None, "nothing is sent");
    }

    #[gpui::test]
    fn the_problem_follows_the_summary(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| {
                board.set(
                    Some(fixture()),
                    Some("stale read".into()),
                    Some(bridge()),
                    cx,
                )
            })
        });
        cx.run_until_parked();
        assert_eq!(
            board_state(cx)["summary"],
            "1 of 2 workers busy · 3 need you · stale read"
        );
    }

    #[gpui::test]
    fn the_default_card_shows_the_first_open_item(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let card = card_state(cx);
        assert_eq!(card["face"], "item");
        assert_eq!(card["key"], "T12");
        assert_eq!(card["label"], "T12 Test the bag drag, do");
        assert_eq!(
            card_buttons(&card),
            vec![
                ("card-done", "Done", false),
                ("card-link", "Open link", true),
                ("card-next", "Next: D7", true),
            ],
            "the board takes no actions: Done is dim, and the link and Next still work"
        );
    }

    #[gpui::test]
    fn the_decision_card_puts_the_recommended_option_first(cx: &mut TestAppContext) {
        let mut snapshot = fixture();
        snapshot
            .items
            .iter_mut()
            .find(|item| item.key == "D7")
            .unwrap()
            .recommended = Some(1);
        let (_frame, cx) = frame_with(cx, Some(snapshot), None);
        ok(click_id(cx, "need:D7"));

        let card = card_state(cx);
        assert_eq!(card["face"], "item");
        assert_eq!(card["key"], "D7");
        assert_eq!(
            card["label"],
            "D7 Row layout, decide, options two lines per row, one line per row"
        );
        assert_eq!(
            card_buttons(&card),
            vec![
                ("card-opt-0", "two lines per row", false),
                ("card-opt-1", "one line per row", false),
                ("card-chat", "Answer in chat", false),
                ("card-next", "Next: N4", true),
            ]
        );
    }

    #[gpui::test]
    fn an_option_click_writes_the_answer_and_the_card_shows_it_resolved(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_frame, cx) = frame_with(cx, Some(fixture()), Some(dir.path()));
        ok(click_id(cx, "need:D7"));
        let state = ok(click_id(cx, "card-opt-0"));
        let card = &state["board"]["card"];
        assert_eq!(card["face"], "item");
        assert_eq!(card["label"], "D7 Row layout, done");
        assert_eq!(
            card_buttons(card),
            vec![("card-undo", "undo", true), ("card-next", "Next: N4", true)],
            "the answer is held, and can be undone"
        );
        assert_eq!(state["board"]["needs"]["rows"][1]["open"], false);
        assert_eq!(queued(dir.path()), None, "a held answer is not written yet");

        advance(cx, Duration::from_millis(UNDO_MS));
        let answer = Action::Answer {
            key: "D7".to_owned(),
            option: 0,
            label: "one line per row".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, answer)]));
    }

    #[gpui::test]
    fn a_card_undo_takes_the_held_answer_back(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_frame, cx) = frame_with(cx, Some(fixture()), Some(dir.path()));
        ok(click_id(cx, "need:D7"));
        ok(click_id(cx, "card-opt-0"));
        let state = ok(click_id(cx, "card-undo"));

        let card = &state["board"]["card"];
        assert_eq!(
            card_buttons(card)[0],
            ("card-opt-0", "one line per row", true),
            "the options are back"
        );
        assert_eq!(state["board"]["needs"]["rows"][1]["open"], true);
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(
            queued(dir.path()),
            None,
            "the undone answer is never written"
        );
    }

    #[gpui::test]
    fn card_done_writes_the_to_do(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_frame, cx) = frame_with(cx, Some(fixture()), Some(dir.path()));
        let state = ok(click_id(cx, "card-done"));
        assert_eq!(
            state["board"]["card"]["label"],
            "T12 Test the bag drag, done"
        );
        advance(cx, Duration::from_millis(UNDO_MS));
        let done = Action::Done {
            key: "T12".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, done)]));
    }

    #[gpui::test]
    fn card_next_moves_the_pick_to_the_next_open_item(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);
        let state = ok(click_id(cx, "card-next"));
        assert_eq!(state["board"]["pick"], json!({"kind": "item", "key": "D7"}));
        assert_eq!(state["board"]["card"]["key"], "D7");
    }

    #[gpui::test]
    fn a_row_card_calls_its_focus_and_open_pr_buttons(cx: &mut TestAppContext) {
        let focused = calls();
        let opened = calls();
        let (_frame, cx) = frame_with_actions(
            cx,
            Some(fixture()),
            None,
            live_with_tree(),
            focus_into(focused.clone()),
            open_into(opened.clone()),
        );
        ok(click_id(cx, "work:owner/name#641"));

        let card = card_state(cx);
        assert_eq!(card["face"], "row");
        assert_eq!(card["key"], "owner/name#641");
        assert_eq!(card["label"], "#641");
        assert_eq!(
            card_buttons(&card),
            vec![
                ("card-focus", "Focus docs", true),
                ("card-pr", "Open PR", true)
            ],
            "#641 has no report"
        );

        ok(click_id(cx, "card-focus"));
        assert_eq!(*focused.borrow(), vec!["w1:p2".to_owned()]);
        ok(click_id(cx, "card-pr"));
        assert_eq!(
            *opened.borrow(),
            vec!["https://example.com/pull/41".to_owned()]
        );
    }

    #[gpui::test]
    fn card_report_shows_the_report_tab_and_back_keeps_the_pick(cx: &mut TestAppContext) {
        let file = tempfile::tempdir().unwrap();
        let path = file.path().join("RESULT.md");
        std::fs::write(&path, include_str!("../report_fixture.md")).unwrap();
        let mut snapshot = fixture();
        snapshot.rows[0].report = Some(path.to_str().unwrap().to_owned());
        let (_frame, cx) = frame_with(cx, Some(snapshot), None);

        ok(click_id(cx, "work:owner/name#640"));
        let state = ok(click_id(cx, "card-report"));
        assert_eq!(state["board"]["tab"], "report");
        assert_eq!(state["board"]["card"], Value::Null, "the card is not drawn");

        // The file is read in the background; the next state has it.
        let state = ok(send(cx, Request::State));
        let report = &state["board"]["report"];
        assert_eq!(report["row"], "owner/name#640");
        assert_eq!(report["title"], "#640  sort bags");
        assert_eq!(report["by"], "b-1151");
        assert_eq!(report["problem"], Value::Null);
        let lines: Vec<&str> = report["lines"]
            .as_array()
            .unwrap()
            .iter()
            .map(|line| line.as_str().unwrap())
            .collect();
        assert_eq!(lines[0], "Result: sort bags (#640)");
        assert!(
            !lines.iter().any(|line| line.contains("<!--")),
            "the hidden comment is not a drawn line"
        );

        // `back` emits an event; the panel handles it when the update's effects run, just after the
        // reply. The next state has the Board tab.
        ok(click_id(cx, "report-back"));
        let state = ok(send(cx, Request::State));
        assert_eq!(state["board"]["tab"], "board");
        assert_eq!(
            state["board"]["pick"],
            json!({"kind": "row", "key": "owner/name#640"}),
            "the pick is kept"
        );
        assert_eq!(state["board"]["report"], Value::Null);
    }

    #[gpui::test]
    fn the_report_controls_are_live_on_the_report_tab(cx: &mut TestAppContext) {
        let file = tempfile::tempdir().unwrap();
        let path = file.path().join("RESULT.md");
        std::fs::write(&path, "Plain text.\n").unwrap();
        let mut snapshot = fixture();
        snapshot.rows[0].report = Some(path.to_str().unwrap().to_owned());
        let (_frame, cx) = frame_with(cx, Some(snapshot), None);

        assert_eq!(live_in(cx, "report-back"), Some(json!(false)));
        assert_eq!(live_in(cx, "report-open"), Some(json!(false)));
        ok(click_id(cx, "board-tab-report"));
        assert_eq!(live_in(cx, "report-back"), Some(json!(true)));
        assert_eq!(
            live_in(cx, "report-open"),
            Some(json!(false)),
            "no row is followed"
        );

        ok(click_id(cx, "board-tab-board"));
        ok(click_id(cx, "work:owner/name#640"));
        ok(click_id(cx, "card-report"));
        assert_eq!(live_in(cx, "report-back"), Some(json!(true)));
        assert_eq!(live_in(cx, "report-open"), Some(json!(true)));
    }

    #[gpui::test]
    fn a_card_button_that_is_not_drawn_is_unknown_and_a_dim_one_is_disabled(
        cx: &mut TestAppContext,
    ) {
        let (_frame, cx) = frame_with(cx, Some(fixture()), None);

        // T12 has no options, so card-opt-0 is on no face.
        assert_eq!(err(click_id(cx, "card-opt-0")).code, code::UNKNOWN_ID);
        // Done is drawn for T12, but without a click.
        assert_eq!(err(click_id(cx, "card-done")).code, code::DISABLED);

        // The card is not drawn on the Report tab.
        ok(click_id(cx, "board-tab-report"));
        assert_eq!(err(click_id(cx, "card-done")).code, code::UNKNOWN_ID);
    }

    #[gpui::test]
    fn a_card_click_with_the_panel_closed_is_disabled(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, Some(fixture()), None);
        close_panel(cx, &frame);
        assert_eq!(err(click_id(cx, "card-done")).code, code::DISABLED);
        assert_eq!(err(click_id(cx, "report-back")).code, code::DISABLED);
    }
}
