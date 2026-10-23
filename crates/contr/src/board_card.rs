//! The answer card: the fixed 168 px frame at the foot of the board panel. It shows what the pick names:
//! a needs-you item, or a work row with its Focus, Report and Open PR buttons. The card only draws. The
//! panel resolves the pick and makes the clicks (`BoardPanel::card_face`). The item actions come with
//! the item card (S5).

use crate::board_actions::ago;
use crate::board_model::{BoardSnapshot, NeedItem, WorkRow, item_status};
use crate::status_icon::{Status, StatusIcon};
use crate::theme::*;
use contr_herdr::HerdrTree;
use gpui::{
    AnyElement, App, ClickEvent, FontWeight, IntoElement, ParentElement, Role, Window, div,
    prelude::*, px, rgb,
};
use std::rc::Rc;

/// A click on a card button. It gets the window's click event, as `on_click` does.
pub type ClickFn = Rc<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

/// Wraps a closure as a `ClickFn`.
pub fn click_fn(on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static) -> ClickFn {
    Rc::new(on_click)
}

/// What the pick names: a work row or a needs-you item, by its slot key.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BoardPick {
    Row(String),
    Item(String),
}

/// `true` when `pick` names a row or an item of `snapshot`.
pub fn names_on_board(snapshot: &BoardSnapshot, pick: &BoardPick) -> bool {
    match pick {
        BoardPick::Row(key) => snapshot.rows.iter().any(|row| row.key == *key),
        BoardPick::Item(key) => snapshot.items.iter().any(|item| item.key == *key),
    }
}

/// The pick the card shows on `snapshot`: `pick` when it names something on the board, else the first
/// open item (the mod's default). `None` when no item is open either.
pub fn effective_pick(snapshot: &BoardSnapshot, pick: Option<&BoardPick>) -> Option<BoardPick> {
    pick.filter(|pick| names_on_board(snapshot, pick))
        .cloned()
        .or_else(|| {
            snapshot
                .items
                .iter()
                .find(|item| item.open)
                .map(|item| BoardPick::Item(item.key.clone()))
        })
}

/// The label of the tab that holds the pane `pane_id`, or `None` when no tab of `tree` holds it.
pub fn tab_label_for_pane(tree: &HerdrTree, pane_id: &str) -> Option<String> {
    tree.workspaces
        .iter()
        .flat_map(|workspace| &workspace.tabs)
        .find(|tab| tab.panes.iter().any(|pane| pane.id == pane_id))
        .map(|tab| tab.label.clone())
}

/// What the card shows. The data is owned, so the card does not borrow the panel's snapshot.
pub enum CardFace {
    /// Nothing is picked and no item is open: the frame, with nothing in it.
    Empty,
    /// A needs-you item. Its actions come with the item card (S5).
    Item(NeedItem),
    /// A work row, with the buttons that apply to it.
    Row(RowFace),
}

/// A work row on the card. A button is drawn only when its field is set.
pub struct RowFace {
    pub row: WorkRow,
    /// `Focus {tab}`: the label of the tab that holds the row's pane, and the click.
    pub focus: Option<(String, ClickFn)>,
    /// `Report`: set when the row has a report.
    pub report: Option<ClickFn>,
    /// `Open PR`: set when the row has a URL.
    pub open_pr: Option<ClickFn>,
}

