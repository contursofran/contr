//! The board panel: the header with the `Board | Report` switch and the wrap label, then the body of
//! the shown tab. The Board tab draws the summary line, the work section, the needs-you section and the
//! answer card at the foot. The Report tab draws the report of the row it follows (`ReportView`) and
//! nothing else. The panel reads the `Board` entity and redraws when that entity notifies. A report is
//! read only while the Report tab is shown. The pick (a row or an item) is kept here: the card shows it,
//! and a click on a row or an item sets it.

use crate::agents_list::{FocusFn, focus_in_background};
use crate::board::{Board, GlobalBoard};
use crate::board_card::{
    BoardPick, CardFace, RowFace, answer_card, click_fn, effective_pick, names_on_board,
    tab_label_for_pane,
};
use crate::board_model::{BoardSnapshot, WorkRow};
use crate::board_needs::{PickFn, needs_section};
use crate::board_outbox::now_ms;
use crate::board_report::{ReportEvent, ReportSource, ReportTarget, ReportView};
use crate::board_work::{WorkRowEvent, WorkRowView, work_section};
use crate::herdr::GlobalHerdr;
use crate::theme::*;
use contr_herdr::HerdrTree;
use gpui::{
    App, ClickEvent, Context, Entity, IntoElement, ParentElement, Render, Styled, Subscription,
    WeakEntity, Window, div, prelude::*, px, rgb, transparent_black,
};
use std::rc::Rc;

/// The two tabs of the panel. `Report` shows the report of the row in `report_row`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum PanelTab {
    #[default]
    Board,
    Report,
}

/// Opens a URL. The app opens it in the system browser; a test records it.
pub type OpenFn = Rc<dyn Fn(String, &mut App)>;

/// The board panel view.
pub struct BoardPanel {
    board: Entity<Board>,
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
    /// The Report tab's view. It is drawn only on the Report tab.
    report: Entity<ReportView>,
    /// The key of the work row that the Report tab follows. It stays when the tab is left.
    report_row: Option<String>,
    _report_events: Subscription,
    /// Focuses a pane, for the card's `Focus` button.
    focus: FocusFn,
    /// Opens a URL, for the card's `Open PR` button.
    open: OpenFn,
}

