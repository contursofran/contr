//! The board panel: the header with the `Board | Report` switch and the wrap label, then the body of
//! the shown tab. The Board tab draws the summary line, the work section, the needs-you section and the
//! answer card at the foot. The Report tab draws the report of the row it follows (`ReportView`) and
//! nothing else. The panel reads the `Board` entity and redraws when that entity notifies. A report is
//! read only while the Report tab is shown. The pick (a row or an item) is kept here: the card shows it,
//! and a click on a row or an item sets it.

use crate::agents_list::{FocusFn, focus_in_background};
use crate::board::{Board, GlobalBoard};
use crate::board_actions::{Action, card_options, next_open};
use crate::board_card::{
    BoardPick, CardFace, ClickFn, ItemActions, ItemFace, OptionFace, RowFace, answer_card,
    card_item, click_fn, effective_pick, enter_action, link_url, names_on_board, option_action,
    resolved_answer, shown_item, stepped_pick, tab_label_for_pane,
};
use crate::board_model::{BoardSnapshot, ItemKind, NeedItem, WorkRow};
use crate::board_needs::{PickFn, needs_section};
use crate::board_outbox::{BoardOutbox, GlobalBoardOutbox, now_ms, show_status};
use crate::board_report::{ReportEvent, ReportSource, ReportTarget, ReportView};
use crate::board_work::{WorkRowEvent, WorkRowView, work_section};
use crate::herdr::GlobalHerdr;
use crate::theme::*;
use contr_herdr::HerdrTree;
use gpui::{
    App, ClickEvent, Context, Div, Entity, FocusHandle, IntoElement, KeyBinding, MouseButton,
    MouseDownEvent, ParentElement, Render, Role, Stateful, Styled, Subscription, WeakEntity,
    Window, actions, div, prelude::*, px, rgb, transparent_black,
};
use std::rc::Rc;

actions!(
    board_panel,
    [
        PickUp,
        PickDown,
        AnswerDefault,
        PickOption1,
        PickOption2,
        PickOption3,
        PickOption4
    ]
);

/// The key context of the panel. The key bindings name it, and the panel's root sets it.
const KEY_CONTEXT: &str = "BoardPanel";

/// The two tabs of the panel. `Report` shows the report of the row in `report_row`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum PanelTab {
    #[default]
    Board,
    Report,
}

/// Opens a URL. The app opens it in the system browser; a test records it.
pub type OpenFn = Rc<dyn Fn(String, &mut App)>;

/// A click on a board control that sends an action. Built by `sends`.
pub(crate) type ActionClick = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

/// The status line after `clear done` is sent.
const CLEAR_SENT: &str = "clear done sent";

/// The board panel view.
pub struct BoardPanel {
    board: Entity<Board>,
    /// The queue that sends actions to the coordinator mod. `None` when the app has none, and then
    /// no control sends anything.
    outbox: Option<Entity<BoardOutbox>>,
    tab: PanelTab,
    /// One view per work row, in the coordinator's order.
    work: Vec<Entity<WorkRowView>>,
    /// The row clicks of `work`. They are replaced with the views.
    _row_picks: Vec<Subscription>,
    /// The picked row or item. `None` shows the first open item. A pick whose key is gone is dropped.
    pick: Option<BoardPick>,
    _observe: Subscription,
    /// Redraws the card's Focus label when the herdr tree changes.
    _observe_herdr: Subscription,
    /// Redraws the panel when the queue changes, such as when it loads and can act.
    _outbox_observe: Option<Subscription>,
    /// The Report tab's view. It is drawn only on the Report tab.
    report: Entity<ReportView>,
    /// The key of the work row that the Report tab follows. It stays when the tab is left.
    report_row: Option<String>,
    _report_events: Subscription,
    /// Focuses a pane, for the card's `Focus` button and `Answer in chat`.
    focus_pane: FocusFn,
    /// Opens a URL, for the card's `Open PR` button.
    open: OpenFn,
    /// The panel's keyboard focus. A click in the panel takes it. The keys act only while it is held.
    focus: FocusHandle,
}

