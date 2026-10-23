//! The empty three-column frame: sidebar, title bar, terminal, board panel, status bar.

use crate::theme::*;
use gpui::{Context, IntoElement, ParentElement, Render, Styled, Window, div, prelude::*, px, rgb};

pub struct Frame {
    panel_open: bool,
}

impl Frame {
    pub fn new() -> Self {
        Self { panel_open: true }
    }
}

impl Default for Frame {
    fn default() -> Self {
        Self::new()
    }
}

impl Render for Frame {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .size_full()
            .flex()
            .flex_col()
            .bg(rgb(BG_WINDOW))
            .font_family(FONT_FAMILY)
            .text_size(px(FONT_SIZE))
            .line_height(px(LINE_HEIGHT))
            .text_color(rgb(TEXT))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(
                        div()
                            .debug_selector(|| "sidebar".into())
                            .w(px(SIDEBAR_WIDTH))
                            .h_full()
                            .flex_none()
                            .bg(rgb(BG_BARS))
                            .border_r_1()
                            .border_color(rgb(LINE)),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .flex_col()
                            .child(
                                div()
                                    .debug_selector(|| "title-bar".into())
                                    .h(px(TITLE_BAR_HEIGHT))
                                    .flex_none()
                                    .bg(rgb(BG_BARS))
                                    .border_b_1()
                                    .border_color(rgb(LINE)),
                            )
                            .child(
                                div()
                                    .flex_1()
                                    .min_h_0()
                                    .flex()
                                    .child(
                                        div()
                                            .debug_selector(|| "terminal".into())
                                            .flex_1()
                                            .min_w_0()
                                            .bg(rgb(BG_TERMINAL)),
                                    )
                                    .when(self.panel_open, |row| {
                                        row.child(
                                            div()
                                                .debug_selector(|| "panel".into())
                                                .w(px(PANEL_WIDTH))
                                                .flex_none()
                                                .bg(rgb(BG_WINDOW))
                                                .border_l_1()
                                                .border_color(rgb(LINE)),
                                        )
                                    }),
                            ),
                    ),
            )
            .child(
                div()
                    .debug_selector(|| "status-bar".into())
                    .h(px(STATUS_BAR_HEIGHT))
                    .flex_none()
                    .bg(rgb(BG_BARS))
                    .border_t_1()
                    .border_color(rgb(LINE)),
            )
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use gpui::{TestAppContext, px, size};

    #[gpui::test]
    fn frame_matches_design_sizes(cx: &mut TestAppContext) {
        let (_view, cx) = cx.add_window_view(|_, _| Frame::new());
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        let sidebar = cx.debug_bounds("sidebar").unwrap();
        assert_eq!(sidebar.origin.y, px(0.)); // reaches the top (title-bar area)
        assert_eq!(sidebar.size.width, px(280.));
        assert_eq!(cx.debug_bounds("title-bar").unwrap().size.height, px(40.));
        let panel = cx.debug_bounds("panel").unwrap();
        assert_eq!(panel.size.width, px(440.));
        assert_eq!(panel.origin.x, px(1000.)); // right edge
        assert_eq!(cx.debug_bounds("terminal").unwrap().size.width, px(720.));
        let status = cx.debug_bounds("status-bar").unwrap();
        assert_eq!(status.size.height, px(26.));
        assert_eq!(status.size.width, px(1440.));
    }
}
