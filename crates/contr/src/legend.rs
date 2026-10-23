//! Debug legend for `contr --icons`: the ten status icons with their labels and where each shows up
//! (the "G · contr status icons" artboard). Each row is its own cached view, so a spinning
//! `working` icon redraws only its own row.

use crate::status_icon::{Status, StatusIcon};
use crate::theme::{BG_WINDOW, FONT_FAMILY, FONT_SIZE, LINE_HEIGHT, TEXT, TEXT_DIM};
use gpui::{
    Context, Entity, IntoElement, ParentElement, Render, StyleRefinement, Styled, Window, div,
    prelude::*, px, rgb,
};

const ROW_HEIGHT: f32 = LINE_HEIGHT + 8.;

pub struct Legend {
    rows: Vec<Entity<LegendRow>>,
}

impl Legend {
    pub fn new(cx: &mut Context<Self>) -> Self {
        let rows = Status::ALL
            .into_iter()
            .map(|status| cx.new(|_| LegendRow { status, renders: 0 }))
            .collect();
        Self { rows }
    }
}

impl Render for Legend {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .size_full()
            .bg(rgb(BG_WINDOW))
            .font_family(FONT_FAMILY)
            .text_size(px(FONT_SIZE))
            .line_height(px(LINE_HEIGHT))
            .text_color(rgb(TEXT))
            .py_4()
            .flex()
            .flex_col()
            .child(
                div()
                    .px_4()
                    .pb_2()
                    .text_color(rgb(TEXT_DIM))
                    .child("contr status icons"),
            )
            .children(self.rows.iter().map(|row| {
                row.clone()
                    .cached(StyleRefinement::default().w_full().h(px(ROW_HEIGHT)))
            }))
    }
}

pub struct LegendRow {
    status: Status,
    renders: usize,
}

impl Render for LegendRow {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        self.renders += 1;
        div()
            .h(px(ROW_HEIGHT))
            .flex()
            .items_center()
            .gap_3()
            .px_4()
            .child(StatusIcon::new("icon", self.status))
            .child(div().text_color(rgb(TEXT)).child(self.status.label()))
            .child(div().text_color(rgb(TEXT_DIM)).child(place(self.status)))
    }
}

/// Where the status shows up (docs/DESIGN.md, status icon table).
fn place(status: Status) -> &'static str {
    match status {
        Status::NeedsYou => "worktree waits on the owner",
        Status::Decide => "decision (D…)",
        Status::Do => "to-do (T…)",
        Status::Blocked => "stuck at a prompt",
        Status::Working => "running",
        Status::Waiting => "CI or another agent",
        Status::Idle => "nothing to do",
        Status::Done => "stage passed",
        Status::Fyi => "news",
        Status::NotStarted => "stage ahead",
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::{Legend, Status};
    use crate::theme::ICON_SIZE;
    use gpui::{TestAppContext, px, size};

    #[gpui::test]
    fn working_redraws_only_its_row(cx: &mut TestAppContext) {
        let (legend, cx) = cx.add_window_view(|_, cx| Legend::new(cx));
        cx.run_until_parked();
        for _ in 0..3 {
            cx.update(|w, cx| w.simulate_next_frame(cx));
            cx.run_until_parked();
        }
        let rows = legend.read_with(cx, |legend, _| legend.rows.clone());
        for row in rows {
            let (status, renders) = row.read_with(cx, |row, _| (row.status, row.renders));
            if status == Status::Working {
                assert!(renders > 1, "working row rendered {renders} times");
            } else {
                assert_eq!(renders, 1, "{status:?} row re-rendered");
            }
        }
    }

    #[gpui::test]
    fn legend_shows_all_ten(cx: &mut TestAppContext) {
        let (_legend, cx) = cx.add_window_view(|_, cx| Legend::new(cx));
        cx.simulate_resize(size(px(480.), px(420.)));
        cx.run_until_parked();
        // Legend order (Status::ALL); debug_bounds needs 'static selectors.
        const SELECTORS: [&str; 10] = [
            "icon-needs-you",
            "icon-decide",
            "icon-do",
            "icon-blocked",
            "icon-working",
            "icon-waiting",
            "icon-idle",
            "icon-done",
            "icon-fyi",
            "icon-not-started",
        ];
        let mut previous_y = None;
        for selector in SELECTORS {
            let bounds = cx.debug_bounds(selector).unwrap();
            assert_eq!(bounds.size, size(px(ICON_SIZE), px(ICON_SIZE)));
            if let Some(previous) = previous_y {
                assert!(
                    bounds.origin.y > previous,
                    "{selector} is not below the row before"
                );
            }
            previous_y = Some(bounds.origin.y);
        }
    }
}
