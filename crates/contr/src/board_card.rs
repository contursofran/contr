//! The answer card: the fixed 168 px frame at the foot of the board panel. It shows what the pick names:
//! a needs-you item with its actions, or a work row with its Focus, Report and Open PR buttons. The card
//! only draws. The panel resolves the pick, builds the faces and makes the clicks (`BoardPanel::card_face`).

use crate::board_actions::{Action, ago, card_options};
use crate::board_model::{
    BoardSnapshot, ItemKind, NeedItem, WorkRow, item_kind_label, item_status,
};
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

/// The item that a card key acts on: the item the card shows. `None` for a work row, or with no pick.
pub fn card_item<'a>(items: &'a [NeedItem], shown: Option<&BoardPick>) -> Option<&'a NeedItem> {
    match shown {
        Some(BoardPick::Item(key)) => items.iter().find(|item| item.key == *key),
        _ => None,
    }
}

/// The pick that an up or down key moves to, from the pick the card shows (`shown`). Down goes to the
/// next item in slot order, up to the one before, and the ends stop. Down from a work row, or with no
/// pick, goes to the first item. Up from a work row, or with no pick, does not move. `None` when the
/// key does not move the pick.
pub fn stepped_pick(
    items: &[NeedItem],
    shown: Option<&BoardPick>,
    down: bool,
) -> Option<BoardPick> {
    let index = match shown {
        Some(BoardPick::Item(key)) => items.iter().position(|item| item.key == *key),
        _ => None,
    };
    let target = match (index, down) {
        (Some(index), true) => items.get(index + 1),
        (Some(index), false) => index
            .checked_sub(1)
            .and_then(|previous| items.get(previous)),
        (None, true) => items.first(),
        (None, false) => None,
    };
    target.map(|item| BoardPick::Item(item.key.clone()))
}

/// The action that `enter` sends for `item`, the same as its main button: the recommended option of an
/// open decision (none when no option is recommended), Done for an open to-do, Dismiss for open news.
/// `None` for a resolved item.
pub fn enter_action(item: &NeedItem) -> Option<Action> {
    if !item.open {
        return None;
    }
    match item.kind {
        ItemKind::Ask => card_options(item)
            .first()
            .filter(|(_, _, recommended)| *recommended)
            .map(|(index, label, _)| answer(item, *index, label)),
        ItemKind::Do => Some(Action::Done {
            key: item.key.clone(),
        }),
        ItemKind::Know | ItemKind::Other => Some(Action::Dismiss {
            key: item.key.clone(),
        }),
    }
}

/// The action that the digit key for `position` (0 for `1`) sends for `item`: the option at that place
/// in the card's order, the same as its option button. Only an open decision has options. `None` when
/// there is no such option.
pub fn option_action(item: &NeedItem, position: usize) -> Option<Action> {
    if !item.open || item.kind != ItemKind::Ask {
        return None;
    }
    card_options(item)
        .get(position)
        .map(|(index, label, _)| answer(item, *index, label))
}

/// The `Answer` for option `option` of `item`, with its `label`.
fn answer(item: &NeedItem, option: usize, label: &str) -> Action {
    Action::Answer {
        key: item.key.clone(),
        option,
        label: label.to_owned(),
    }
}

/// The accessible name of an item card: `{key} {title}, {kind label}`. A decision adds its option
/// labels after `options`, in the card's order.
pub fn item_card_label(item: &NeedItem, actions: &ItemActions) -> String {
    let base = format!("{} {}, {}", item.key, item.title, item_kind_label(item));
    match actions {
        ItemActions::Decide { options, .. } => {
            let labels: Vec<&str> = options.iter().map(|option| option.label.as_str()).collect();
            format!("{base}, options {}", labels.join(", "))
        }
        ItemActions::Do { .. } | ItemActions::Know { .. } | ItemActions::Resolved { .. } => base,
    }
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
    /// A needs-you item, with its actions.
    Item(ItemFace),
    /// A work row, with the buttons that apply to it.
    Row(RowFace),
}