/// The card with what `face` shows. `now_ms` dates the `ago` part of an item's meta line.
pub fn answer_card(face: CardFace, now_ms: u64) -> impl IntoElement {
    let (head, text, actions) = match face {
        CardFace::Empty => (None, None, Vec::new()),
        // The item's actions come with S5. Until then the actions line is empty.
        CardFace::Item(item) => (
            Some(item_head(&item, now_ms).into_any_element()),
            item.text.clone(),
            Vec::new(),
        ),
        CardFace::Row(row) => {
            let head = head_line(
                None,
                format!("{}  {}", row.row.label, row.row.title),
                false,
                String::new(),
            );
            (
                Some(head.into_any_element()),
                row.row.now.clone(),
                row_buttons(row),
            )
        }
    };
    div()
        .debug_selector(|| "answer-card".into())
        .mx(px(ANSWER_MARGIN))
        .mb(px(ANSWER_MARGIN))
        .h(px(ANSWER_CARD_HEIGHT))
        .flex_none()
        .flex()
        .flex_col()
        .gap(px(ANSWER_GAP))
        .p(px(ANSWER_PAD))
        .bg(rgb(BG_ANSWER))
        .border_1()
        .border_color(rgb(ANSWER_BORDER))
        .rounded(px(ANSWER_RADIUS))
        .children(head)
        .child(card_text(text))
        .child(
            div()
                .flex_none()
                .flex()
                .gap(px(ANSWER_BUTTON_GAP))
                .children(actions),
        )
        // The foot line is empty until the item card (S5) fills it with `Next: {key}`.
        .child(
            div()
                .debug_selector(|| "card-foot".into())
                .h(px(LINE_HEIGHT))
                .flex_none(),
        )
}