impl BoardPanel {
    /// Needs `Board::init` and `Herdr::init` first. Redraws when the board or the herdr tree changes.
    /// On the Report tab, a board revision also syncs the report. Focus and Open PR use the app's
    /// calls. Uses the queue from `BoardOutbox::init`, when there is one.
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self::with_actions(Rc::new(focus_in_background), Rc::new(open_in_browser), cx)
    }

    /// Binds the panel's keys in its key context: ↑ and ↓ move the pick, enter sends the default action,
    /// and 1 to 4 send an option. Called once at start-up. The keys act only while the panel has focus.
    pub fn bind_keys(cx: &mut App) {
        cx.bind_keys([
            KeyBinding::new("up", PickUp, Some(KEY_CONTEXT)),
            KeyBinding::new("down", PickDown, Some(KEY_CONTEXT)),
            KeyBinding::new("enter", AnswerDefault, Some(KEY_CONTEXT)),
            KeyBinding::new("1", PickOption1, Some(KEY_CONTEXT)),
            KeyBinding::new("2", PickOption2, Some(KEY_CONTEXT)),
            KeyBinding::new("3", PickOption3, Some(KEY_CONTEXT)),
            KeyBinding::new("4", PickOption4, Some(KEY_CONTEXT)),
        ]);
    }

    /// The panel's keyboard focus, for the dev control's `board-panel` click.
    #[cfg(debug_assertions)]
    pub(crate) fn focus_handle(&self) -> &FocusHandle {
        &self.focus
    }

    /// The tab the panel shows, for the dev control's state.
    #[cfg(debug_assertions)]
    pub(crate) fn tab(&self) -> PanelTab {
        self.tab
    }

    /// The board the panel shows. The dev control's state reads its snapshot, problem and bridge.
    #[cfg(debug_assertions)]
    pub(crate) fn board(&self) -> &Entity<Board> {
        &self.board
    }

    /// The pick the card shows on `snapshot`: the held pick, or the fall-back. For the dev control's
    /// state.
    #[cfg(debug_assertions)]
    pub(crate) fn shown_pick(&self, snapshot: &BoardSnapshot) -> Option<BoardPick> {
        effective_pick(snapshot, self.pick.as_ref())
    }

    /// Whether the board takes actions now: a queue is held and the board is live. The panel's
    /// buttons send only when this is true.
    pub(crate) fn can_act(&self, cx: &App) -> bool {
        self.outbox
            .as_ref()
            .is_some_and(|outbox| outbox.read(cx).can_act())
    }

    /// `new` with the Focus and Open PR calls given, so the tests can record them.
    fn with_actions(focus_pane: FocusFn, open: OpenFn, cx: &mut Context<Self>) -> Self {
        let board = cx.global::<GlobalBoard>().0.clone();
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let observe = cx.observe(&board, |this, _, cx| {
            this.sync_work(cx);
            if this.tab == PanelTab::Report {
                this.sync_report(cx);
            }
        });
        let observe_herdr = cx.observe(&herdr, |_, _, cx| cx.notify());
        let outbox = cx
            .try_global::<GlobalBoardOutbox>()
            .map(|global| global.0.clone());
        let outbox_observe = outbox
            .as_ref()
            .map(|outbox| cx.observe(outbox, |_, _, cx| cx.notify()));
        let report = cx.new(ReportView::new);
        let report_events = cx.subscribe(&report, |this, _, event: &ReportEvent, cx| match event {
            ReportEvent::Back => this.open_board_tab(cx),
        });
        let focus = cx.focus_handle();
        let mut panel = Self {
            board,
            outbox,
            tab: PanelTab::default(),
            work: Vec::new(),
            _row_picks: Vec::new(),
            pick: None,
            _observe: observe,
            _observe_herdr: observe_herdr,
            _outbox_observe: outbox_observe,
            report,
            report_row: None,
            _report_events: report_events,
            focus_pane,
            open,
            focus,
        };
        panel.sync_work(cx);
        panel
    }

    /// Sends `action` through the queue. A sent action shows `feedback`, when there is one, in the
    /// status bar. Returns `false`, and shows nothing, when the queue sends nothing: no queue, or a
    /// board that takes no actions.
    fn send_action(
        &mut self,
        action: Action,
        feedback: Option<&'static str>,
        cx: &mut Context<Self>,
    ) -> bool {
        let Some(outbox) = self.outbox.clone() else {
            return false;
        };
        let sent = outbox.update(cx, |outbox, cx| outbox.send(action, cx));
        if let (true, Some(feedback)) = (sent, feedback) {
            show_status(feedback.to_owned(), cx);
        }
        sent
    }

    /// The wrap label's click: sends the other wrap state, with its status line. Nothing is sent
    /// without a snapshot.
    pub(crate) fn send_wrap(&mut self, cx: &mut Context<Self>) {
        let Some(snapshot) = self.board.read(cx).snapshot.clone() else {
            return;
        };
        let (action, feedback) = wrap_flip(snapshot.wrap_up);
        self.send_action(action, Some(feedback), cx);
    }

    /// The `clear done` click of the work section (`rows`) or of the needs-you section. Sends the
    /// clear, with its status line.
    pub(crate) fn send_clear(&mut self, rows: bool, cx: &mut Context<Self>) {
        let action = if rows {
            Action::ClearRows
        } else {
            Action::ClearItems
        };
        self.send_action(action, Some(CLEAR_SENT), cx);
    }

    /// Shows the Report tab for the work row `row_key`. The card's Report button calls it.
    pub fn show_report(&mut self, row_key: String, cx: &mut Context<Self>) {
        self.report_row = Some(row_key);
        self.enter_report_tab(cx);
    }

    /// Picks `pick`: the card shows it, and the picked work row draws as selected.
    pub(crate) fn set_pick(&mut self, pick: BoardPick, cx: &mut Context<Self>) {
        self.pick = Some(pick);
        self.sync_picks(cx);
        cx.notify();
    }

    /// The Report tab's click. A picked work row becomes the row the tab follows. Otherwise the tab
    /// keeps the row it followed before, if any.
    pub(crate) fn open_report_tab(&mut self, cx: &mut Context<Self>) {
        if let Some(BoardPick::Row(key)) = &self.pick {
            self.report_row = Some(key.clone());
        }
        self.enter_report_tab(cx);
    }

    /// Shows the Report tab for the row that `report_row` names.
    fn enter_report_tab(&mut self, cx: &mut Context<Self>) {
        self.tab = PanelTab::Report;
        self.sync_report(cx);
        cx.notify();
    }

    /// The Board tab's click, and `back` in the report. The report row stays.
    pub(crate) fn open_board_tab(&mut self, cx: &mut Context<Self>) {
        self.tab = PanelTab::Board;
        cx.notify();
    }

    /// Gives the report view the row it follows, as the board is now. The view reads the file only
    /// when its path or the board revision is new. With no snapshot there is no report to show, so
    /// nothing is set.
    fn sync_report(&mut self, cx: &mut Context<Self>) {
        let Some(snapshot) = self.board.read(cx).snapshot.clone() else {
            return;
        };
        let target = report_target(&snapshot, self.report_row.as_deref());
        let rev = snapshot.rev;
        self.report
            .update(cx, |report, cx| report.show(target, rev, cx));
    }

    /// Brings the work rows in line with the board. When the keys match in order, only the rows that
    /// changed are updated, and only they redraw. The panel itself always redraws, because the
    /// summary and the needs section can change too. Otherwise the views are rebuilt. A pick whose
    /// key is no longer on the board is dropped, so the card falls back to the first open item.
    fn sync_work(&mut self, cx: &mut Context<Self>) {
        let snapshot = self.board.read(cx).snapshot.clone();
        let rows: &[WorkRow] = snapshot
            .as_ref()
            .map(|shown| shown.rows.as_slice())
            .unwrap_or(&[]);
        let same_keys = self.work.len() == rows.len()
            && self
                .work
                .iter()
                .zip(rows)
                .all(|(view, row)| view.read(cx).key() == row.key);
        if same_keys {
            for (view, row) in self.work.iter().zip(rows) {
                view.update(cx, |view, cx| view.set_row(row, cx));
            }
        } else {
            self.work = rows
                .iter()
                .enumerate()
                .map(|(index, row)| cx.new(|_| WorkRowView::new(row.clone(), index)))
                .collect();
            self._row_picks = self
                .work
                .iter()
                .map(|view| {
                    cx.subscribe(view, |this, _, event: &WorkRowEvent, cx| match event {
                        WorkRowEvent::Picked(key) => {
                            this.set_pick(BoardPick::Row(key.clone()), cx);
                        }
                    })
                })
                .collect();
        }
        let gone = match (snapshot.as_deref(), self.pick.as_ref()) {
            (Some(shown), Some(pick)) => !names_on_board(shown, pick),
            _ => false,
        };
        if gone {
            self.pick = None;
        }
        self.sync_picks(cx);
        cx.notify();
    }

    /// Marks the work row that the card names as picked, and no other. The needs section draws its
    /// own picked item, so only rows are set here.
    fn sync_picks(&mut self, cx: &mut Context<Self>) {
        let snapshot = self.board.read(cx).snapshot.clone();
        let shown = snapshot
            .as_deref()
            .and_then(|shown| effective_pick(shown, self.pick.as_ref()));
        let picked_row = match shown {
            Some(BoardPick::Row(key)) => Some(key),
            _ => None,
        };
        for view in &self.work {
            let picked = picked_row.as_deref() == Some(view.read(cx).key());
            view.update(cx, |view, cx| view.set_picked(picked, cx));
        }
    }

    /// The answer queued for `key`, and whether it can still be undone. `None` when nothing is queued.
    fn queued(&self, key: &str, cx: &App) -> Option<(String, bool)> {
        self.outbox.as_ref()?.read(cx).pending_answer(key)
    }

    /// Cancels the queued answer, done or dismiss for `key`, if it is still held.
    fn undo_answer(&mut self, key: &str, cx: &mut Context<Self>) {
        if let Some(outbox) = self.outbox.clone() {
            outbox.update(cx, |outbox, cx| outbox.undo(key, cx));
        }
    }

    /// `Answer in chat`: sends `Chat` for `key`, then focuses the bridge pane, so the owner lands in the
    /// coordinator's prompt. Nothing is focused when the send fails or the board has no bridge pane.
    fn chat(&mut self, key: String, cx: &mut Context<Self>) {
        if !self.send_action(Action::Chat { key }, None, cx) {
            return;
        }
        let pane = self
            .board
            .read(cx)
            .bridge
            .as_ref()
            .map(|bridge| bridge.pane_id.clone());
        if let Some(pane) = pane {
            (self.focus_pane)(pane, cx);
        }
    }

    /// Moves the pick one item up or, with `down`, down. See `stepped_pick`. Nothing moves on the Report
    /// tab, or without a snapshot.
    fn step_pick(&mut self, down: bool, cx: &mut Context<Self>) {
        if self.tab == PanelTab::Report {
            return;
        }
        let Some(snapshot) = self.board.read(cx).snapshot.clone() else {
            return;
        };
        let items = self.shown_items(&snapshot, cx);
        let shown = effective_pick(&snapshot, self.pick.as_ref());
        if let Some(pick) = stepped_pick(&items, shown.as_ref(), down) {
            self.set_pick(pick, cx);
        }
    }

    /// Sends what `key_action` gives for the item the card shows, as the card's buttons send theirs.
    /// Nothing is sent on the Report tab, when the card shows no item, or when the board takes no
    /// actions (`send_action` then returns false).
    fn send_for_card_item(
        &mut self,
        key_action: impl Fn(&NeedItem) -> Option<Action>,
        cx: &mut Context<Self>,
    ) {
        if self.tab == PanelTab::Report {
            return;
        }
        let Some(snapshot) = self.board.read(cx).snapshot.clone() else {
            return;
        };
        let items = self.shown_items(&snapshot, cx);
        let shown = effective_pick(&snapshot, self.pick.as_ref());
        if let Some(action) = card_item(&items, shown.as_ref()).and_then(key_action) {
            self.send_action(action, None, cx);
        }
    }

    /// `enter`: the card's default action. See `enter_action`.
    fn send_default(&mut self, cx: &mut Context<Self>) {
        self.send_for_card_item(enter_action, cx);
    }

    /// The digit key for `position` (0 for `1`): the option at that place in the card's order.
    fn send_option(&mut self, position: usize, cx: &mut Context<Self>) {
        self.send_for_card_item(|item| option_action(item, position), cx);
    }

    /// The items as the panel draws them: an item with an answer queued reads as resolved, with that
    /// answer. Every other item is as the board has it.
    pub(crate) fn shown_items(&self, snapshot: &BoardSnapshot, cx: &App) -> Vec<NeedItem> {
        snapshot
            .items
            .iter()
            .map(|item| {
                let queued = self.queued(&item.key, cx);
                shown_item(item, queued.as_ref().map(|(answer, _)| answer.as_str()))
            })
            .collect()
    }

    /// What the card shows for `shown`, the pick that `effective_pick` resolved on `snapshot`. `items`
    /// are the items as shown (see `shown_items`).
    fn card_face(
        &self,
        snapshot: &BoardSnapshot,
        items: &[NeedItem],
        shown: Option<&BoardPick>,
        tree: Option<&HerdrTree>,
        can_act: bool,
        cx: &Context<Self>,
    ) -> CardFace {
        match shown {
            Some(BoardPick::Item(key)) => items
                .iter()
                .find(|item| item.key == *key)
                .map_or(CardFace::Empty, |item| {
                    CardFace::Item(self.item_face(item, items, can_act, cx))
                }),
            Some(BoardPick::Row(key)) => match snapshot.rows.iter().find(|row| row.key == *key) {
                Some(row) => CardFace::Row(self.row_face(row, tree, cx)),
                None => CardFace::Empty,
            },
            None => CardFace::Empty,
        }
    }

    /// The face of the item card for `item`, one of `items` as shown. The actions follow the item's
    /// state: an answer queued for it, then an open decision, to-do or news item, else a resolved item.
    /// With `can_act` false the buttons that send an action have no click. Undo, Open link and Next
    /// still work, because they do not send.
    fn item_face(
        &self,
        item: &NeedItem,
        items: &[NeedItem],
        can_act: bool,
        cx: &Context<Self>,
    ) -> ItemFace {
        let actions = match self.queued(&item.key, cx) {
            Some((_, undoable)) => ItemActions::Resolved {
                answer: resolved_answer(item),
                undo: undoable.then(|| self.undo_click(item.key.clone(), cx)),
                sending: !undoable,
            },
            None if !item.open => ItemActions::Resolved {
                answer: resolved_answer(item),
                undo: None,
                sending: false,
            },
            None => match item.kind {
                ItemKind::Ask => ItemActions::Decide {
                    options: card_options(item)
                        .into_iter()
                        .map(|(index, label, recommended)| OptionFace {
                            label: label.to_owned(),
                            recommended,
                            on_click: can_act.then(|| {
                                self.sends_click(
                                    Action::Answer {
                                        key: item.key.clone(),
                                        option: index,
                                        label: label.to_owned(),
                                    },
                                    cx,
                                )
                            }),
                        })
                        .collect(),
                    chat: can_act.then(|| self.chat_click(item.key.clone(), cx)),
                },
                ItemKind::Do => ItemActions::Do {
                    done: can_act.then(|| {
                        self.sends_click(
                            Action::Done {
                                key: item.key.clone(),
                            },
                            cx,
                        )
                    }),
                    link: item
                        .link
                        .as_deref()
                        .map(|link| self.open_click(link_url(link))),
                },
                ItemKind::Know | ItemKind::Other => ItemActions::Know {
                    dismiss: can_act.then(|| {
                        self.sends_click(
                            Action::Dismiss {
                                key: item.key.clone(),
                            },
                            cx,
                        )
                    }),
                },
            },
        };
        let next = next_open(items, &item.key, |other| other.open).map(|next| {
            let pick = BoardPick::Item(next.key.clone());
            (next.key.clone(), self.pick_click(pick, cx))
        });
        ItemFace {
            item: item.clone(),
            actions,
            next,
            inert: !can_act,
        }
    }

    /// A click that sends `action` through the queue, with no status line.
    fn sends_click(&self, action: Action, cx: &Context<Self>) -> ClickFn {
        click_fn(cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.send_action(action.clone(), None, cx);
        }))
    }

    /// A click on `Answer in chat` for `key`.
    fn chat_click(&self, key: String, cx: &Context<Self>) -> ClickFn {
        click_fn(cx.listener(move |this, _: &ClickEvent, _, cx| this.chat(key.clone(), cx)))
    }

    /// A click on `undo` for the answer queued for `key`.
    fn undo_click(&self, key: String, cx: &Context<Self>) -> ClickFn {
        click_fn(cx.listener(move |this, _: &ClickEvent, _, cx| this.undo_answer(&key, cx)))
    }

    /// A click that picks `pick`, as a click on a row does.
    fn pick_click(&self, pick: BoardPick, cx: &Context<Self>) -> ClickFn {
        click_fn(cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.set_pick(pick.clone(), cx);
        }))
    }

    /// A click that opens `url` through the panel's Open PR call (the app opens it in the browser).
    fn open_click(&self, url: String) -> ClickFn {
        let open = self.open.clone();
        click_fn(move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
            open(url.clone(), cx);
        })
    }

    /// The buttons of a work row. Focus needs a pane that the tree holds in a tab, Report needs a
    /// report, and Open PR needs a URL. Each button's click calls the panel's Focus or Open PR call,
    /// or shows the report.
    fn row_face(&self, row: &WorkRow, tree: Option<&HerdrTree>, cx: &Context<Self>) -> RowFace {
        let focus = row.pane_id.as_deref().and_then(|pane| {
            let tab = tree.and_then(|tree| tab_label_for_pane(tree, pane))?;
            let focus = self.focus_pane.clone();
            let pane = pane.to_owned();
            let on_click = click_fn(move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
                focus(pane.clone(), cx);
            });
            Some((tab, on_click))
        });
        let report = row.report.is_some().then(|| {
            let key = row.key.clone();
            click_fn(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.show_report(key.clone(), cx);
            }))
        });
        let open_pr = row.url.clone().map(|url| self.open_click(url));
        RowFace {
            row: row.clone(),
            focus,
            report,
            open_pr,
        }
    }

    /// The work row views, in order. Tests read them.
    #[cfg(test)]
    pub(crate) fn work_rows(&self) -> &[Entity<WorkRowView>] {
        &self.work
    }

    /// The pick as the panel holds it, before the fall-back. Tests read it.
    #[cfg(test)]
    pub(crate) fn picked(&self) -> Option<&BoardPick> {
        self.pick.as_ref()
    }

    /// The key of the work row that the Report tab follows. Tests read it.
    #[cfg(test)]
    pub(crate) fn report_key(&self) -> Option<&str> {
        self.report_row.as_deref()
    }
}

