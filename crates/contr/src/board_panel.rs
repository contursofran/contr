//! The board panel: the header with the `Board | Report` switch and the wrap label, then the summary
//! line, the work section (filled in by #9-S5) and the needs-you section. Read-only: the switch and
//! the wrap label are inert in #9. The panel reads the `Board` entity and redraws only when that
//! entity notifies.

use crate::board::{Board, GlobalBoard};
use crate::board_model::BoardSnapshot;
use crate::board_needs::needs_section;
use crate::theme::*;
use gpui::{
    Context, Entity, IntoElement, ParentElement, Render, Styled, Subscription, Window, div,
    prelude::*, px, rgb,
};

/// The two tabs of the panel. `Report` shows in the switch but is inert until #11.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum PanelTab {
    #[default]
    Board,
    Report,
}

/// The board panel view.
pub struct BoardPanel {
    board: Entity<Board>,
    tab: PanelTab,
    _observe: Subscription,
}

impl BoardPanel {
    /// Needs `Board::init` first. Redraws when the board notifies.
    pub fn new(cx: &mut Context<Self>) -> Self {
        let board = cx.global::<GlobalBoard>().0.clone();
        let observe = cx.observe(&board, |_, _, cx| cx.notify());
        Self {
            board,
            tab: PanelTab::default(),
            _observe: observe,
        }
    }
}

impl Render for BoardPanel {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let board = self.board.read(cx);
        let bridge = board.bridge.is_some();
        let problem = board.problem.clone();
        let snapshot = board.snapshot.clone();
        let root = div()
            .debug_selector(|| "board-panel".into())
            .size_full()
            .flex()
            .flex_col()
            .child(header(
                self.tab,
                snapshot.as_ref().map(|shown| shown.wrap_up),
            ));
        match snapshot {
            Some(snapshot) => root
                .child(summary(&snapshot, problem.as_deref()))
                .child(div().debug_selector(|| "work-section".into()))
                .child(needs_section(
                    &snapshot.items,
                    snapshot.need_count,
                    snapshot.clear_items,
                )),
            None => root.child(text_line(
                "board-empty",
                empty_text(bridge, problem.as_deref()),
                TEXT_DIM,
            )),
        }
    }
}

/// The header row. `wrap_up` is `None` while there is no snapshot: the wrap label is then hidden.
fn header(tab: PanelTab, wrap_up: Option<bool>) -> impl IntoElement {
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
                .child(segment("board-tab-board", "Board", tab == PanelTab::Board))
                .child(segment(
                    "board-tab-report",
                    "Report",
                    tab == PanelTab::Report,
                )),
        )
        .when_some(wrap_up, |row, wrap_up| row.child(wrap_label(wrap_up)))
}

/// One segment of the switch. The selected one is a card with a 1 px border. The border is always
/// there (the panel colour when not selected), so the labels do not move when the selection does.
fn segment(selector: &'static str, label: &'static str, selected: bool) -> impl IntoElement {
    div()
        .debug_selector(move || selector.to_owned())
        .px(px(SEGMENT_PAD_X))
        .py(px(SEGMENT_PAD_Y))
        .rounded(px(SEGMENT_RADIUS))
        .border_1()
        .border_color(rgb(if selected { CARD_BORDER } else { BG_WINDOW }))
        .when(selected, |tab| tab.bg(rgb(BG_CARD)).text_color(rgb(TEXT)))
        .when(!selected, |tab| tab.text_color(rgb(TEXT_DIM)))
        .child(label)
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
fn text_line(selector: &'static str, text: String, colour: u32) -> impl IntoElement {
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

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board::{BridgePane, NO_BOARD_FILE, init_detached};
    use crate::board_model::{BridgeRef, parse_board};
    use crate::herdr::{GlobalHerdr, Herdr};
    use contr_herdr::LiveHerdr;
    use gpui::{Bounds, Pixels, TestAppContext, VisualTestContext, px, size};

    const FIXTURE: &str = include_str!("board_fixture_v1.json");

    fn fixture() -> BoardSnapshot {
        parse_board(FIXTURE.as_bytes()).unwrap()
    }

    fn bridge() -> BridgePane {
        BridgePane {
            pane_id: "w1:p1".into(),
            bridge: BridgeRef {
                session: "s1".into(),
                rev: 1,
            },
        }
    }

    /// A panel in a window of the panel's size, over a board that shows `snapshot`, `problem` and
    /// `bridge` (set by the test, not read from a file).
    fn panel<'a>(
        cx: &'a mut TestAppContext,
        snapshot: Option<BoardSnapshot>,
        problem: Option<&str>,
        bridge: Option<BridgePane>,
    ) -> (Entity<BoardPanel>, &'a mut VisualTestContext) {
        cx.update(|app| {
            let herdr = app.new(|cx| Herdr::detached(LiveHerdr::default(), cx));
            app.set_global(GlobalHerdr(herdr));
            init_detached(app);
        });
        let board = cx.read(|app| app.global::<GlobalBoard>().0.clone());
        let problem = problem.map(str::to_owned);
        cx.update(|app| {
            board.update(app, |board, cx| board.set(snapshot, problem, bridge, cx));
        });
        let (view, cx) = cx.add_window_view(|_, cx| BoardPanel::new(cx));
        cx.simulate_resize(size(px(PANEL_WIDTH), px(900.)));
        cx.run_until_parked();
        (view, cx)
    }

    fn bounds(cx: &mut VisualTestContext, selector: &'static str) -> Option<Bounds<Pixels>> {
        cx.debug_bounds(selector)
    }

    #[test]
    fn the_panel_opens_on_the_board_tab() {
        assert_eq!(PanelTab::default(), PanelTab::Board);
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

        let board_tab = bounds(cx, "board-tab-board").unwrap();
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
    fn the_needs_section_shows_the_snapshot_and_ends_at_the_panel_bottom(cx: &mut TestAppContext) {
        let (_view, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "needs-count-3").is_some());
        assert!(bounds(cx, "need-row-3-resolved").is_some());
        let panel = bounds(cx, "board-panel").unwrap();
        let section = bounds(cx, "needs-section").unwrap();
        assert_eq!(section.bottom(), panel.bottom());
    }
}