/// The head line: a status icon (items only), the title in bold (cut with an ellipsis, struck through
/// when resolved), and `meta` in dim text on the right.
fn head_line(
    icon: Option<Status>,
    title: String,
    resolved: bool,
    meta: String,
) -> impl IntoElement {
    div()
        .debug_selector(|| "card-head".into())
        .h(px(LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(NEED_GAP))
        .when_some(icon, |line, status| {
            line.child(StatusIcon::new("card-icon", status).size(px(ICON_SIZE_SMALL)))
        })
        .child(
            div()
                .flex_1()
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .whitespace_nowrap()
                .font_weight(FontWeight::BOLD)
                .text_color(rgb(TEXT))
                .when(resolved, |title| title.line_through())
                .child(title),
        )
        .when(!meta.is_empty(), |line| {
            line.child(div().flex_none().text_color(rgb(TEXT_DIM)).child(meta))
        })
}

/// The head of an item: its status icon, `{key}  {title}`, and `from · ago` on the right.
fn item_head(item: &NeedItem, now_ms: u64) -> impl IntoElement {
    head_line(
        Some(item_status(item)),
        format!("{}  {}", item.key, item.title),
        !item.open,
        item_meta(item, now_ms),
    )
}

/// The `from · ago` text of an item. A part the item lacks is left out. `ago` reads `now` for a time
/// under a minute old.
pub fn item_meta(item: &NeedItem, now_ms: u64) -> String {
    let since = item.at.map(|at| ago(now_ms, at));
    [item.from.clone(), since]
        .into_iter()
        .flatten()
        .filter(|part| !part.is_empty())
        .collect::<Vec<_>>()
        .join(" · ")
}

/// The selected-card background of a picked work row or item. It fills the row and sits behind the
/// row's content, as the agents list draws its selected row. `selector` names it, such as
/// `work-row-0-picked`, so a test can find the picked row.
pub fn selected_card(selector: String) -> impl IntoElement {
    div()
        .debug_selector(move || selector)
        .absolute()
        .inset_0()
        .rounded(px(CARD_RADIUS))
        .bg(rgb(BG_CARD))
        .border_1()
        .border_color(rgb(CARD_BORDER))
}

/// The text of the card, in `TEXT_2`, at most three lines. A longer text is clipped. The text takes
/// the room that the actions line does not, so the actions stay at the bottom.
fn card_text(text: Option<String>) -> impl IntoElement {
    div()
        .debug_selector(|| "card-text".into())
        .flex_1()
        .min_h_0()
        .overflow_hidden()
        .text_color(rgb(TEXT_2))
        .line_height(px(LINE_HEIGHT))
        .child(
            div()
                .max_h(px(ANSWER_TEXT_HEIGHT))
                .overflow_hidden()
                .child(text.unwrap_or_default()),
        )
}

/// The buttons of a row, in order: `Focus {tab}`, `Report`, `Open PR`. Each is drawn only when set.
fn row_buttons(row: RowFace) -> Vec<AnyElement> {
    let mut buttons = Vec::new();
    if let Some((tab, on_click)) = row.focus {
        let label = format!("Focus {tab}");
        buttons.push(card_button("card-focus", label, on_click).into_any_element());
    }
    if let Some(on_click) = row.report {
        buttons.push(card_button("card-report", "Report".to_owned(), on_click).into_any_element());
    }
    if let Some(on_click) = row.open_pr {
        buttons.push(card_button("card-pr", "Open PR".to_owned(), on_click).into_any_element());
    }
    buttons
}

/// An outlined button of the card. Its accessible name is its label.
fn card_button(selector: &'static str, label: String, on_click: ClickFn) -> impl IntoElement {
    div()
        .id(selector)
        .debug_selector(move || selector.to_owned())
        .role(Role::Button)
        .aria_label(label.clone())
        .h(px(ANSWER_BUTTON_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .px(px(ANSWER_BUTTON_PAD_X))
        .rounded(px(ANSWER_BUTTON_RADIUS))
        .border_1()
        .border_color(rgb(CARD_BORDER))
        .text_color(rgb(TEXT))
        .cursor_pointer()
        .on_click(move |event, window, cx| on_click(event, window, cx))
        .child(label)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::agents_list::FocusFn;
    use crate::board::GlobalBoard;
    use crate::board_panel::BoardPanel;
    use crate::board_panel::OpenFn;
    use crate::board_panel::test_support::{
        bounds, bridge, fixture, ignored_focus, ignored_open, panel, panel_on,
    };
    use contr_herdr::{AgentState, Pane, Tab, Workspace};
    use gpui::{Entity, Modifiers, TestAppContext, VisualTestContext};
    use std::cell::RefCell;
    use std::collections::BTreeMap;

    /// One workspace with one tab, labelled `tab`, that holds the panes `panes`.
    fn tree_with(tab: &str, panes: &[&str]) -> HerdrTree {
        let panes = panes
            .iter()
            .map(|id| Pane {
                id: (*id).to_owned(),
                label: None,
                cwd: None,
                focused: false,
                agent: None,
                tokens: BTreeMap::new(),
            })
            .collect();
        HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: None,
            workspaces: vec![Workspace {
                id: "w1".into(),
                label: "w1".into(),
                status: AgentState::Idle,
                focused: true,
                tabs: vec![Tab {
                    id: "t1".into(),
                    label: tab.into(),
                    status: AgentState::Idle,
                    focused: false,
                    panes,
                }],
            }],
        }
    }

    /// The calls that the card's Focus and Open PR buttons made, in order.
    type Calls = Rc<RefCell<Vec<String>>>;

    /// A panel over `snapshot` with `tree`, and the Focus and Open PR calls it makes recorded.
    fn recording_panel(
        cx: &mut TestAppContext,
        snapshot: Option<BoardSnapshot>,
        tree: Option<HerdrTree>,
    ) -> (Entity<BoardPanel>, Calls, Calls, &mut VisualTestContext) {
        let focused: Calls = Rc::new(RefCell::new(Vec::new()));
        let opened: Calls = Rc::new(RefCell::new(Vec::new()));
        let focus_log = focused.clone();
        let open_log = opened.clone();
        let focus: FocusFn =
            Rc::new(move |pane: String, _: &mut App| focus_log.borrow_mut().push(pane));
        let open: OpenFn = Rc::new(move |url: String, _: &mut App| open_log.borrow_mut().push(url));
        let (view, cx) = panel_on(cx, snapshot, None, Some(bridge()), tree, focus, open);
        (view, focused, opened, cx)
    }

    /// Shows `snapshot` on the board, as a change from a read would.
    fn show(cx: &mut VisualTestContext, snapshot: BoardSnapshot) {
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| {
                board.set(Some(snapshot), None, Some(bridge()), cx)
            })
        });
        cx.run_until_parked();
    }

    /// Clicks the centre of the element with the given selector, as the mouse would.
    fn click(cx: &mut VisualTestContext, selector: &'static str) {
        let bounds = cx.debug_bounds(selector).unwrap();
        cx.simulate_click(bounds.center(), Modifiers::none());
        cx.run_until_parked();
    }

    /// The fixture's first work row, the one with a pane, a URL and a report.
    const ROW_640: &str = "owner/name#640";

    /// The fixture with its items replaced by the JSON array `items`.
    fn fixture_with_items(items: &str) -> BoardSnapshot {
        let mut snapshot = fixture();
        snapshot.items = serde_json::from_str(items).unwrap();
        snapshot
    }

    #[test]
    fn the_default_pick_is_the_first_open_item() {
        let snapshot = fixture();
        assert_eq!(
            effective_pick(&snapshot, None),
            Some(BoardPick::Item("T12".into()))
        );
    }

    #[test]
    fn a_pick_on_the_board_is_kept() {
        let snapshot = fixture();
        let item = BoardPick::Item("D7".into());
        let row = BoardPick::Row(ROW_640.into());
        assert_eq!(effective_pick(&snapshot, Some(&item)), Some(item));
        assert_eq!(effective_pick(&snapshot, Some(&row)), Some(row));
    }

    #[test]
    fn a_pick_whose_key_is_gone_falls_back_to_the_first_open_item() {
        let snapshot = fixture();
        let gone_row = BoardPick::Row("owner/name#999".into());
        let gone_item = BoardPick::Item("D99".into());
        assert_eq!(
            effective_pick(&snapshot, Some(&gone_row)),
            Some(BoardPick::Item("T12".into()))
        );
        assert_eq!(
            effective_pick(&snapshot, Some(&gone_item)),
            Some(BoardPick::Item("T12".into()))
        );
    }

    #[test]
    fn with_no_open_item_and_no_pick_nothing_is_shown() {
        let snapshot = fixture_with_items(r#"[{"key":"D6","kind":"ask","open":false}]"#);
        assert_eq!(effective_pick(&snapshot, None), None);
    }

    #[test]
    fn the_tab_label_is_the_one_of_the_tab_that_holds_the_pane() {
        let mut tree = tree_with("builder", &["w1:p4"]);
        tree.workspaces[0].tabs.push(Tab {
            id: "t2".into(),
            label: "reviewer".into(),
            status: AgentState::Idle,
            focused: false,
            panes: vec![Pane {
                id: "w1:p2".into(),
                label: None,
                cwd: None,
                focused: false,
                agent: None,
                tokens: BTreeMap::new(),
            }],
        });
        assert_eq!(
            tab_label_for_pane(&tree, "w1:p4"),
            Some("builder".to_owned())
        );
        assert_eq!(
            tab_label_for_pane(&tree, "w1:p2"),
            Some("reviewer".to_owned())
        );
        assert_eq!(tab_label_for_pane(&tree, "w1:p9"), None);
    }

    #[test]
    fn the_item_meta_leaves_out_what_the_item_lacks() {
        let now = 1_000_000_000;
        // 300 000 s ago is three days, and 300 s ago is five minutes.
        let full: NeedItem =
            serde_json::from_str(r#"{"key":"D7","kind":"ask","from":"p-7","at":700000000}"#)
                .unwrap();
        assert_eq!(item_meta(&full, now), "p-7 · 3d");

        let no_from: NeedItem =
            serde_json::from_str(r#"{"key":"D7","kind":"ask","at":999700000}"#).unwrap();
        assert_eq!(item_meta(&no_from, now), "5m");

        let no_at: NeedItem =
            serde_json::from_str(r#"{"key":"D7","kind":"ask","from":"p-7"}"#).unwrap();
        assert_eq!(item_meta(&no_at, now), "p-7");

        let blank_from: NeedItem =
            serde_json::from_str(r#"{"key":"D7","kind":"ask","from":""}"#).unwrap();
        assert_eq!(item_meta(&blank_from, now), "");
    }

    #[gpui::test]
    fn the_card_is_168_tall_and_ends_one_margin_above_the_panel_bottom(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        let card = bounds(cx, "answer-card").unwrap();
        let board = bounds(cx, "board-panel").unwrap();
        assert_eq!(card.size.height, px(ANSWER_CARD_HEIGHT));
        assert_eq!(card.bottom(), board.bottom() - px(ANSWER_MARGIN));
        assert_eq!(card.origin.x, board.origin.x + px(ANSWER_MARGIN));
    }

    #[gpui::test]
    fn the_default_pick_shows_the_first_open_item_and_no_row_buttons(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "card-head").is_some(), "T12 is in the head");
        assert!(bounds(cx, "need-row-0-picked").is_some(), "T12 is picked");
        assert!(bounds(cx, "card-focus").is_none());
        assert!(bounds(cx, "card-report").is_none());
        assert!(bounds(cx, "card-pr").is_none());
    }

    #[gpui::test]
    fn the_card_is_on_the_board_tab_only(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        click(cx, "board-tab-report");
        assert!(bounds(cx, "answer-card").is_none());
    }

    #[gpui::test]
    fn with_nothing_picked_or_open_the_card_is_empty(cx: &mut TestAppContext) {
        let mut snapshot = fixture();
        snapshot.items.clear();
        snapshot.rows.clear();
        let (_view, cx) = panel(cx, Some(snapshot), None, Some(bridge()));
        assert!(bounds(cx, "answer-card").is_some(), "the frame is drawn");
        assert!(bounds(cx, "card-head").is_none(), "nothing in it");
    }

    #[gpui::test]
    fn a_click_on_a_work_row_shows_its_buttons(cx: &mut TestAppContext) {
        let tree = tree_with("builder", &["w1:p4"]);
        let (_view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), Some(tree));
        click(cx, "work-row-0");

        assert!(
            bounds(cx, "work-row-0-picked").is_some(),
            "the row is picked"
        );
        assert!(
            bounds(cx, "need-row-0-picked").is_none(),
            "T12 is not picked"
        );
        assert!(bounds(cx, "card-head").is_some());
        assert!(bounds(cx, "card-focus").is_some(), "the pane is in a tab");
        assert!(bounds(cx, "card-report").is_some(), "the row has a report");
        assert!(bounds(cx, "card-pr").is_some(), "the row has a URL");
    }

    #[gpui::test]
    fn focus_records_the_pane_and_open_pr_the_url(cx: &mut TestAppContext) {
        let tree = tree_with("builder", &["w1:p4"]);
        let (_view, focused, opened, cx) = recording_panel(cx, Some(fixture()), Some(tree));
        click(cx, "work-row-0");

        click(cx, "card-focus");
        assert_eq!(*focused.borrow(), vec!["w1:p4".to_owned()]);

        click(cx, "card-pr");
        assert_eq!(
            *opened.borrow(),
            vec!["https://example.com/pull/40".to_owned()]
        );
    }

    #[gpui::test]
    fn report_shows_the_report_tab_for_the_row(cx: &mut TestAppContext) {
        let (view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), None);
        click(cx, "work-row-0");
        click(cx, "card-report");

        assert!(bounds(cx, "board-tab-report-on").is_some());
        let key = view.read_with(cx, |panel, _| panel.report_key().map(str::to_owned));
        assert_eq!(key.as_deref(), Some(ROW_640));
    }

    #[gpui::test]
    fn a_row_without_pane_url_or_report_has_none_of_its_buttons(cx: &mut TestAppContext) {
        let tree = tree_with("builder", &["w1:p4", "w1:p2"]);
        let (_view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), Some(tree));
        // #642 has no pane, no URL and no report.
        click(cx, "work-row-2");

        assert!(bounds(cx, "card-head").is_some());
        assert!(bounds(cx, "card-focus").is_none());
        assert!(bounds(cx, "card-report").is_none());
        assert!(bounds(cx, "card-pr").is_none());
    }

    #[gpui::test]
    fn a_pane_missing_from_the_tree_has_no_focus_button(cx: &mut TestAppContext) {
        // #641 has pane w1:p2 and a URL, but the tree holds only w1:p4.
        let tree = tree_with("builder", &["w1:p4"]);
        let (_view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), Some(tree));
        click(cx, "work-row-1");

        assert!(bounds(cx, "card-focus").is_none());
        assert!(bounds(cx, "card-pr").is_some());
        assert!(bounds(cx, "card-report").is_none(), "#641 has no report");
    }

    #[gpui::test]
    fn without_a_herdr_tree_a_row_has_no_focus_button(cx: &mut TestAppContext) {
        let (_view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), None);
        click(cx, "work-row-0");
        assert!(bounds(cx, "card-focus").is_none());
        assert!(bounds(cx, "card-pr").is_some());
    }

    #[gpui::test]
    fn a_click_on_an_item_row_picks_the_item(cx: &mut TestAppContext) {
        let (_view, cx) = panel_on(
            cx,
            Some(fixture()),
            None,
            Some(bridge()),
            None,
            ignored_focus(),
            ignored_open(),
        );
        click(cx, "work-row-0");
        click(cx, "need-row-1");

        assert!(bounds(cx, "need-row-1-picked").is_some(), "D7 is picked");
        assert!(bounds(cx, "work-row-0-picked").is_none(), "the row is not");
        assert!(bounds(cx, "card-head").is_some());
        assert!(
            bounds(cx, "card-focus").is_none(),
            "an item has no row buttons"
        );
    }

    #[gpui::test]
    fn the_row_pick_survives_a_new_snapshot_with_the_same_keys(cx: &mut TestAppContext) {
        let tree = tree_with("builder", &["w1:p4"]);
        let (_view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), Some(tree));
        click(cx, "work-row-0");

        let mut next = fixture();
        next.rev = 43;
        show(cx, next);
        assert!(bounds(cx, "work-row-0-picked").is_some());
        assert!(bounds(cx, "card-focus").is_some());
    }

    #[gpui::test]
    fn a_picked_row_that_leaves_the_board_falls_back_to_the_first_open_item(
        cx: &mut TestAppContext,
    ) {
        let (view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), None);
        click(cx, "work-row-0");

        let mut gone = fixture();
        gone.rows.clear();
        gone.rev = 43;
        show(cx, gone);
        assert!(
            bounds(cx, "need-row-0-picked").is_some(),
            "T12 is picked again"
        );
        assert!(bounds(cx, "card-head").is_some());
        let stored = view.read_with(cx, |panel, _| panel.picked().cloned());
        assert_eq!(stored, None, "the gone pick is dropped");
    }

    #[gpui::test]
    fn the_row_pick_is_kept_across_the_tabs(cx: &mut TestAppContext) {
        let tree = tree_with("builder", &["w1:p4"]);
        let (view, _focused, _opened, cx) = recording_panel(cx, Some(fixture()), Some(tree));
        click(cx, "work-row-0");
        click(cx, "board-tab-report");
        click(cx, "board-tab-board");

        assert!(bounds(cx, "work-row-0-picked").is_some());
        assert!(bounds(cx, "card-focus").is_some());
        let stored = view.read_with(cx, |panel, _| panel.picked().cloned());
        assert_eq!(stored, Some(BoardPick::Row(ROW_640.to_owned())));
    }
}