/// A needs-you item on the card.
pub struct ItemFace {
    /// The item as the card draws it. An item with an answer queued reads as resolved (see `shown_item`).
    pub item: NeedItem,
    pub actions: ItemActions,
    /// `Next: {key}` and the click that picks that item. `None` when no other item is open.
    pub next: Option<(String, ClickFn)>,
    /// `true` when the board takes no actions: the foot says so, and the item buttons do nothing.
    pub inert: bool,
}

/// The actions line of an item. A button's `on_click` is `None` when the board takes no actions.
pub enum ItemActions {
    /// An open decision: one button per option in card order, then `Answer in chat`.
    Decide {
        options: Vec<OptionFace>,
        chat: Option<ClickFn>,
    },
    /// An open to-do: `Done`, then `Open link` when the item has a link.
    Do {
        done: Option<ClickFn>,
        link: Option<ClickFn>,
    },
    /// Open news: `Dismiss`.
    Know { dismiss: Option<ClickFn> },
    /// A resolved item, or one with an answer queued: `✓ {answer}`, then `undo` while the answer can
    /// still be taken back, then `· sending` once it is written.
    Resolved {
        answer: String,
        undo: Option<ClickFn>,
        sending: bool,
    },
}

/// One option of a decision. `recommended` makes it the filled button.
pub struct OptionFace {
    pub label: String,
    pub recommended: bool,
    pub on_click: Option<ClickFn>,
}

/// How a card button is drawn.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ButtonStyle {
    /// White, with bold dark text: the owner acts (`YOU_BG`, `YOU_TEXT`).
    Filled,
    /// Outlined grey, with `TEXT`.
    Outlined,
    /// No border: text in `colour`, underlined on hover.
    Plain { colour: u32 },
}

/// The style of an option button: the recommended option is filled, the others are outlined.
pub fn option_style(recommended: bool) -> ButtonStyle {
    if recommended {
        ButtonStyle::Filled
    } else {
        ButtonStyle::Outlined
    }
}

/// The item as the card and the needs-you rows draw it. An item with an answer queued reads as
/// resolved, with that answer, until the mod takes the action or refuses it.
pub fn shown_item(item: &NeedItem, queued: Option<&str>) -> NeedItem {
    let mut shown = item.clone();
    if let Some(answer) = queued {
        shown.open = false;
        shown.answer = Some(answer.to_owned());
    }
    shown
}

/// The answer that a resolved item shows: its answer, or `done` when it has none.
pub fn resolved_answer(item: &NeedItem) -> String {
    item.answer.clone().unwrap_or_else(|| "done".to_owned())
}

/// The URL that a to-do's link opens. A link without `://` gets `http://` in front.
pub fn link_url(link: &str) -> String {
    if link.contains("://") {
        link.to_owned()
    } else {
        format!("http://{link}")
    }
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
    let label = match &face {
        CardFace::Item(face) => Some(item_card_label(&face.item, &face.actions)),
        CardFace::Empty | CardFace::Row(_) => None,
    };
    let (head, text, actions, foot) = match face {
        CardFace::Empty => (None, None, Vec::new(), Vec::new()),
        CardFace::Item(face) => (
            Some(item_head(&face.item, now_ms).into_any_element()),
            face.item.text.clone(),
            item_actions(face.actions),
            item_foot(face.inert, face.next),
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
                Vec::new(),
            )
        }
    };
    div()
        .id("answer-card")
        .debug_selector(|| "answer-card".into())
        .when_some(label, |card, label| {
            card.role(Role::Group).aria_label(label)
        })
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
                .flex_wrap()
                .gap(px(ANSWER_BUTTON_GAP))
                .children(actions),
        )
        .child(
            div()
                .debug_selector(|| "card-foot".into())
                .h(px(LINE_HEIGHT))
                .flex_none()
                .flex()
                .items_center()
                .children(foot),
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
        buttons.push(action_line_button(
            "card-focus",
            label,
            ButtonStyle::Outlined,
            Some(on_click),
        ));
    }
    if let Some(on_click) = row.report {
        buttons.push(action_line_button(
            "card-report",
            "Report".to_owned(),
            ButtonStyle::Outlined,
            Some(on_click),
        ));
    }
    if let Some(on_click) = row.open_pr {
        buttons.push(action_line_button(
            "card-pr",
            "Open PR".to_owned(),
            ButtonStyle::Outlined,
            Some(on_click),
        ));
    }
    buttons
}

