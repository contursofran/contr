//! The empty three-column frame: sidebar, title bar, terminal, board panel, status bar.

use crate::spaces::{Spaces, SpacesEvent, toggle_box};
use crate::status_bar::StatusBar;
use crate::theme::*;
use crate::title_bar::{TitleBar, TitleBarEvent};
use gpui::{
    Context, Entity, IntoElement, ParentElement, Render, StyleRefinement, Styled, Subscription,
    Window, div, prelude::*, px, rgb,
};

pub struct Frame {
    spaces: Entity<Spaces>,
    status_bar: Entity<StatusBar>,
    title_bar: Entity<TitleBar>,
    sidebar_open: bool,
    panel_open: bool,
    _toggle: Subscription,
    _title_events: Subscription,
}

impl Frame {
    /// Needs `Host::init` and `StatusMessage::init` first (the status bar reads them).
    pub fn new(cx: &mut Context<Self>) -> Self {
        let spaces = cx.new(Spaces::new);
        let status_bar = cx.new(StatusBar::new);
        let toggle = cx.subscribe(&spaces, |this, _, event: &SpacesEvent, cx| {
            match event {
                SpacesEvent::ToggleSidebar => this.sidebar_open = !this.sidebar_open,
            }
            cx.notify();
        });
        let title_bar = cx.new(|cx| TitleBar::new(true, cx));
        let title_events = cx.subscribe(&title_bar, |this, bar, event: &TitleBarEvent, cx| {
            match event {
                TitleBarEvent::TogglePanel => {
                    this.panel_open = !this.panel_open;
                    let open = this.panel_open;
                    bar.update(cx, |bar, cx| bar.set_panel_open(open, cx));
                    cx.notify();
                }
                TitleBarEvent::Command(_) => {} // S4 sends these to herdr
            }
        });
        Self {
            spaces,
            status_bar,
            title_bar,
            sidebar_open: true,
            panel_open: true,
            _toggle: toggle,
            _title_events: title_events,
        }
    }
}

impl Render for Frame {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
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
                    .when(self.sidebar_open, |row| {
                        row.child(
                            div()
                                .debug_selector(|| "sidebar".into())
                                .w(px(SIDEBAR_WIDTH))
                                .h_full()
                                .flex_none()
                                .flex()
                                .flex_col()
                                .bg(rgb(BG_BARS))
                                .border_r_1()
                                .border_color(rgb(LINE))
                                .child(self.spaces.clone()),
                        )
                    })
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
                                    .flex()
                                    .items_center()
                                    .bg(rgb(BG_BARS))
                                    .border_b_1()
                                    .border_color(rgb(LINE))
                                    .when(!self.sidebar_open, |bar| bar.child(reopen_sidebar(cx)))
                                    .child(self.title_bar.clone().cached(
                                        StyleRefinement::default().flex_1().min_w_0().h_full(),
                                    )),
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
                self.status_bar.clone().cached(
                    // A cached view needs a definite size: full width, one bar high.
                    StyleRefinement::default()
                        .w_full()
                        .h(px(STATUS_BAR_HEIGHT))
                        .flex_none(),
                ),
            )
    }
}

/// The title bar's first child while the sidebar is closed: the window-button inset, then the
/// toggle drawing at its right edge. Clicking anywhere on it opens the sidebar again.
fn reopen_sidebar(cx: &mut Context<Frame>) -> impl IntoElement {
    div()
        .debug_selector(|| "sidebar-reopen".into())
        .id("sidebar-reopen")
        .w(px(TRAFFIC_LIGHT_INSET + 24.))
        .h_full()
        .flex_none()
        .flex()
        .justify_end()
        .cursor_pointer()
        .on_click(cx.listener(|this, _, _, cx| {
            this.sidebar_open = true;
            cx.notify();
        }))
        .child(toggle_box())
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::selection::Selection;
    use gpui::{Modifiers, TestAppContext, px, size};

    #[gpui::test]
    fn frame_matches_design_sizes(cx: &mut TestAppContext) {
        cx.update(Selection::init);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        let sidebar = cx.debug_bounds("sidebar").unwrap();
        assert_eq!(sidebar.origin.y, px(0.)); // reaches the top (title-bar area)
        assert_eq!(sidebar.size.width, px(280.));
        let header = cx.debug_bounds("sidebar-header").unwrap();
        assert_eq!(header.origin.y, px(0.));
        assert_eq!(header.size.height, px(40.));
        let body = cx.debug_bounds("spaces-body").unwrap();
        assert_eq!(body.origin.y, px(40.));
        assert_eq!(cx.debug_bounds("title-bar").unwrap().size.height, px(40.));
        let panel = cx.debug_bounds("panel").unwrap();
        assert_eq!(panel.size.width, px(440.));
        assert_eq!(panel.origin.x, px(1000.)); // right edge
        assert_eq!(cx.debug_bounds("terminal").unwrap().size.width, px(720.));
        let status = cx.debug_bounds("status-bar").unwrap();
        assert_eq!(status.size.height, px(26.));
        assert_eq!(status.size.width, px(1440.));
    }

    #[gpui::test]
    fn toggle_sits_right_of_the_window_buttons(cx: &mut TestAppContext) {
        cx.update(Selection::init);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        let button = cx.debug_bounds("sidebar-toggle").unwrap();
        assert_eq!(button.origin.x, px(TRAFFIC_LIGHT_INSET));
        assert!(cx.debug_bounds("sidebar-reopen").is_none());
    }

    #[gpui::test]
    fn toggle_hides_and_reopens_the_sidebar(cx: &mut TestAppContext) {
        cx.update(Selection::init);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        let button = cx.debug_bounds("sidebar-toggle").unwrap();
        cx.simulate_click(button.center(), Modifiers::none());
        cx.run_until_parked();
        assert!(cx.debug_bounds("sidebar").is_none());
        assert!(cx.debug_bounds("sidebar-spaces").is_none());
        assert!(cx.debug_bounds("sidebar-reopen").is_some());
        assert_eq!(cx.debug_bounds("terminal").unwrap().size.width, px(1000.));
        assert_eq!(cx.debug_bounds("terminal").unwrap().origin.x, px(0.));

        let reopen = cx.debug_bounds("sidebar-reopen").unwrap();
        cx.simulate_click(reopen.center(), Modifiers::none());
        cx.run_until_parked();
        assert!(cx.debug_bounds("sidebar-reopen").is_none());
        assert_eq!(cx.debug_bounds("sidebar").unwrap().size.width, px(280.));
        assert_eq!(cx.debug_bounds("terminal").unwrap().size.width, px(720.));
    }

    #[gpui::test]
    fn panel_toggle_hides_and_shows_the_panel(cx: &mut TestAppContext) {
        cx.update(Selection::init);
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("panel-toggle-open").is_some());

        let button = cx.debug_bounds("panel-toggle").unwrap();
        cx.simulate_click(button.center(), Modifiers::none());
        cx.run_until_parked();
        assert!(cx.debug_bounds("panel").is_none());
        assert!(cx.debug_bounds("panel-toggle-closed").is_some());
        assert_eq!(cx.debug_bounds("terminal").unwrap().size.width, px(1160.));

        let button = cx.debug_bounds("panel-toggle").unwrap();
        cx.simulate_click(button.center(), Modifiers::none());
        cx.run_until_parked();
        assert_eq!(cx.debug_bounds("panel").unwrap().size.width, px(440.));
        assert!(cx.debug_bounds("panel-toggle-open").is_some());
        assert_eq!(cx.debug_bounds("terminal").unwrap().size.width, px(720.));
    }
}