impl Render for BoardPanel {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let board = self.board.read(cx);
        let bridge = board.bridge.is_some();
        let problem = board.problem.clone();
        let snapshot = board.snapshot.clone();
        let tree = cx.global::<GlobalHerdr>().0.read(cx).live.tree.clone();
        // The pick, the items and the card come first, so every use of `cx` for them is done
        // before the header.
        let shown = snapshot
            .as_deref()
            .and_then(|current| effective_pick(current, self.pick.as_ref()));
        let can_act = self.can_act(cx);
        let items = snapshot
            .as_deref()
            .map(|current| self.shown_items(current, cx))
            .unwrap_or_default();
        let face = match (snapshot.as_deref(), self.tab) {
            (Some(current), PanelTab::Board) => self.card_face(
                current,
                &items,
                shown.as_ref(),
                tree.as_deref(),
                can_act,
                cx,
            ),
            _ => CardFace::Empty,
        };
        let picked_item = match &shown {
            Some(BoardPick::Item(key)) => Some(key.clone()),
            _ => None,
        };
        let pick_item = item_pick(cx.weak_entity());
        let wrap_up = snapshot.as_ref().map(|current| current.wrap_up);
        let on_wrap = wrap_up.filter(|_| can_act).map(|_| wrap_click(cx));
        let root = div()
            .debug_selector(|| "board-panel".into())
            .size_full()
            .flex()
            .flex_col()
            .key_context(KEY_CONTEXT)
            .track_focus(&self.focus)
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, _: &MouseDownEvent, window, cx| {
                    window.focus(&this.focus, cx);
                }),
            )
            .on_action(cx.listener(|this, _: &PickUp, _, cx| this.step_pick(false, cx)))
            .on_action(cx.listener(|this, _: &PickDown, _, cx| this.step_pick(true, cx)))
            .on_action(cx.listener(|this, _: &AnswerDefault, _, cx| this.send_default(cx)))
            .on_action(cx.listener(|this, _: &PickOption1, _, cx| this.send_option(0, cx)))
            .on_action(cx.listener(|this, _: &PickOption2, _, cx| this.send_option(1, cx)))
            .on_action(cx.listener(|this, _: &PickOption3, _, cx| this.send_option(2, cx)))
            .on_action(cx.listener(|this, _: &PickOption4, _, cx| this.send_option(3, cx)));
        // The header is built after the listeners: it borrows `cx` for as long as it lives.
        let head = header(self.tab, wrap_up, on_wrap, cx);
        let root = root.child(head);
        match (snapshot, self.tab) {
            (None, _) => root.child(text_line(
                "board-empty",
                empty_text(bridge, problem.as_deref()),
                TEXT_DIM,
            )),
            (Some(_), PanelTab::Report) => root.child(report_area(self.report.clone())),
            (Some(snapshot), PanelTab::Board) => {
                let on_clear_rows = can_act.then(|| clear_click(true, cx));
                let on_clear_items = can_act.then(|| clear_click(false, cx));
                root.child(summary(&snapshot, problem.as_deref()))
                    .child(work_section(&self.work, snapshot.clear_rows, on_clear_rows))
                    .child(needs_section(
                        &items,
                        snapshot.need_count,
                        snapshot.clear_items,
                        picked_item.as_deref(),
                        pick_item,
                        on_clear_items,
                    ))
                    .child(answer_card(face, now_ms()))
            }
        }
    }
}