/// The actions line of an item, in order. See `ItemActions` for each kind.
fn item_actions(actions: ItemActions) -> Vec<AnyElement> {
    match actions {
        ItemActions::Decide { options, chat } => {
            let mut line: Vec<AnyElement> = options
                .into_iter()
                .enumerate()
                .map(|(position, option)| {
                    action_line_button(
                        &format!("card-opt-{position}"),
                        option.label,
                        option_style(option.recommended),
                        option.on_click,
                    )
                })
                .collect();
            line.push(action_line_button(
                "card-chat",
                "Answer in chat".to_owned(),
                ButtonStyle::Plain { colour: TEXT_DIM },
                chat,
            ));
            line
        }
        ItemActions::Do { done, link } => {
            let mut line = vec![action_line_button(
                "card-done",
                "Done".to_owned(),
                ButtonStyle::Filled,
                done,
            )];
            if let Some(on_click) = link {
                line.push(action_line_button(
                    "card-link",
                    "Open link".to_owned(),
                    ButtonStyle::Outlined,
                    Some(on_click),
                ));
            }
            line
        }
        ItemActions::Know { dismiss } => vec![action_line_button(
            "card-dismiss",
            "Dismiss".to_owned(),
            ButtonStyle::Outlined,
            dismiss,
        )],
        ItemActions::Resolved {
            answer,
            undo,
            sending,
        } => {
            let mut line = vec![
                div()
                    .debug_selector(|| "card-answer".into())
                    .flex_none()
                    .text_color(rgb(TEXT_2))
                    .child(format!("✓ {answer}"))
                    .into_any_element(),
            ];
            if let Some(on_click) = undo {
                line.push(action_line_button(
                    "card-undo",
                    "undo".to_owned(),
                    ButtonStyle::Plain { colour: TEXT },
                    Some(on_click),
                ));
            }
            if sending {
                line.push(
                    div()
                        .debug_selector(|| "card-sending".into())
                        .flex_none()
                        .text_color(rgb(TEXT_DIM))
                        .child("· sending")
                        .into_any_element(),
                );
            }
            line
        }
    }
}

/// The foot of an item card: the inert note on the left when the board takes no actions, and
/// `Next: {key}` on the right.
fn item_foot(inert: bool, next: Option<(String, ClickFn)>) -> Vec<AnyElement> {
    let mut foot = Vec::new();
    if inert {
        foot.push(
            div()
                .debug_selector(|| "card-inert".into())
                .flex_none()
                .text_color(rgb(TEXT_DIM))
                .child("coordinator mod takes no actions")
                .into_any_element(),
        );
    }
    if let Some((key, on_click)) = next {
        foot.push(
            div()
                .flex_none()
                .ml_auto()
                .child(action_line_button(
                    "card-next",
                    format!("Next: {key}"),
                    ButtonStyle::Plain { colour: TEXT_DIM },
                    Some(on_click),
                ))
                .into_any_element(),
        );
    }
    foot
}