impl BoardPanel {
    /// Needs `Board::init` and `Herdr::init` first. Redraws when the board or the herdr tree changes.
    /// On the Report tab, a board revision also syncs the report. Focus and Open PR use the app's calls.
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self::with_actions(Rc::new(focus_in_background), Rc::new(open_in_browser), cx)
    }

    /// `new` with the Focus and Open PR calls given, so the tests can record them.
    fn with_actions(focus: FocusFn, open: OpenFn, cx: &mut Context<Self>) -> Self {
        let board = cx.global::<GlobalBoard>().0.clone();
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let observe = cx.observe(&board, |this, _, cx| {
            this.sync_work(cx);
            if this.tab == PanelTab::Report {
                this.sync_report(cx);
            }
        });
        let observe_herdr = cx.observe(&herdr, |_, _, cx| cx.notify());
        let report = cx.new(ReportView::new);
        let report_events = cx.subscribe(&report, |this, _, event: &ReportEvent, cx| match event {
            ReportEvent::Back => this.open_board_tab(cx),
        });
        let mut panel = Self {
            board,
            tab: PanelTab::default(),
            work: Vec::new(),
            _row_picks: Vec::new(),
            pick: None,
            _observe: observe,
            _observe_herdr: observe_herdr,
            report,
            report_row: None,
            _report_events: report_events,
            focus,
            open,
        };
        panel.sync_work(cx);
        panel
    }

    /// Shows the Report tab for the work row `row_key`.
    pub fn show_report(&mut self, row_key: String, cx: &mut Context<Self>) {
        self.report_row = Some(row_key);
        self.open_report_tab(cx);
    }

    /// Picks `pick`: the card shows it, and the picked work row draws as selected.
    pub(crate) fn set_pick(&mut self, pick: BoardPick, cx: &mut Context<Self>) {
        self.pick = Some(pick);
        self.sync_picks(cx);
        cx.notify();
    }

    /// The Report tab's click. The tab keeps the row it followed before, if any.
    fn open_report_tab(&mut self, cx: &mut Context<Self>) {
        self.tab = PanelTab::Report;
        self.sync_report(cx);
        cx.notify();
    }

    /// The Board tab's click, and `back` in the report. The report row stays.
    fn open_board_tab(&mut self, cx: &mut Context<Self>) {
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

    /// What the card shows for `shown`, the pick that `effective_pick` resolved on `snapshot`.
    fn card_face(
        &self,
        snapshot: &BoardSnapshot,
        shown: Option<&BoardPick>,
        tree: Option<&HerdrTree>,
        cx: &Context<Self>,
    ) -> CardFace {
        match shown {
            Some(BoardPick::Item(key)) => snapshot
                .items
                .iter()
                .find(|item| item.key == *key)
                .map_or(CardFace::Empty, |item| CardFace::Item(item.clone())),
            Some(BoardPick::Row(key)) => match snapshot.rows.iter().find(|row| row.key == *key) {
                Some(row) => CardFace::Row(self.row_face(row, tree, cx)),
                None => CardFace::Empty,
            },
            None => CardFace::Empty,
        }
    }

    /// The buttons of a work row. Focus needs a pane that the tree holds in a tab, Report needs a
    /// report, and Open PR needs a URL. Each button's click calls the panel's Focus or Open PR call,
    /// or shows the report.
    fn row_face(&self, row: &WorkRow, tree: Option<&HerdrTree>, cx: &Context<Self>) -> RowFace {
        let focus = row.pane_id.as_deref().and_then(|pane| {
            let tab = tree.and_then(|tree| tab_label_for_pane(tree, pane))?;
            let focus = self.focus.clone();
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
        let open_pr = row.url.clone().map(|url| {
            let open = self.open.clone();
            click_fn(move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
                open(url.clone(), cx);
            })
        });
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
        // The pick and the card come first, so every use of `cx` for them is done before the header.
        let shown = snapshot
            .as_deref()
            .and_then(|current| effective_pick(current, self.pick.as_ref()));
        let face = match (snapshot.as_deref(), self.tab) {
            (Some(current), PanelTab::Board) => {
                self.card_face(current, shown.as_ref(), tree.as_deref(), cx)
            }
            _ => CardFace::Empty,
        };
        let picked_item = match &shown {
            Some(BoardPick::Item(key)) => Some(key.clone()),
            _ => None,
        };
        let pick_item = item_pick(cx.weak_entity());
        let head = header(self.tab, snapshot.as_ref().map(|shown| shown.wrap_up), cx);
        let root = div()
            .debug_selector(|| "board-panel".into())
            .size_full()
            .flex()
            .flex_col()
            .child(head);
        match (snapshot, self.tab) {
            (None, _) => root.child(text_line(
                "board-empty",
                empty_text(bridge, problem.as_deref()),
                TEXT_DIM,
            )),
            (Some(_), PanelTab::Report) => root.child(report_area(self.report.clone())),
            (Some(snapshot), PanelTab::Board) => root
                .child(summary(&snapshot, problem.as_deref()))
                .child(work_section(&self.work, snapshot.clear_rows))
                .child(needs_section(
                    &snapshot.items,
                    snapshot.need_count,
                    snapshot.clear_items,
                    picked_item.as_deref(),
                    pick_item,
                ))
                .child(answer_card(face, now_ms())),
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

/// The header row. `wrap_up` is `None` while there is no snapshot: the wrap label is then hidden.
fn header(tab: PanelTab, wrap_up: Option<bool>, cx: &mut Context<BoardPanel>) -> impl IntoElement {
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
        .when_some(wrap_up, |row, wrap_up| row.child(wrap_label(wrap_up)))
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

/// The inert wrap label on the right of the header.
fn wrap_label(wrap_up: bool) -> impl IntoElement {
    let selector = if wrap_up {
        "board-wrap-keep"
    } else {
        "board-wrap-up"
    };
    div()
        .debug_selector(|| "board-wrap".into())
        .text_color(rgb(TEXT_DIM))
        .child(
            div()
                .debug_selector(move || selector.to_owned())
                .child(wrap_text(wrap_up)),
        )
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
fn empty_text(bridge: bool, problem: Option<&str>) -> String {
    match (bridge, problem) {
        (false, _) => "no coordinator board".to_owned(),
        (true, Some(problem)) => problem.to_owned(),
        (true, None) => "loading board…".to_owned(),
    }
}

/// The summary line text. With a problem, the problem follows the summary after " · ". With an
/// empty summary, the problem is the whole line.
fn summary_text(summary: &str, problem: Option<&str>) -> String {
    match problem {
        None => summary.to_owned(),
        Some(problem) if summary.is_empty() => problem.to_owned(),
        Some(problem) => format!("{summary} · {problem}"),
    }
}

/// The wrap label: `keep going` while the coordinator wraps up, else `wrap up`.
fn wrap_text(wrap_up: bool) -> &'static str {
    if wrap_up { "keep going" } else { "wrap up" }
}

/// Helpers for the panel's tests here and in `board_work`: the panel in a frame-sized column.
#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
pub(crate) mod test_support {
    use super::*;
    use crate::board::{BridgePane, init_detached};
    use crate::board_model::{BridgeRef, parse_board};
    use crate::herdr::{GlobalHerdr, Herdr};
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
        });
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        let problem = problem.map(str::to_owned);
        cx.update(|app| {
            board.update(app, |board, cx| board.set(snapshot, problem, bridge, cx));
        });
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
    use super::test_support::{bounds, bridge, fixture, panel};
    use super::*;
    use crate::board::NO_BOARD_FILE;
    use gpui::{Modifiers, TestAppContext, VisualTestContext};

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
}