/// The needs-you rows' pick: a click sets the panel's pick to the item with the key it is given.
fn item_pick(panel: WeakEntity<BoardPanel>) -> PickFn {
    Rc::new(move |key: String, cx: &mut App| {
        let _ = panel.update(cx, |panel, cx| panel.set_pick(BoardPick::Item(key), cx));
    })
}

/// Opens `url` in the system browser, as `App::open_url` does.
fn open_in_browser(url: String, cx: &mut App) {
    cx.open_url(&url);
}

/// The click handler of the wrap label: `send_wrap`.
fn wrap_click(cx: &Context<BoardPanel>) -> ActionClick {
    Box::new(cx.listener(|this, _: &ClickEvent, _, cx| this.send_wrap(cx)))
}

/// The click handler of a `clear done` control: `send_clear` for the rows (`rows`) or the items.
fn clear_click(rows: bool, cx: &Context<BoardPanel>) -> ActionClick {
    Box::new(cx.listener(move |this, _: &ClickEvent, _, cx| this.send_clear(rows, cx)))
}

/// The action that flips the wrap state, and its status line. From wrap up, the action is
/// `keep going`; from keep going, it is `wrap up`.
fn wrap_flip(wrap_up: bool) -> (Action, &'static str) {
    if wrap_up {
        (Action::WrapUp { on: false }, "keep going sent")
    } else {
        (Action::WrapUp { on: true }, "wrap up sent")
    }
}

/// The `clear done` control of a section head. Lit and clickable when `on_click` is set. Dim and
/// inert when it is `None`, as the board takes no actions.
pub(crate) fn clear_done(selector: &'static str, on_click: Option<ActionClick>) -> Stateful<Div> {
    let control = div()
        .id(selector)
        .debug_selector(move || selector.to_owned())
        .child("clear done");
    match on_click {
        Some(on_click) => control
            .role(Role::Button)
            .aria_label("clear done")
            .text_color(rgb(TEXT_2))
            .hover(|style| style.text_color(rgb(TEXT)))
            .cursor_pointer()
            .on_click(on_click),
        None => control.text_color(rgb(TEXT_DIM_2)),
    }
}

