//! The work section of the board panel: a header with the five stage heads, then one row per issue
//! or pull request, in the coordinator's order. Each row is its own cached view, so a spinning
//! `working` icon redraws only its own row.

use crate::board_model::{STAGE_HEADS, Stage, WhoKind, WorkRow, stage_status};
use crate::board_panel::text_line;
use crate::status_icon::StatusIcon;
use crate::theme::*;
use gpui::{
    AnyElement, Context, Entity, FontWeight, IntoElement, ParentElement, Render, Role,
    StyleRefinement, Styled, Window, div, prelude::*, px, relative, rgb,
};

/// One work row. `index` is its place in the section; it names the row's debug selectors.
pub struct WorkRowView {
    row: WorkRow,
    index: usize,
    /// Renders so far. Tests count them to check that only a changed row redraws.
    #[cfg(test)]
    renders: usize,
}

impl WorkRowView {
    pub fn new(row: WorkRow, index: usize) -> Self {
        Self {
            row,
            index,
            #[cfg(test)]
            renders: 0,
        }
    }

    /// The stable slot key of the row.
    pub fn key(&self) -> &str {
        &self.row.key
    }

    /// Takes `row` and redraws. Nothing happens when `row` is the row already shown.
    pub fn set_row(&mut self, row: &WorkRow, cx: &mut Context<Self>) {
        if self.row != *row {
            self.row = row.clone();
            cx.notify();
        }
    }
}

impl Render for WorkRowView {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        #[cfg(test)]
        {
            self.renders += 1;
        }
        let index = self.index;
        let row = &self.row;
        div()
            .id(("work-row", index))
            .debug_selector(move || format!("work-row-{index}"))
            .role(Role::ListItem)
            .aria_label(accessible_label(row))
            .w_full()
            .h(px(WORK_ROW_HEIGHT))
            .flex()
            .items_center()
            .px(px(PANEL_PAD_X))
            .child(
                div()
                    .w(px(WORK_LABEL_WIDTH))
                    .flex_none()
                    .text_color(rgb(TEXT_DIM))
                    .child(row.label.clone()),
            )
            .child(text_column(row, index))
            .when(row.joined.is_none(), |line| {
                line.children(
                    (0..STAGE_HEADS.len())
                        .map(|column| stage_cell(index, column, row.stage(column))),
                )
            })
    }
}

/// The work section: the header, then the rows, or the empty line when there are none. The section
/// is at most half the panel tall, and the rows scroll inside it.
pub fn work_section(work: &[Entity<WorkRowView>], clear_rows: bool) -> impl IntoElement {
    let section = div()
        .debug_selector(|| "work-section".into())
        .flex_none()
        .max_h(relative(0.5))
        .flex()
        .flex_col()
        .child(work_header(clear_rows));
    if work.is_empty() {
        section.child(text_line(
            "work-empty",
            "No workers yet.".to_owned(),
            TEXT_DIM,
        ))
    } else {
        section.child(
            div()
                .id("work-rows")
                .debug_selector(|| "work-rows".into())
                .min_h_0()
                .overflow_y_scroll()
                .children(work.iter().map(|view| {
                    view.clone()
                        .cached(StyleRefinement::default().w_full().h(px(WORK_ROW_HEIGHT)))
                })),
        )
    }
}