/// A card button as an element. With `on_click` `None` the board takes no actions: the button is dim
/// and does nothing. The button's accessible name is its label.
fn action_line_button(
    selector: &str,
    label: String,
    style: ButtonStyle,
    on_click: Option<ClickFn>,
) -> AnyElement {
    let live = on_click.is_some();
    let base = div()
        .id(selector.to_owned())
        .debug_selector({
            let selector = selector.to_owned();
            move || selector
        })
        .role(Role::Button)
        .aria_label(label.clone())
        .h(px(ANSWER_BUTTON_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .px(px(ANSWER_BUTTON_PAD_X))
        .rounded(px(ANSWER_BUTTON_RADIUS))
        .child(label);
    let styled = match style {
        ButtonStyle::Filled if live => base
            .bg(rgb(YOU_BG))
            .font_weight(FontWeight::BOLD)
            .text_color(rgb(YOU_TEXT)),
        ButtonStyle::Plain { colour } if live => base
            .text_color(rgb(colour))
            .hover(|hovered| hovered.underline()),
        ButtonStyle::Plain { .. } => base.text_color(rgb(TEXT_DIM)),
        ButtonStyle::Filled | ButtonStyle::Outlined if live => base
            .border_1()
            .border_color(rgb(CARD_BORDER))
            .text_color(rgb(TEXT)),
        ButtonStyle::Filled | ButtonStyle::Outlined => base
            .border_1()
            .border_color(rgb(CARD_BORDER))
            .text_color(rgb(TEXT_DIM)),
    };
    match on_click {
        Some(on_click) => styled
            .cursor_pointer()
            .on_click(move |event, window, cx| on_click(event, window, cx))
            .into_any_element(),
        None => styled.into_any_element(),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::agents_list::FocusFn;
    use crate::board::GlobalBoard;
    use crate::board_actions::{Action, UNDO_MS};
    use crate::board_model::ActionFailure;
    use crate::board_panel::BoardPanel;
    use crate::board_panel::OpenFn;
    use crate::board_panel::test_support::{
        bounds, bridge, fixture, ignored_focus, ignored_open, panel, panel_on, panel_with_queue_on,
        queued, status,
    };
    use contr_herdr::{AgentState, Pane, Tab, Workspace};
    use gpui::{Bounds, Entity, Modifiers, Pixels, TestAppContext, VisualTestContext};
    use std::cell::RefCell;
    use std::collections::BTreeMap;
    use std::time::Duration;

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

    /// The answer that D7's first button sends when the fixture recommends its second option.
    const D7_ANSWER: &str = "two lines per row";

    /// The fixture with D7 recommending its second option, so `card-opt-0` is `D7_ANSWER` (index 1).
    fn fixture_recommending_the_second_option() -> BoardSnapshot {
        let mut snapshot = fixture();
        item_mut(&mut snapshot, "D7").recommended = Some(1);
        snapshot
    }

    /// The item with `key` in `snapshot`.
    fn item_mut<'a>(snapshot: &'a mut BoardSnapshot, key: &str) -> &'a mut NeedItem {
        snapshot
            .items
            .iter_mut()
            .find(|item| item.key == key)
            .unwrap()
    }

    /// Moves the test clock by `by`, after what is due has run, and runs what that makes due.
    fn advance(cx: &mut VisualTestContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    /// A panel with a queue in `dir` over `snapshot`, with the Focus and Open PR calls recorded.
    fn queued_recording_panel<'a>(
        cx: &'a mut TestAppContext,
        dir: &std::path::Path,
        snapshot: BoardSnapshot,
    ) -> (Entity<BoardPanel>, Calls, Calls, &'a mut VisualTestContext) {
        let focused: Calls = Rc::new(RefCell::new(Vec::new()));
        let opened: Calls = Rc::new(RefCell::new(Vec::new()));
        let focus_log = focused.clone();
        let open_log = opened.clone();
        let focus: FocusFn =
            Rc::new(move |pane: String, _: &mut App| focus_log.borrow_mut().push(pane));
        let open: OpenFn = Rc::new(move |url: String, _: &mut App| open_log.borrow_mut().push(url));
        let (view, cx) = panel_with_queue_on(cx, dir, snapshot, focus, open);
        (view, focused, opened, cx)
    }

    #[test]
    fn a_link_without_a_scheme_gets_http() {
        assert_eq!(
            link_url("example.com/pull/41"),
            "http://example.com/pull/41"
        );
        assert_eq!(
            link_url("https://example.com/pull/41"),
            "https://example.com/pull/41"
        );
    }

    #[test]
    fn the_recommended_option_is_filled_and_the_others_are_outlined() {
        assert_eq!(option_style(true), ButtonStyle::Filled);
        assert_eq!(option_style(false), ButtonStyle::Outlined);
    }

    #[test]
    fn a_queued_answer_shows_the_item_resolved_with_it() {
        let item: NeedItem = serde_json::from_str(r#"{"key":"D7","kind":"ask"}"#).unwrap();
        let queued = shown_item(&item, Some(D7_ANSWER));
        assert!(!queued.open);
        assert_eq!(queued.answer.as_deref(), Some(D7_ANSWER));
        assert_eq!(
            shown_item(&item, None),
            item,
            "without a queued answer, nothing changes"
        );
    }

    #[test]
    fn a_resolved_item_without_an_answer_shows_done() {
        let answered: NeedItem =
            serde_json::from_str(r#"{"key":"D6","kind":"ask","open":false,"answer":"keep"}"#)
                .unwrap();
        let bare: NeedItem =
            serde_json::from_str(r#"{"key":"D6","kind":"ask","open":false}"#).unwrap();
        assert_eq!(resolved_answer(&answered), "keep");
        assert_eq!(resolved_answer(&bare), "done");
    }

    #[gpui::test]
    fn a_decide_item_shows_its_options_then_answer_in_chat(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, _focused, _opened, cx) =
            queued_recording_panel(cx, dir.path(), fixture_recommending_the_second_option());
        click(cx, "need-row-1");

        // The actions line may wrap, so the order is reading order: the line first, then x.
        let first = bounds(cx, "card-opt-0").unwrap();
        let second = bounds(cx, "card-opt-1").unwrap();
        let chat = bounds(cx, "card-chat").unwrap();
        let reads_before = |a: Bounds<Pixels>, b: Bounds<Pixels>| {
            a.origin.y < b.origin.y || (a.origin.y == b.origin.y && a.origin.x < b.origin.x)
        };
        assert!(
            reads_before(first, second),
            "the recommended option is first"
        );
        assert!(reads_before(second, chat), "Answer in chat comes last");
        assert!(bounds(cx, "card-done").is_none(), "a decision has no Done");
        assert!(bounds(cx, "card-next").is_some(), "N4 is open after D7");
    }

    #[gpui::test]
    fn an_option_click_queues_the_answer_and_undo_takes_it_back(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (view, _focused, _opened, cx) =
            queued_recording_panel(cx, dir.path(), fixture_recommending_the_second_option());
        click(cx, "need-row-1");
        click(cx, "card-opt-0");

        assert!(bounds(cx, "card-answer").is_some(), "the answer shows");
        assert!(
            bounds(cx, "card-undo").is_some(),
            "the answer can be undone"
        );
        assert!(
            bounds(cx, "need-row-1-resolved").is_some(),
            "D7 reads as resolved"
        );
        assert!(bounds(cx, "card-opt-0").is_none(), "the options are gone");
        let stored = view.read_with(cx, |panel, _| panel.picked().cloned());
        assert_eq!(
            stored,
            Some(BoardPick::Item("D7".to_owned())),
            "the pick stays on D7"
        );
        assert_eq!(queued(dir.path()), None, "a held answer is not written yet");

        click(cx, "card-undo");
        assert!(bounds(cx, "card-opt-0").is_some(), "the options are back");
        assert!(bounds(cx, "card-answer").is_none());
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(
            queued(dir.path()),
            None,
            "the undone answer is never written"
        );
    }

    #[gpui::test]
    fn an_answer_is_written_after_the_undo_window_and_leaves_once_acted(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, _focused, _opened, cx) =
            queued_recording_panel(cx, dir.path(), fixture_recommending_the_second_option());
        click(cx, "need-row-1");
        click(cx, "card-opt-0");

        advance(cx, Duration::from_millis(UNDO_MS - 1));
        assert_eq!(queued(dir.path()), None, "the window is not over");
        assert!(bounds(cx, "card-sending").is_none());

        advance(cx, Duration::from_millis(1));
        let answer = Action::Answer {
            key: "D7".to_owned(),
            option: 1,
            label: D7_ANSWER.to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, answer)]));
        assert!(
            bounds(cx, "card-sending").is_some(),
            "it is written, not yet taken"
        );
        assert!(
            bounds(cx, "card-undo").is_none(),
            "a written answer can not be undone"
        );

        // The mod takes seq 1 and shows D7 answered.
        let mut taken = fixture_recommending_the_second_option();
        taken.rev = 43;
        taken.acted = 1;
        let d7 = item_mut(&mut taken, "D7");
        d7.open = false;
        d7.answer = Some(D7_ANSWER.to_owned());
        show(cx, taken);
        assert_eq!(
            queued(dir.path()),
            Some(vec![]),
            "the taken action leaves the file"
        );
        assert!(
            bounds(cx, "card-answer").is_some(),
            "the answer stays on the card"
        );
        assert!(bounds(cx, "card-sending").is_none());
        assert!(bounds(cx, "card-undo").is_none());
    }

    #[gpui::test]
    fn a_refused_answer_shows_its_status_and_the_options_again(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, _focused, _opened, cx) =
            queued_recording_panel(cx, dir.path(), fixture_recommending_the_second_option());
        click(cx, "need-row-1");
        click(cx, "card-opt-0");
        advance(cx, Duration::from_millis(UNDO_MS));

        let mut refused = fixture_recommending_the_second_option();
        refused.rev = 43;
        refused.acted = 1;
        refused.failed = vec![ActionFailure {
            seq: 1,
            key: Some("D7".to_owned()),
            text: "options changed".to_owned(),
        }];
        show(cx, refused);
        assert_eq!(status(cx).as_deref(), Some("D7 not sent: options changed"));
        assert!(
            bounds(cx, "card-answer").is_none(),
            "the optimistic answer is dropped"
        );
        assert!(bounds(cx, "card-opt-0").is_some(), "D7 is open again");
    }

    #[gpui::test]
    fn a_to_do_shows_done_and_its_link_which_opens_with_http(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        item_mut(&mut snapshot, "T12").link = Some("example.com/pull/41".to_owned());
        let (_view, _focused, opened, cx) = queued_recording_panel(cx, dir.path(), snapshot);

        assert!(bounds(cx, "card-done").is_some(), "T12 is the default pick");
        assert!(bounds(cx, "card-opt-0").is_none(), "a to-do has no options");
        assert!(
            bounds(cx, "card-dismiss").is_none(),
            "a to-do has no Dismiss"
        );
        click(cx, "card-link");
        assert_eq!(
            *opened.borrow(),
            vec!["http://example.com/pull/41".to_owned()]
        );
    }

    #[gpui::test]
    fn done_is_written_after_the_undo_window(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, _focused, _opened, cx) = queued_recording_panel(cx, dir.path(), fixture());
        click(cx, "card-done");
        assert!(
            bounds(cx, "card-answer").is_some(),
            "Done shows its answer at once"
        );
        assert_eq!(queued(dir.path()), None, "a held Done is not written yet");

        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(
            queued(dir.path()),
            Some(vec![(
                1,
                Action::Done {
                    key: "T12".to_owned()
                }
            )])
        );
    }

    #[gpui::test]
    fn a_news_item_shows_dismiss_which_is_written_after_the_undo_window(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, _focused, _opened, cx) = queued_recording_panel(cx, dir.path(), fixture());
        click(cx, "need-row-2");
        assert!(bounds(cx, "card-dismiss").is_some());
        assert!(bounds(cx, "card-done").is_none(), "news has no Done");
        assert!(bounds(cx, "card-opt-0").is_none(), "news has no options");

        click(cx, "card-dismiss");
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(
            queued(dir.path()),
            Some(vec![(
                1,
                Action::Dismiss {
                    key: "N4".to_owned()
                }
            )])
        );
    }

    #[gpui::test]
    fn next_picks_the_next_open_item(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (view, _focused, _opened, cx) = queued_recording_panel(cx, dir.path(), fixture());
        assert!(bounds(cx, "card-next").is_some(), "D7 comes after T12");
        click(cx, "card-next");

        assert!(bounds(cx, "need-row-1-picked").is_some(), "D7 is picked");
        let stored = view.read_with(cx, |panel, _| panel.picked().cloned());
        assert_eq!(stored, Some(BoardPick::Item("D7".to_owned())));
    }

    #[gpui::test]
    fn without_another_open_item_there_is_no_next(cx: &mut TestAppContext) {
        let mut snapshot = fixture();
        snapshot.items.retain(|item| item.key == "T12");
        let (_view, cx) = panel(cx, Some(snapshot), None, Some(bridge()));
        assert!(bounds(cx, "card-done").is_some());
        assert!(bounds(cx, "card-next").is_none(), "no other item is open");
    }

    #[gpui::test]
    fn answer_in_chat_sends_chat_and_focuses_the_bridge_pane(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, focused, _opened, cx) = queued_recording_panel(cx, dir.path(), fixture());
        click(cx, "need-row-1");
        click(cx, "card-chat");

        let chat = Action::Chat {
            key: "D7".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, chat)]));
        assert_eq!(
            *focused.borrow(),
            vec!["w1:p1".to_owned()],
            "the bridge pane is focused"
        );
    }

    #[gpui::test]
    fn answer_in_chat_without_a_bridge_pane_sends_but_focuses_nothing(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, focused, _opened, cx) = queued_recording_panel(cx, dir.path(), fixture());
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| board.set(Some(fixture()), None, None, cx))
        });
        cx.run_until_parked();

        click(cx, "need-row-1");
        click(cx, "card-chat");
        let chat = Action::Chat {
            key: "D7".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, chat)]));
        assert!(focused.borrow().is_empty(), "there is no pane to focus");
    }

    #[gpui::test]
    fn an_inert_board_dims_the_item_buttons_and_queues_nothing(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.actions = 0;
        let (_view, focused, opened, cx) = queued_recording_panel(cx, dir.path(), snapshot);
        assert!(
            bounds(cx, "card-inert").is_some(),
            "the foot says the mod takes no actions"
        );

        click(cx, "card-done");
        assert!(bounds(cx, "card-answer").is_none(), "Done does nothing");
        click(cx, "card-link");
        assert_eq!(
            *opened.borrow(),
            vec!["https://example.com/pull/41".to_owned()],
            "the link still opens"
        );
        click(cx, "card-next");
        assert!(
            bounds(cx, "need-row-1-picked").is_some(),
            "Next still picks"
        );
        click(cx, "card-opt-0");
        click(cx, "card-chat");

        assert_eq!(queued(dir.path()), None, "no actions file is written");
        assert_eq!(status(cx), None, "no status line is shown");
        assert!(
            focused.borrow().is_empty(),
            "Answer in chat focuses nothing"
        );
        assert!(bounds(cx, "card-opt-0").is_some(), "the options stay");
    }

    #[gpui::test]
    fn a_resolved_item_shows_its_answer_and_no_undo(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, _focused, _opened, cx) = queued_recording_panel(cx, dir.path(), fixture());
        click(cx, "need-row-3");
        assert!(bounds(cx, "card-answer").is_some(), "D6 shows its answer");
        assert!(bounds(cx, "card-undo").is_none());
        assert!(bounds(cx, "card-sending").is_none());
        assert!(
            bounds(cx, "card-opt-0").is_none(),
            "a resolved item has no options"
        );
    }

    /// An open item of `kind`, with the given `recommended`, for the key helpers.
    fn open_item(kind: &str, recommended: Option<usize>) -> NeedItem {
        let mut item: NeedItem = serde_json::from_str(&format!(
            r#"{{"key":"X1","kind":"{kind}","title":"Title","open":true,"options":["one","two"]}}"#
        ))
        .unwrap();
        item.recommended = recommended;
        item
    }

    #[test]
    fn the_card_item_is_the_picked_item_only() {
        let items = fixture().items;
        let picked = card_item(&items, Some(&BoardPick::Item("N4".into())));
        assert_eq!(picked.map(|item| item.key.as_str()), Some("N4"));
        assert!(card_item(&items, Some(&BoardPick::Item("D99".into()))).is_none());
        assert!(card_item(&items, Some(&BoardPick::Row(ROW_640.into()))).is_none());
        assert!(card_item(&items, None).is_none());
    }

    #[test]
    fn down_steps_through_the_items_and_stops_at_the_last() {
        let items = fixture().items;
        assert_eq!(
            stepped_pick(&items, Some(&BoardPick::Item("T12".into())), true),
            Some(BoardPick::Item("D7".into()))
        );
        assert_eq!(
            stepped_pick(&items, Some(&BoardPick::Item("D6".into())), true),
            None,
            "D6 is the last item"
        );
    }

    #[test]
    fn up_steps_back_and_stops_at_the_first() {
        let items = fixture().items;
        assert_eq!(
            stepped_pick(&items, Some(&BoardPick::Item("D7".into())), false),
            Some(BoardPick::Item("T12".into()))
        );
        assert_eq!(
            stepped_pick(&items, Some(&BoardPick::Item("T12".into())), false),
            None,
            "T12 is the first item"
        );
    }

    #[test]
    fn down_from_a_row_or_no_pick_goes_to_the_first_item_and_up_does_not_move() {
        let items = fixture().items;
        let row = BoardPick::Row(ROW_640.into());
        assert_eq!(
            stepped_pick(&items, Some(&row), true),
            Some(BoardPick::Item("T12".into()))
        );
        assert_eq!(stepped_pick(&items, Some(&row), false), None);
        assert_eq!(
            stepped_pick(&items, None, true),
            Some(BoardPick::Item("T12".into()))
        );
        assert_eq!(stepped_pick(&items, None, false), None);
    }

    #[test]
    fn with_no_items_no_key_moves_the_pick() {
        assert_eq!(stepped_pick(&[], None, true), None);
        assert_eq!(
            stepped_pick(&[], Some(&BoardPick::Item("D7".into())), false),
            None
        );
    }

    #[test]
    fn enter_sends_the_recommended_option_of_an_open_decision() {
        let items = fixture().items;
        assert_eq!(
            enter_action(&items[1]),
            Some(Action::Answer {
                key: "D7".into(),
                option: 0,
                label: "one line per row".into(),
            })
        );
    }

    #[test]
    fn enter_sends_nothing_for_a_decision_without_a_usable_recommendation() {
        assert_eq!(enter_action(&open_item("ask", None)), None);
        assert_eq!(
            enter_action(&open_item("ask", Some(7))),
            None,
            "the recommended option is out of range"
        );
    }

    #[test]
    fn enter_sends_done_for_a_to_do_and_dismiss_for_news() {
        let items = fixture().items;
        assert_eq!(
            enter_action(&items[0]),
            Some(Action::Done { key: "T12".into() })
        );
        assert_eq!(
            enter_action(&items[2]),
            Some(Action::Dismiss { key: "N4".into() })
        );
        assert_eq!(
            enter_action(&open_item("weird", None)),
            Some(Action::Dismiss { key: "X1".into() }),
            "an unknown kind reads as news"
        );
    }

    #[test]
    fn enter_sends_nothing_for_a_resolved_item() {
        let items = fixture().items;
        assert_eq!(enter_action(&items[3]), None, "D6 is resolved");
    }

    #[test]
    fn a_digit_sends_the_option_at_its_place_in_card_order() {
        let mut item = open_item("ask", Some(0));
        let answer = |option: usize, label: &str| Action::Answer {
            key: "X1".into(),
            option,
            label: label.into(),
        };
        assert_eq!(option_action(&item, 0), Some(answer(0, "one")));
        assert_eq!(option_action(&item, 1), Some(answer(1, "two")));
        assert_eq!(option_action(&item, 2), None, "there are two options");

        item.recommended = Some(1);
        assert_eq!(
            option_action(&item, 0),
            Some(answer(1, "two")),
            "the recommended option comes first"
        );
    }

    #[test]
    fn a_digit_sends_nothing_for_a_to_do_news_or_a_resolved_decision() {
        let items = fixture().items;
        assert_eq!(option_action(&items[0], 0), None, "a to-do has no options");
        assert_eq!(option_action(&items[2], 0), None, "news has no options");
        let mut resolved = open_item("ask", Some(0));
        resolved.open = false;
        assert_eq!(
            option_action(&resolved, 0),
            None,
            "a resolved item has none"
        );
    }

    #[test]
    fn the_item_card_label_names_the_item_its_kind_and_its_options() {
        let items = fixture().items;
        let options = |labels: [&str; 2]| -> Vec<OptionFace> {
            labels
                .into_iter()
                .enumerate()
                .map(|(position, label)| OptionFace {
                    label: label.to_owned(),
                    recommended: position == 0,
                    on_click: None,
                })
                .collect()
        };
        let decide = ItemActions::Decide {
            options: options(["one line per row", "two lines per row"]),
            chat: None,
        };
        assert_eq!(
            item_card_label(&items[1], &decide),
            "D7 Row layout, decide, options one line per row, two lines per row"
        );
        let to_do = ItemActions::Do {
            done: None,
            link: None,
        };
        assert_eq!(
            item_card_label(&items[0], &to_do),
            "T12 Test the bag drag, do"
        );
        let news = ItemActions::Know { dismiss: None };
        assert_eq!(
            item_card_label(&items[2], &news),
            "N4 CI is green on linux-pc, fyi"
        );
    }
}