/// The header row. `wrap_up` is `None` while there is no snapshot: the wrap label is then hidden.
/// `on_wrap` makes the label a button.
fn header(
    tab: PanelTab,
    wrap_up: Option<bool>,
    on_wrap: Option<ActionClick>,
    cx: &mut Context<BoardPanel>,
) -> impl IntoElement {
    div()
        .debug_selector(|| "board-header".into())
        .h(px(PANEL_HEADER_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .justify_between()
        .px(px(PANEL_PAD_X))
        .child(
            div()
                .debug_selector(|| "board-switch".into())
                .flex()
                .items_center()
                .child(segment(
                    "board-tab-board",
                    "Board",
                    tab == PanelTab::Board,
                    cx.listener(|this, _: &ClickEvent, _, cx| this.open_board_tab(cx)),
                ))
                .child(segment(
                    "board-tab-report",
                    "Report",
                    tab == PanelTab::Report,
                    cx.listener(|this, _: &ClickEvent, _, cx| this.open_report_tab(cx)),
                )),
        )
        .when_some(wrap_up, |row, wrap_up| {
            row.child(wrap_label(wrap_up, on_wrap))
        })
}

/// One segment of the switch. The selected one is a card with a 1 px border. The border is always
/// there (transparent when not selected), so the labels do not move when the selection does. The
/// selected segment's selector ends in `-on`, so a test can read which tab is shown.
fn segment(
    selector: &'static str,
    label: &'static str,
    selected: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let shown = if selected {
        format!("{selector}-on")
    } else {
        selector.to_owned()
    };
    div()
        .id(selector)
        .debug_selector(move || shown)
        .cursor_pointer()
        .px(px(SEGMENT_PAD_X))
        .py(px(SEGMENT_PAD_Y))
        .rounded(px(SEGMENT_RADIUS))
        .border_1()
        .border_color(if selected {
            rgb(CARD_BORDER).into()
        } else {
            transparent_black()
        })
        .when(selected, |tab| tab.bg(rgb(BG_CARD)).text_color(rgb(TEXT)))
        .when(!selected, |tab| tab.text_color(rgb(TEXT_DIM)))
        .on_click(on_click)
        .child(label)
}

/// The Report tab's body: the report view under the header, filling the rest of the panel.
fn report_area(report: Entity<ReportView>) -> impl IntoElement {
    div().flex_1().min_h_0().flex().flex_col().child(report)
}

/// What the Report tab shows for the row `key` on `snapshot`: nothing without a key, the row's
/// fields while the row is on the board, else `Gone`.
fn report_target(snapshot: &BoardSnapshot, key: Option<&str>) -> ReportTarget {
    let Some(key) = key else {
        return ReportTarget::None;
    };
    match snapshot.rows.iter().find(|row| row.key == key) {
        Some(row) => ReportTarget::Row(ReportSource::from_row(row)),
        None => ReportTarget::Gone,
    }
}

/// The wrap label on the right of the header. It is a button when `on_click` is set, and dim and
/// inert when it is `None`, as the board takes no actions.
fn wrap_label(wrap_up: bool, on_click: Option<ActionClick>) -> impl IntoElement {
    let selector = if wrap_up {
        "board-wrap-keep"
    } else {
        "board-wrap-up"
    };
    let label = div()
        .id("board-wrap")
        .debug_selector(|| "board-wrap".into())
        .child(
            div()
                .debug_selector(move || selector.to_owned())
                .child(wrap_text(wrap_up)),
        );
    match on_click {
        Some(on_click) => label
            .role(Role::Button)
            .aria_label(wrap_text(wrap_up))
            .text_color(rgb(TEXT_2))
            .hover(|style| style.text_color(rgb(TEXT)))
            .cursor_pointer()
            .on_click(on_click),
        None => label.text_color(rgb(TEXT_DIM)),
    }
}

/// The summary line. A problem with a snapshot is shown after it, in the dim colour.
fn summary(snapshot: &BoardSnapshot, problem: Option<&str>) -> impl IntoElement {
    let colour = if problem.is_some() { TEXT_DIM } else { TEXT_2 };
    text_line(
        "board-summary",
        summary_text(&snapshot.summary, problem),
        colour,
    )
}

/// One line of text across the panel: `BOARD_LINE_HEIGHT` tall, padded, and cut with an ellipsis.
pub(crate) fn text_line(selector: &'static str, text: String, colour: u32) -> impl IntoElement {
    div()
        .debug_selector(move || selector.to_owned())
        .h(px(BOARD_LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .px(px(PANEL_PAD_X))
        .text_color(rgb(colour))
        .child(
            div()
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .whitespace_nowrap()
                .child(text),
        )
}

/// The line in place of the board while there is no snapshot: no bridge, a problem, or a read in
/// progress.
pub(crate) fn empty_text(bridge: bool, problem: Option<&str>) -> String {
    match (bridge, problem) {
        (false, _) => "no coordinator board".to_owned(),
        (true, Some(problem)) => problem.to_owned(),
        (true, None) => "loading board…".to_owned(),
    }
}

/// The summary line text. With a problem, the problem follows the summary after " · ". With an
/// empty summary, the problem is the whole line.
pub(crate) fn summary_text(summary: &str, problem: Option<&str>) -> String {
    match problem {
        None => summary.to_owned(),
        Some(problem) if summary.is_empty() => problem.to_owned(),
        Some(problem) => format!("{summary} · {problem}"),
    }
}

/// The wrap label: `keep going` while the coordinator wraps up, else `wrap up`.
pub(crate) fn wrap_text(wrap_up: bool) -> &'static str {
    if wrap_up { "keep going" } else { "wrap up" }
}

/// Helpers for the panel's tests here and in `board_work`: the panel in a frame-sized column.
#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
pub(crate) mod test_support {
    use super::*;
    use crate::board::{BridgePane, init_detached};
    use crate::board_actions::{actions_path, parse_actions};
    use crate::board_model::{BridgeRef, parse_board};
    use crate::herdr::{GlobalHerdr, Herdr};
    use crate::status_message::{GlobalStatusMessage, StatusMessage};
    use contr_herdr::LiveHerdr;
    use gpui::{Bounds, Pixels, TestAppContext, VisualTestContext, size};
    use std::sync::Arc;

    pub(crate) const FIXTURE: &str = include_str!("board_fixture_v1.json");

    pub(crate) fn fixture() -> BoardSnapshot {
        parse_board(FIXTURE.as_bytes()).unwrap()
    }

    pub(crate) fn bridge() -> BridgePane {
        BridgePane {
            pane_id: "w1:p1".into(),
            bridge: BridgeRef {
                session: "s1".into(),
                rev: 1,
            },
        }
    }

    /// Hosts the panel in a column of the frame's width, as the frame does, so the panel's bounds
    /// are the real ones in a window of the reference size.
    pub(crate) struct InFrame {
        panel: Entity<BoardPanel>,
    }

    impl Render for InFrame {
        fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
            div().size_full().flex().child(
                div()
                    .w(px(PANEL_WIDTH))
                    .h_full()
                    .flex_none()
                    .child(self.panel.clone()),
            )
        }
    }

    /// A Focus or Open PR call that does nothing, for panels whose tests do not record them.
    pub(crate) fn ignored_focus() -> FocusFn {
        Rc::new(|_: String, _: &mut App| {})
    }

    /// An Open PR call that does nothing.
    pub(crate) fn ignored_open() -> OpenFn {
        Rc::new(|_: String, _: &mut App| {})
    }

    /// A panel over a board that shows `snapshot`, `problem` and `bridge` (set by the test, not read
    /// from a file). The herdr tree is empty, and Focus and Open PR do nothing.
    pub(crate) fn panel<'a>(
        cx: &'a mut TestAppContext,
        snapshot: Option<BoardSnapshot>,
        problem: Option<&str>,
        bridge: Option<BridgePane>,
    ) -> (Entity<BoardPanel>, &'a mut VisualTestContext) {
        panel_on(
            cx,
            snapshot,
            problem,
            bridge,
            None,
            ignored_focus(),
            ignored_open(),
        )
    }

    /// `panel`, with the herdr `tree` (for the Focus labels) and the Focus and Open PR calls given.
    pub(crate) fn panel_on<'a>(
        cx: &'a mut TestAppContext,
        snapshot: Option<BoardSnapshot>,
        problem: Option<&str>,
        bridge: Option<BridgePane>,
        tree: Option<HerdrTree>,
        focus: FocusFn,
        open: OpenFn,
    ) -> (Entity<BoardPanel>, &'a mut VisualTestContext) {
        cx.update(|app| {
            let live = LiveHerdr {
                tree: tree.map(Arc::new),
                ..LiveHerdr::default()
            };
            let herdr = app.new(|cx| Herdr::detached(live, cx));
            app.set_global(GlobalHerdr(herdr));
            init_detached(app);
            BoardPanel::bind_keys(app);
        });
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        let problem = problem.map(str::to_owned);
        cx.update(|app| {
            board.update(app, |board, cx| board.set(snapshot, problem, bridge, cx));
        });
        open_in_window(cx, focus, open)
    }

    /// A panel over a board that reads and writes its files in `dir` and shows `snapshot` on the
    /// bridge pane. The queue is set up as `BoardOutbox::init` does it, so the panel can send.
    pub(crate) fn panel_with_queue<'a>(
        cx: &'a mut TestAppContext,
        dir: &std::path::Path,
        snapshot: BoardSnapshot,
    ) -> (Entity<BoardPanel>, &'a mut VisualTestContext) {
        panel_with_queue_on(cx, dir, snapshot, ignored_focus(), ignored_open())
    }

    /// `panel_with_queue`, with the Focus and Open PR calls given, so the tests can record them.
    pub(crate) fn panel_with_queue_on<'a>(
        cx: &'a mut TestAppContext,
        dir: &std::path::Path,
        snapshot: BoardSnapshot,
        focus: FocusFn,
        open: OpenFn,
    ) -> (Entity<BoardPanel>, &'a mut VisualTestContext) {
        cx.update(|app| {
            let herdr = app.new(|cx| Herdr::detached(LiveHerdr::default(), cx));
            app.set_global(GlobalHerdr(herdr));
            StatusMessage::init(app);
            let board = app.new(|cx| Board::new(Some(dir.to_path_buf()), cx));
            app.set_global(GlobalBoard(board.clone()));
            BoardOutbox::init(app);
            BoardPanel::bind_keys(app);
            board.update(app, |board, cx| {
                board.set(Some(snapshot), None, Some(bridge()), cx)
            });
        });
        open_in_window(cx, focus, open)
    }

    /// The queued actions of the fixture's session in `dir`, as `(seq, action)`. `None` when no
    /// actions file was written.
    pub(crate) fn queued(dir: &std::path::Path) -> Option<Vec<(u64, Action)>> {
        let session = fixture().session;
        let file = parse_actions(&std::fs::read(actions_path(dir, &session)).ok()?)?;
        Some(
            file.actions
                .into_iter()
                .map(|envelope| (envelope.seq, envelope.action))
                .collect(),
        )
    }

    /// The status line in the status bar, if one is shown.
    pub(crate) fn status(cx: &VisualTestContext) -> Option<String> {
        cx.read(|app| {
            app.global::<GlobalStatusMessage>()
                .0
                .read(app)
                .text
                .as_ref()
                .map(|text| text.to_string())
        })
    }

    /// Opens the panel in a window of the reference size and lets it settle. The panel's Focus and
    /// Open PR calls are `focus` and `open`.
    fn open_in_window(
        cx: &mut TestAppContext,
        focus: FocusFn,
        open: OpenFn,
    ) -> (Entity<BoardPanel>, &mut VisualTestContext) {
        let (host, cx) = cx.add_window_view(move |_, cx| InFrame {
            panel: cx.new(|cx| BoardPanel::with_actions(focus, open, cx)),
        });
        let view = host.read_with(cx, |host, _| host.panel.clone());
        cx.simulate_resize(size(px(WINDOW_WIDTH), px(WINDOW_HEIGHT)));
        cx.run_until_parked();
        (view, cx)
    }

    pub(crate) fn bounds(
        cx: &mut VisualTestContext,
        selector: &'static str,
    ) -> Option<Bounds<Pixels>> {
        cx.debug_bounds(selector)
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::test_support::{bounds, bridge, fixture, panel, panel_with_queue, queued, status};
    use super::*;
    use crate::board::NO_BOARD_FILE;
    use crate::board_actions::UNDO_MS;
    use gpui::{Modifiers, TestAppContext, VisualTestContext};
    use std::time::Duration;

    /// A RESULT.md with the S1 fixture, in a new temp dir. The dir is returned so it lives as long as
    /// the test. The path is absolute and ends in `.md`.
    fn report_file() -> (tempfile::TempDir, String) {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("RESULT.md");
        std::fs::write(&path, include_str!("report_fixture.md")).unwrap();
        (dir, path.to_str().unwrap().to_owned())
    }

    /// The fixture, with row 0's report set to `raw`. The shared fixture file keeps its own path.
    fn snapshot_with_report(raw: &str) -> BoardSnapshot {
        let mut snapshot = fixture();
        snapshot.rows[0].report = Some(raw.to_owned());
        snapshot
    }

    /// Sets the board to the fixture with row 0 reporting `raw`, at revision `rev`, as a read would.
    fn set_rev(cx: &mut VisualTestContext, raw: &str, rev: u64) {
        let mut next = snapshot_with_report(raw);
        next.rev = rev;
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| {
                board.set(Some(next), None, Some(bridge()), cx)
            })
        });
        cx.run_until_parked();
    }

    /// Reads started by the panel's report view.
    fn reads(cx: &mut VisualTestContext, panel: &Entity<BoardPanel>) -> usize {
        cx.update(|_, app| panel.read(app).report.read(app).reads)
    }

    /// Calls `show_report` on the panel for the row `key`, as a click on a work row does.
    fn show_report_for(cx: &mut VisualTestContext, view: &Entity<BoardPanel>, key: &str) {
        cx.update(|_, app| {
            view.update(app, |panel, cx| panel.show_report(key.to_owned(), cx));
        });
        cx.run_until_parked();
    }

    /// Clicks the centre of the element with the given selector, as the mouse would.
    fn click(cx: &mut VisualTestContext, selector: &'static str) {
        let bounds = cx.debug_bounds(selector).unwrap();
        cx.simulate_click(bounds.center(), Modifiers::none());
        cx.run_until_parked();
    }

    #[test]
    fn the_panel_opens_on_the_board_tab() {
        assert_eq!(PanelTab::default(), PanelTab::Board);
    }

    #[test]
    fn report_target_follows_the_row_key() {
        let snapshot = fixture();
        assert_eq!(report_target(&snapshot, None), ReportTarget::None);
        assert_eq!(
            report_target(&snapshot, Some("owner/name#999")),
            ReportTarget::Gone
        );
        assert_eq!(
            report_target(&snapshot, Some("owner/name#640")),
            ReportTarget::Row(ReportSource::from_row(&snapshot.rows[0]))
        );
    }

    #[test]
    fn empty_text_names_the_state() {
        assert_eq!(empty_text(false, None), "no coordinator board");
        assert_eq!(empty_text(false, Some("x")), "no coordinator board");
        assert_eq!(empty_text(true, None), "loading board…");
        assert_eq!(empty_text(true, Some(NO_BOARD_FILE)), NO_BOARD_FILE);
    }

    #[test]
    fn summary_text_adds_a_problem_after_the_summary() {
        assert_eq!(summary_text("2 of 2 busy", None), "2 of 2 busy");
        assert_eq!(
            summary_text("2 of 2 busy", Some("board v2 · this contr reads v1")),
            "2 of 2 busy · board v2 · this contr reads v1"
        );
        assert_eq!(
            summary_text("", Some("no board file yet")),
            "no board file yet"
        );
    }

    #[test]
    fn wrap_text_follows_wrap_up() {
        assert_eq!(wrap_text(false), "wrap up");
        assert_eq!(wrap_text(true), "keep going");
    }

    #[gpui::test]
    fn no_bridge_shows_the_empty_line_and_no_summary(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, None, None, None);
        assert!(bounds(cx, "board-empty").is_some());
        assert!(bounds(cx, "board-summary").is_none());
        assert!(bounds(cx, "work-section").is_none());
        assert!(
            bounds(cx, "board-wrap").is_none(),
            "no wrap label without a snapshot"
        );
        assert!(bounds(cx, "board-header").is_some());
    }

    #[gpui::test]
    fn a_problem_without_a_snapshot_is_the_empty_line(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, None, Some(NO_BOARD_FILE), Some(bridge()));
        assert!(bounds(cx, "board-empty").is_some());
        assert!(bounds(cx, "board-summary").is_none());
    }

    #[gpui::test]
    fn a_snapshot_shows_the_header_summary_and_sections(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "board-empty").is_none());
        assert!(bounds(cx, "board-summary").is_some());
        assert!(bounds(cx, "work-section").is_some());
        assert!(bounds(cx, "needs-section").is_some());

        let panel = bounds(cx, "board-panel").unwrap();
        let header = bounds(cx, "board-header").unwrap();
        assert_eq!(header.size.height, px(PANEL_HEADER_HEIGHT));
        assert_eq!(header.origin.y, panel.origin.y, "the header is at the top");

        let board_tab = bounds(cx, "board-tab-board-on").unwrap();
        let report_tab = bounds(cx, "board-tab-report").unwrap();
        assert!(
            board_tab.origin.x < report_tab.origin.x,
            "Board is left of Report"
        );
    }

    #[gpui::test]
    fn the_wrap_label_follows_wrap_up(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "board-wrap-up").is_some());
        assert!(bounds(cx, "board-wrap-keep").is_none());

        let mut next = fixture();
        next.wrap_up = true;
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| {
                board.set(Some(next), None, Some(bridge()), cx)
            })
        });
        cx.run_until_parked();
        assert!(bounds(cx, "board-wrap-keep").is_some());
        assert!(bounds(cx, "board-wrap-up").is_none());
    }

    #[gpui::test]
    fn a_problem_keeps_the_summary_on_screen(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), Some(NO_BOARD_FILE), Some(bridge()));
        assert!(bounds(cx, "board-summary").is_some());
        assert!(bounds(cx, "board-empty").is_none());
    }

    #[gpui::test]
    fn the_needs_section_shows_the_snapshot_and_ends_where_the_card_begins(
        cx: &mut TestAppContext,
    ) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "needs-count-3").is_some());
        assert!(bounds(cx, "need-row-3-resolved").is_some());
        let section = bounds(cx, "needs-section").unwrap();
        let card = bounds(cx, "answer-card").unwrap();
        assert_eq!(
            section.bottom(),
            card.origin.y,
            "the card is below the section"
        );
    }

    #[gpui::test]
    fn the_report_tab_is_clickable_and_selected_when_open(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "board-tab-board-on").is_some());
        assert!(bounds(cx, "board-tab-report-on").is_none());

        click(cx, "board-tab-report");
        assert!(bounds(cx, "board-tab-report-on").is_some());
        assert!(bounds(cx, "board-tab-board-on").is_none());
        assert!(
            bounds(cx, "board-tab-board").is_some(),
            "the Board tab is now the unselected one"
        );
    }

    #[gpui::test]
    fn the_report_tab_without_a_row_shows_the_pick_line(cx: &mut TestAppContext) {
        let (view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        click(cx, "board-tab-report");
        assert!(bounds(cx, "report").is_some());
        assert!(
            bounds(cx, "report-empty").is_some(),
            "the pick line is drawn"
        );
        assert!(bounds(cx, "report-title").is_none());
        assert_eq!(reads(cx, &view), 0, "no row, no read");
    }

    #[gpui::test]
    fn board_tab_and_back_return_to_the_board(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        click(cx, "board-tab-report");
        assert!(bounds(cx, "report").is_some());

        click(cx, "board-tab-board");
        assert!(bounds(cx, "report").is_none());
        assert!(bounds(cx, "work-section").is_some());
        assert!(bounds(cx, "board-tab-board-on").is_some());

        click(cx, "board-tab-report");
        click(cx, "report-back");
        assert!(bounds(cx, "report").is_none());
        assert!(bounds(cx, "needs-section").is_some());
        assert!(bounds(cx, "board-tab-board-on").is_some());
    }

    #[gpui::test]
    fn show_report_opens_the_rows_report(cx: &mut TestAppContext) {
        let (_dir, raw) = report_file();
        let (view, cx) = panel(cx, Some(snapshot_with_report(&raw)), None, Some(bridge()));
        show_report_for(cx, &view, "owner/name#640");

        assert!(bounds(cx, "board-tab-report-on").is_some());
        assert!(bounds(cx, "report-title").is_some());
        assert!(bounds(cx, "report-heading-0").is_some());
        assert_eq!(reads(cx, &view), 1);
    }

    #[gpui::test]
    fn a_new_board_rev_rereads_the_shown_report(cx: &mut TestAppContext) {
        let (_dir, raw) = report_file();
        let (view, cx) = panel(cx, Some(snapshot_with_report(&raw)), None, Some(bridge()));
        show_report_for(cx, &view, "owner/name#640");
        assert_eq!(reads(cx, &view), 1);

        set_rev(cx, &raw, 43);
        assert_eq!(reads(cx, &view), 2, "a new revision reads the report again");
    }

    #[gpui::test]
    fn the_board_tab_reads_no_report(cx: &mut TestAppContext) {
        let (_dir, raw) = report_file();
        let (view, cx) = panel(cx, Some(snapshot_with_report(&raw)), None, Some(bridge()));
        set_rev(cx, &raw, 43);
        assert_eq!(reads(cx, &view), 0, "the Board tab reads no report");

        show_report_for(cx, &view, "owner/name#640");
        click(cx, "board-tab-board");
        set_rev(cx, &raw, 44);
        assert_eq!(
            reads(cx, &view),
            1,
            "after the Board tab, new revisions read nothing"
        );
    }

    #[gpui::test]
    fn a_row_that_leaves_the_board_shows_the_gone_line(cx: &mut TestAppContext) {
        let (_dir, raw) = report_file();
        let (view, cx) = panel(cx, Some(snapshot_with_report(&raw)), None, Some(bridge()));
        show_report_for(cx, &view, "owner/name#640");

        let mut gone = snapshot_with_report(&raw);
        gone.rows.clear();
        gone.rev = 43;
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| {
                board.set(Some(gone), None, Some(bridge()), cx)
            })
        });
        cx.run_until_parked();

        assert!(bounds(cx, "report-empty").is_some(), "the gone line");
        assert!(bounds(cx, "report-title").is_none());
        assert_eq!(reads(cx, &view), 1, "a gone row reads nothing");
    }

    #[gpui::test]
    fn the_report_tab_hides_summary_and_sections(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        click(cx, "board-tab-report");
        assert!(bounds(cx, "board-summary").is_none());
        assert!(bounds(cx, "work-section").is_none());
        assert!(bounds(cx, "needs-section").is_none());
        assert!(bounds(cx, "board-header").is_some(), "the header stays");

        let board_panel = bounds(cx, "board-panel").unwrap();
        let report = bounds(cx, "report").unwrap();
        assert_eq!(
            report.bottom(),
            board_panel.bottom(),
            "the report fills the rest of the panel"
        );
    }

    #[gpui::test]
    fn no_snapshot_shows_board_empty_on_the_report_tab(cx: &mut TestAppContext) {
        let (view, cx) = panel(cx, None, None, None);
        click(cx, "board-tab-report");
        assert!(bounds(cx, "board-tab-report-on").is_some());
        assert!(bounds(cx, "board-empty").is_some());
        assert!(bounds(cx, "report").is_none());
        assert_eq!(reads(cx, &view), 0);
    }

    /// The key of the work row that the Report tab follows.
    fn report_key(cx: &mut VisualTestContext, view: &Entity<BoardPanel>) -> Option<String> {
        view.read_with(cx, |panel, _| panel.report_key().map(str::to_owned))
    }

    #[gpui::test]
    fn card_report_shows_only_for_a_row_with_a_report(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(
            bounds(cx, "card-report").is_none(),
            "the item T12 is picked"
        );

        click(cx, "work-row-0");
        assert!(bounds(cx, "card-report").is_some(), "#640 has a report");

        click(cx, "work-row-1");
        assert!(bounds(cx, "card-report").is_none(), "#641 has no report");
    }

    #[gpui::test]
    fn card_report_opens_the_report_tab_for_that_row(cx: &mut TestAppContext) {
        let (_dir, raw) = report_file();
        let (view, cx) = panel(cx, Some(snapshot_with_report(&raw)), None, Some(bridge()));
        click(cx, "work-row-0");
        click(cx, "card-report");

        assert!(bounds(cx, "board-tab-report-on").is_some());
        assert_eq!(report_key(cx, &view).as_deref(), Some("owner/name#640"));
        assert!(
            bounds(cx, "report-heading-0").is_some(),
            "the row's file is drawn"
        );
        assert_eq!(reads(cx, &view), 1);
    }

    #[gpui::test]
    fn the_report_tab_follows_the_picked_row(cx: &mut TestAppContext) {
        let (dir, raw) = report_file();
        let other = dir.path().join("OTHER.md");
        std::fs::write(&other, "Other row, plain text.\n").unwrap();
        let mut snapshot = snapshot_with_report(&raw);
        snapshot.rows[1].report = Some(other.to_str().unwrap().to_owned());
        let (view, cx) = panel(cx, Some(snapshot), None, Some(bridge()));

        click(cx, "work-row-1");
        click(cx, "board-tab-report");
        assert_eq!(report_key(cx, &view).as_deref(), Some("owner/name#641"));
        assert!(
            bounds(cx, "report-text-0").is_some(),
            "#641's file is drawn"
        );
        click(cx, "board-tab-board");

        click(cx, "work-row-0");
        click(cx, "board-tab-report");
        assert_eq!(report_key(cx, &view).as_deref(), Some("owner/name#640"));
        assert!(
            bounds(cx, "report-heading-0").is_some(),
            "#640's file is drawn"
        );
        assert_eq!(reads(cx, &view), 2, "each pick reads its own report once");
    }

    #[gpui::test]
    fn back_keeps_the_pick(cx: &mut TestAppContext) {
        let (view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        click(cx, "work-row-0");
        click(cx, "board-tab-report");
        click(cx, "report-back");

        assert!(bounds(cx, "board-tab-board-on").is_some());
        assert!(
            bounds(cx, "work-row-0-picked").is_some(),
            "the row stays picked"
        );
        assert!(
            bounds(cx, "card-report").is_some(),
            "the card still shows the row"
        );
        let picked = view.read_with(cx, |panel, _| panel.picked().cloned());
        assert_eq!(picked, Some(BoardPick::Row("owner/name#640".to_owned())));
    }

    #[gpui::test]
    fn the_report_tab_keeps_its_row_when_an_item_is_picked(cx: &mut TestAppContext) {
        let (_dir, raw) = report_file();
        let (view, cx) = panel(cx, Some(snapshot_with_report(&raw)), None, Some(bridge()));
        show_report_for(cx, &view, "owner/name#640");
        click(cx, "board-tab-board");
        click(cx, "need-row-1");

        click(cx, "board-tab-report");
        assert_eq!(report_key(cx, &view).as_deref(), Some("owner/name#640"));
        assert!(bounds(cx, "report-heading-0").is_some());
    }

    #[gpui::test]
    fn show_report_keeps_its_row_over_another_pick(cx: &mut TestAppContext) {
        let (view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        click(cx, "work-row-1");
        show_report_for(cx, &view, "owner/name#640");

        assert!(bounds(cx, "board-tab-report-on").is_some());
        assert_eq!(report_key(cx, &view).as_deref(), Some("owner/name#640"));
    }

    /// Sets the board to `snapshot` on the bridge pane, as a read would, and lets the panel react.
    fn set_board(cx: &mut VisualTestContext, snapshot: BoardSnapshot) {
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        cx.update(|_, app| {
            board.update(app, |board, cx| {
                board.set(Some(snapshot), None, Some(bridge()), cx)
            })
        });
        cx.run_until_parked();
    }

    #[test]
    fn wrap_flip_sends_the_other_state_with_its_status() {
        assert_eq!(
            wrap_flip(false),
            (Action::WrapUp { on: true }, "wrap up sent")
        );
        assert_eq!(
            wrap_flip(true),
            (Action::WrapUp { on: false }, "keep going sent")
        );
    }

    #[gpui::test]
    fn wrap_up_click_queues_wrap_up_and_shows_the_status(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "board-wrap");
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, Action::WrapUp { on: true })])
        );
        assert_eq!(status(cx).as_deref(), Some("wrap up sent"));
    }

    #[gpui::test]
    fn keep_going_click_queues_wrap_up_off(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.wrap_up = true;
        let (_view, cx) = panel_with_queue(cx, dir.path(), snapshot);
        assert!(bounds(cx, "board-wrap-keep").is_some());
        click(cx, "board-wrap");
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, Action::WrapUp { on: false })])
        );
        assert_eq!(status(cx).as_deref(), Some("keep going sent"));
    }

    #[gpui::test]
    fn clear_done_on_the_work_section_queues_clear_rows(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "work-clear");
        assert_eq!(queued(dir.path()), Some(vec![(1, Action::ClearRows)]));
        assert_eq!(status(cx).as_deref(), Some("clear done sent"));
    }

    #[gpui::test]
    fn clear_done_on_the_needs_section_queues_clear_items(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "needs-clear");
        assert_eq!(queued(dir.path()), Some(vec![(1, Action::ClearItems)]));
        assert_eq!(status(cx).as_deref(), Some("clear done sent"));
    }

    #[gpui::test]
    fn a_board_that_takes_no_actions_queues_nothing(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.actions = 0;
        let (_view, cx) = panel_with_queue(cx, dir.path(), snapshot);
        click(cx, "board-wrap");
        click(cx, "work-clear");
        click(cx, "needs-clear");
        assert_eq!(queued(dir.path()), None, "no actions file is written");
        assert_eq!(status(cx), None, "no status line is shown");
        assert!(bounds(cx, "board-wrap").is_some(), "the label stays");
        assert!(bounds(cx, "work-clear").is_some(), "clear done stays");
        assert!(bounds(cx, "needs-clear").is_some(), "clear done stays");
    }

    #[gpui::test]
    fn the_wrap_label_follows_the_next_snapshot(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "board-wrap");
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, Action::WrapUp { on: true })])
        );

        // The mod takes seq 1 and shows wrap up on.
        let mut next = fixture();
        next.wrap_up = true;
        next.acted = 1;
        next.rev = 43;
        set_board(cx, next);
        assert_eq!(queued(dir.path()), Some(vec![]), "the taken action leaves");
        assert!(bounds(cx, "board-wrap-keep").is_some());

        click(cx, "board-wrap");
        assert_eq!(
            queued(dir.path()),
            Some(vec![(2, Action::WrapUp { on: false })])
        );
    }

    #[gpui::test]
    fn a_panel_without_a_queue_sends_nothing(cx: &mut TestAppContext) {
        let (view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        let sent = cx.update(|_, app| {
            view.update(app, |panel, cx| {
                panel.send_action(Action::ClearRows, Some(CLEAR_SENT), cx)
            })
        });
        assert!(!sent, "no queue, nothing is sent");
    }

    /// The pick the panel holds, before the fall-back to the first open item.
    fn picked(cx: &mut VisualTestContext, view: &Entity<BoardPanel>) -> Option<BoardPick> {
        view.read_with(cx, |panel, _| panel.picked().cloned())
    }

    /// Moves the test clock by `by`, after what is due has run, and runs what that makes due.
    fn advance(cx: &mut VisualTestContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    #[gpui::test]
    fn the_keys_do_nothing_until_the_panel_has_focus(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (view, cx) = panel_with_queue(cx, dir.path(), fixture());
        cx.simulate_keystrokes("down enter 1");
        assert_eq!(picked(cx, &view), None, "the pick does not move");
        assert_eq!(queued(dir.path()), None, "nothing is sent");
    }

    #[gpui::test]
    fn the_arrow_keys_move_the_pick_through_the_items(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "board-summary");

        cx.simulate_keystrokes("up");
        assert_eq!(picked(cx, &view), None, "up from the first item stays");
        assert!(
            bounds(cx, "need-row-0-picked").is_some(),
            "T12 is still shown"
        );

        cx.simulate_keystrokes("down");
        assert!(
            bounds(cx, "need-row-1-picked").is_some(),
            "down moves to D7"
        );
        cx.simulate_keystrokes("down down");
        assert!(bounds(cx, "need-row-3-picked").is_some(), "down reaches D6");
        cx.simulate_keystrokes("down");
        assert!(
            bounds(cx, "need-row-3-picked").is_some(),
            "down stops at D6"
        );

        cx.simulate_keystrokes("up");
        assert!(bounds(cx, "need-row-2-picked").is_some(), "up moves to N4");
    }

    #[gpui::test]
    fn down_from_a_picked_row_goes_to_the_first_item(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "work-row-0");

        cx.simulate_keystrokes("up");
        assert!(
            bounds(cx, "work-row-0-picked").is_some(),
            "up keeps the row"
        );

        cx.simulate_keystrokes("down");
        assert!(bounds(cx, "need-row-0-picked").is_some(), "down picks T12");
        assert!(bounds(cx, "work-row-0-picked").is_none());
        assert_eq!(picked(cx, &view), Some(BoardPick::Item("T12".to_owned())));
    }

    #[gpui::test]
    fn enter_on_an_open_decision_queues_its_recommended_answer(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "need-row-1");

        cx.simulate_keystrokes("enter");
        assert!(
            bounds(cx, "card-undo").is_some(),
            "the answer can be undone"
        );
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
    fn enter_on_a_to_do_sends_done_and_enter_on_news_dismisses(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "board-summary");

        cx.simulate_keystrokes("enter");
        advance(cx, Duration::from_millis(UNDO_MS));
        let done = Action::Done {
            key: "T12".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, done.clone())]));

        click(cx, "need-row-2");
        cx.simulate_keystrokes("enter");
        advance(cx, Duration::from_millis(UNDO_MS));
        let dismiss = Action::Dismiss {
            key: "N4".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, done), (2, dismiss)]));
    }

    #[gpui::test]
    fn enter_does_nothing_on_a_resolved_item_or_while_an_answer_is_held(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "need-row-3");
        cx.simulate_keystrokes("enter");
        assert_eq!(queued(dir.path()), None, "D6 is resolved");

        click(cx, "need-row-1");
        cx.simulate_keystrokes("enter enter");
        advance(cx, Duration::from_millis(UNDO_MS));
        let answer = Action::Answer {
            key: "D7".to_owned(),
            option: 0,
            label: "one line per row".to_owned(),
        };
        assert_eq!(
            queued(dir.path()),
            Some(vec![(1, answer)]),
            "the second enter sends nothing"
        );
    }

    #[gpui::test]
    fn digit_1_sends_the_first_option_in_card_order(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.items[1].recommended = Some(1);
        let (_view, cx) = panel_with_queue(cx, dir.path(), snapshot);
        click(cx, "need-row-1");

        cx.simulate_keystrokes("1");
        advance(cx, Duration::from_millis(UNDO_MS));
        let answer = Action::Answer {
            key: "D7".to_owned(),
            option: 1,
            label: "two lines per row".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, answer)]));
    }

    #[gpui::test]
    fn digit_2_sends_the_second_option_in_card_order(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "need-row-1");

        cx.simulate_keystrokes("2");
        advance(cx, Duration::from_millis(UNDO_MS));
        let answer = Action::Answer {
            key: "D7".to_owned(),
            option: 1,
            label: "two lines per row".to_owned(),
        };
        assert_eq!(queued(dir.path()), Some(vec![(1, answer)]));
    }

    #[gpui::test]
    fn digits_send_nothing_past_the_last_option_or_on_a_to_do(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (_view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "board-summary");
        cx.simulate_keystrokes("1");
        assert!(bounds(cx, "card-done").is_some(), "T12 is still open");
        assert_eq!(queued(dir.path()), None, "a to-do has no options");

        click(cx, "need-row-1");
        cx.simulate_keystrokes("4");
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(queued(dir.path()), None, "D7 has two options only");
        assert!(bounds(cx, "card-undo").is_none());
    }

    #[gpui::test]
    fn the_report_tab_ignores_the_arrow_keys_enter_and_digits(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let (view, cx) = panel_with_queue(cx, dir.path(), fixture());
        click(cx, "work-row-0");
        click(cx, "board-tab-report");
        cx.simulate_keystrokes("down up enter 1");
        assert_eq!(
            picked(cx, &view),
            Some(BoardPick::Row("owner/name#640".to_owned())),
            "the row stays picked"
        );

        click(cx, "board-tab-board");
        click(cx, "need-row-1");
        click(cx, "board-tab-report");
        cx.simulate_keystrokes("down up enter 1 2");
        assert_eq!(
            picked(cx, &view),
            Some(BoardPick::Item("D7".to_owned())),
            "the item stays picked"
        );
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(queued(dir.path()), None, "nothing is sent");

        click(cx, "board-tab-board");
        assert!(bounds(cx, "card-opt-0").is_some(), "D7 is still open");
        assert!(bounds(cx, "card-undo").is_none());
    }

    #[gpui::test]
    fn an_inert_board_moves_the_pick_and_sends_nothing(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let mut snapshot = fixture();
        snapshot.actions = 0;
        let (_view, cx) = panel_with_queue(cx, dir.path(), snapshot);
        click(cx, "board-summary");

        cx.simulate_keystrokes("down");
        assert!(bounds(cx, "need-row-1-picked").is_some(), "the pick moves");
        cx.simulate_keystrokes("enter 1");
        advance(cx, Duration::from_millis(UNDO_MS));
        assert_eq!(queued(dir.path()), None, "no actions file is written");
        assert_eq!(status(cx), None, "no status line is shown");
        assert!(bounds(cx, "card-opt-0").is_some(), "the options stay");
    }
}