/// "Work", the inert `clear done` label when `clear_rows`, and the five stage heads on the right.
fn work_header(clear_rows: bool) -> impl IntoElement {
    div()
        .debug_selector(|| "work-header".into())
        .h(px(BOARD_LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .px(px(PANEL_PAD_X))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .items_center()
                .child(div().text_color(rgb(TEXT_DIM)).child("Work"))
                .when(clear_rows, |left| {
                    left.child(
                        div()
                            .debug_selector(|| "work-clear".into())
                            .ml(px(BOARD_GAP))
                            .text_color(rgb(TEXT_DIM_2))
                            .child("clear done"),
                    )
                }),
        )
        .children(STAGE_HEADS.iter().enumerate().map(|(column, head)| {
            div()
                .debug_selector(move || format!("work-head-{column}"))
                .w(px(STAGE_COL_WIDTH))
                .flex_none()
                .flex()
                .justify_center()
                .text_color(rgb(TEXT_DIM))
                .child(*head)
        }))
}

/// The label and text of a row: the title over who holds it. A joined row shows its join instead.
fn text_column(row: &WorkRow, index: usize) -> impl IntoElement {
    let column = div().flex_1().min_w_0().flex().flex_col().justify_center();
    if let Some(joined) = &row.joined {
        return column.child(
            div()
                .debug_selector(move || format!("work-row-{index}-joined"))
                .h(px(LINE_HEIGHT))
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .whitespace_nowrap()
                .text_color(rgb(TEXT_DIM))
                .line_through()
                .child(format!("joined {joined}")),
        );
    }
    column
        .child(
            div()
                .h(px(LINE_HEIGHT))
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .whitespace_nowrap()
                .text_color(rgb(title_colour(row)))
                .child(row.title.clone()),
        )
        .child(who_line(row, index))
}

/// The second line: a `you` chip when the owner holds the row, else the text in its colour.
fn who_line(row: &WorkRow, index: usize) -> AnyElement {
    let line = div()
        .h(px(LINE_HEIGHT))
        .flex()
        .items_center()
        .min_w_0()
        .overflow_hidden()
        .whitespace_nowrap();
    match row.who.kind {
        WhoKind::You => line
            .child(
                div()
                    .debug_selector(move || format!("work-row-{index}-you"))
                    .px(px(BADGE_PAD_X))
                    .rounded(px(BADGE_RADIUS))
                    .bg(rgb(YOU_BG))
                    .text_color(rgb(YOU_TEXT))
                    .font_weight(FontWeight::BOLD)
                    .child(row.who.text.clone()),
            )
            .into_any_element(),
        kind => line
            .text_ellipsis()
            .text_color(rgb(who_colour(kind)))
            .child(row.who.text.clone())
            .into_any_element(),
    }
}

/// One of the five stage cells, with its icon centred.
fn stage_cell(row: usize, column: usize, stage: Stage) -> impl IntoElement {
    div()
        .w(px(STAGE_COL_WIDTH))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .child(
            StatusIcon::new(
                ("work-stage", row * STAGE_HEADS.len() + column),
                stage_status(stage),
            )
            .size(px(ICON_SIZE_SMALL)),
        )
}

/// The title is dim when nothing runs or waits on the row.
fn title_colour(row: &WorkRow) -> u32 {
    if row.finished { TEXT_DIM } else { TEXT }
}

/// `dim` is quieter than `text`. Any other kind reads as `text`.
fn who_colour(kind: WhoKind) -> u32 {
    match kind {
        WhoKind::Dim => TEXT_DIM_2,
        WhoKind::You | WhoKind::Text | WhoKind::Other => TEXT_2,
    }
}

/// The text a screen reader gets for a row: its label, title, the five stages and who holds it. A
/// joined row has no stages, so it reads its join.
fn accessible_label(row: &WorkRow) -> String {
    if let Some(joined) = &row.joined {
        return format!("{} joined {joined}", row.label);
    }
    let stages = STAGE_HEADS
        .iter()
        .enumerate()
        .map(|(column, head)| format!("{head} {}", stage_status(row.stage(column)).label()))
        .collect::<Vec<_>>()
        .join(", ");
    let who = if row.who.text.is_empty() {
        String::new()
    } else {
        format!(", {}", row.who.text)
    };
    format!("{} {}, {stages}{who}", row.label, row.title)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board::GlobalBoard;
    use crate::board_model::BoardSnapshot;
    use crate::board_panel::BoardPanel;
    use crate::board_panel::test_support::{bounds, bridge, fixture, panel};
    use gpui::{TestAppContext, VisualTestContext};

    /// The selectors need `'static` strings, so the names are written out.
    const HEADS: [&str; 5] = [
        "work-head-0",
        "work-head-1",
        "work-head-2",
        "work-head-3",
        "work-head-4",
    ];
    const ROWS: [&str; 3] = ["work-row-0", "work-row-1", "work-row-2"];

    fn row(index: usize) -> WorkRow {
        fixture().rows.remove(index)
    }

    /// The fixture with `rows` in place of its rows.
    fn with_rows(rows: Vec<WorkRow>) -> BoardSnapshot {
        let mut snapshot = fixture();
        snapshot.rows = rows;
        snapshot
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

    /// The renders of each work row view so far, in order.
    fn renders(panel: &Entity<BoardPanel>, cx: &mut VisualTestContext) -> Vec<usize> {
        let views = panel.read_with(cx, |panel, _| panel.work_rows().to_vec());
        views
            .iter()
            .map(|view| view.read_with(cx, |view, _| view.renders))
            .collect()
    }

    #[test]
    fn the_accessible_label_reads_the_stages_in_order() {
        assert_eq!(
            accessible_label(&row(0)),
            "#640 sort bags, plan done, build working, review not started, CI not started, \
             you not started, b-1151 5m"
        );
    }

    #[test]
    fn a_row_without_who_has_no_trailing_comma() {
        let mut row = row(0);
        row.who.text.clear();
        assert!(accessible_label(&row).ends_with("you not started"));
    }

    #[test]
    fn a_joined_row_reads_its_join() {
        assert_eq!(accessible_label(&row(2)), "#642 joined #641");
    }

    #[test]
    fn a_finished_title_is_dim_and_a_live_one_is_not() {
        assert_eq!(title_colour(&row(2)), TEXT_DIM);
        assert_eq!(title_colour(&row(0)), TEXT);
    }

    #[test]
    fn who_colour_follows_the_kind() {
        assert_eq!(who_colour(WhoKind::Dim), TEXT_DIM_2);
        assert_eq!(who_colour(WhoKind::Text), TEXT_2);
        assert_eq!(who_colour(WhoKind::Other), TEXT_2);
    }

    #[gpui::test]
    fn the_five_stage_heads_fill_the_right_220_px(cx: &mut TestAppContext) {
        let (_panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        let board = bounds(cx, "board-panel").unwrap();
        let right = board.origin.x + board.size.width - px(PANEL_PAD_X);
        for (column, selector) in HEADS.into_iter().enumerate() {
            let head = bounds(cx, selector).unwrap();
            assert_eq!(head.size.width, px(STAGE_COL_WIDTH), "{selector}");
            assert_eq!(
                head.origin.x,
                right - px(STAGE_COL_WIDTH * (5 - column) as f32),
                "{selector} is not in its column"
            );
        }
    }

    #[gpui::test]
    fn each_work_row_is_42_px_tall(cx: &mut TestAppContext) {
        let (_panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        for selector in ROWS {
            let rect = bounds(cx, selector).unwrap();
            assert_eq!(rect.size.height, px(WORK_ROW_HEIGHT), "{selector}");
        }
    }

    #[gpui::test]
    fn a_working_row_shows_done_and_working_in_their_columns(cx: &mut TestAppContext) {
        // No items: a resolved item draws its own `icon-done`, which would hide the row's.
        let mut snapshot = with_rows(vec![row(0)]);
        snapshot.items.clear();
        let (_panel, cx) = panel(cx, Some(snapshot), None, Some(bridge()));
        let board = bounds(cx, "board-panel").unwrap();
        let line = bounds(cx, ROWS[0]).unwrap();
        assert_eq!(line.size.width, board.size.width, "the row spans the panel");
        let in_column = |cx: &mut VisualTestContext, icon: &'static str, head: &'static str| {
            let icon = bounds(cx, icon).unwrap();
            let head = bounds(cx, head).unwrap();
            icon.origin.x >= head.origin.x && icon.origin.x < head.origin.x + px(STAGE_COL_WIDTH)
        };
        assert!(in_column(cx, "icon-done", HEADS[0]), "plan is done");
        assert!(in_column(cx, "icon-working", HEADS[1]), "build is running");
    }

    #[gpui::test]
    fn the_you_row_shows_its_chip(cx: &mut TestAppContext) {
        let (_panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "work-row-1-you").is_some());
        assert!(bounds(cx, "work-row-0-you").is_none());
    }

    #[gpui::test]
    fn a_joined_row_has_no_stage_icons(cx: &mut TestAppContext) {
        let (_panel, cx) = panel(cx, Some(with_rows(vec![row(2)])), None, Some(bridge()));
        assert!(bounds(cx, "work-row-0-joined").is_some());
        assert!(bounds(cx, "icon-not-started").is_none());
    }

    #[gpui::test]
    fn clear_done_shows_only_when_rows_can_be_cleared(cx: &mut TestAppContext) {
        let (_panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        assert!(bounds(cx, "work-clear").is_some());

        let mut snapshot = fixture();
        snapshot.clear_rows = false;
        show(cx, snapshot);
        assert!(bounds(cx, "work-clear").is_none());
    }

    #[gpui::test]
    fn rows_keep_the_coordinators_order(cx: &mut TestAppContext) {
        let mut second = row(0);
        second.key = "b".into();
        second.label = "#2".into();
        let mut first = row(1);
        first.key = "a".into();
        first.label = "#1".into();
        let (panel, cx) = panel(
            cx,
            Some(with_rows(vec![second, first])),
            None,
            Some(bridge()),
        );
        let keys: Vec<String> = panel.read_with(cx, |panel, cx| {
            panel
                .work_rows()
                .iter()
                .map(|view| view.read(cx).key().to_owned())
                .collect()
        });
        assert_eq!(keys, ["b", "a"]);
        let first_row = bounds(cx, ROWS[0]).unwrap();
        let second_row = bounds(cx, ROWS[1]).unwrap();
        assert!(first_row.origin.y < second_row.origin.y);
    }

    #[gpui::test]
    fn a_changed_stage_redraws_only_its_row(cx: &mut TestAppContext) {
        let (panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        let before = renders(&panel, cx);

        let mut next = fixture();
        next.rows[1].stages[4] = Stage::Done;
        show(cx, next);
        let after = renders(&panel, cx);

        assert_eq!(after[1], before[1] + 1, "the changed row redraws once");
        assert_eq!(after[2], before[2], "the joined row does not redraw");
    }

    #[gpui::test]
    fn the_same_snapshot_again_redraws_no_row(cx: &mut TestAppContext) {
        let (panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        let before = renders(&panel, cx);
        show(cx, fixture());
        assert_eq!(renders(&panel, cx), before);
    }

    #[gpui::test]
    fn new_keys_rebuild_the_rows(cx: &mut TestAppContext) {
        let (panel, cx) = panel(cx, Some(fixture()), None, Some(bridge()));
        let old = panel.read_with(cx, |panel, _| panel.work_rows().to_vec());

        show(cx, with_rows(vec![row(0)]));
        let new = panel.read_with(cx, |panel, _| panel.work_rows().to_vec());
        assert_eq!(new.len(), 1);
        assert_ne!(new[0].entity_id(), old[0].entity_id());
    }

    #[gpui::test]
    fn no_rows_shows_the_empty_line(cx: &mut TestAppContext) {
        let (_panel, cx) = panel(cx, Some(with_rows(Vec::new())), None, Some(bridge()));
        assert!(bounds(cx, "work-empty").is_some());
        assert!(bounds(cx, "work-rows").is_none());
    }

    #[gpui::test]
    fn a_long_list_scrolls_inside_half_the_panel(cx: &mut TestAppContext) {
        let base = row(0);
        let rows = (0..20)
            .map(|i| WorkRow {
                key: format!("k{i}"),
                label: format!("#{i}"),
                ..base.clone()
            })
            .collect();
        let (_panel, cx) = panel(cx, Some(with_rows(rows)), None, Some(bridge()));
        let board = bounds(cx, "board-panel").unwrap();
        let section = bounds(cx, "work-section").unwrap();
        assert!(bounds(cx, "work-rows").is_some());
        assert!(section.size.height <= board.size.height / 2.);
    }
}
