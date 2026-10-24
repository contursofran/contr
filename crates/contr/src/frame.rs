//! The empty three-column frame: sidebar, title bar, terminal, board panel, status bar.

use crate::agents_list::{AgentsList, FocusFn, focus_in_background};
use crate::board_panel::BoardPanel;
use crate::herdr::{GlobalHerdr, Herdr};
use crate::spaces::{Spaces, SpacesEvent, toggle_box};
use crate::status_bar::StatusBar;
#[cfg(not(test))]
use crate::tab_commands;
use crate::terminal::TerminalView;
use crate::theme::*;
#[cfg(test)]
use crate::title_bar::TabCommand;
use crate::title_bar::{TitleBar, TitleBarEvent};
use gpui::{
    AnyElement, Context, Entity, IntoElement, ParentElement, Render, StyleRefinement, Styled,
    Subscription, Window, div, prelude::*, px, rgb,
};
use std::rc::Rc;

pub struct Frame {
    spaces: Entity<Spaces>,
    agents: Entity<AgentsList>,
    herdr: Entity<Herdr>,
    status_bar: Entity<StatusBar>,
    title_bar: Entity<TitleBar>,
    board_panel: Entity<BoardPanel>,
    terminal: Entity<TerminalView>,
    sidebar_open: bool,
    panel_open: bool,
    /// The herdr commands the title bar asked for, in order. Tests read them instead of sending.
    #[cfg(test)]
    pub(crate) sent: Vec<TabCommand>,
    _toggle: Subscription,
    _herdr: Subscription,
    _title_events: Subscription,
}

impl Frame {
    /// Needs `Host::init`, `StatusMessage::init`, `Herdr::init` and `Board::init` first: the status
    /// bar reads the first two, the terminal area shows the connection's line or the terminal view,
    /// and the panel reads the board.
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        Self::with_focus(window, cx, Rc::new(focus_in_background))
    }

    /// `new` with the focus call that the sidebar's rows make, so the tests can record the panes.
    pub(crate) fn with_focus(window: &mut Window, cx: &mut Context<Self>, focus: FocusFn) -> Self {
        let spaces = {
            let focus = focus.clone();
            cx.new(|cx| Spaces::with_focus(focus, cx))
        };
        let agents = cx.new(|cx| AgentsList::with_focus(focus, cx));
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let status_bar = cx.new(StatusBar::new);
        let board_panel = cx.new(BoardPanel::new);
        let terminal = cx.new(|cx| TerminalView::new(window, cx));
        let toggle = cx.subscribe(&spaces, |this, _, event: &SpacesEvent, cx| match event {
            SpacesEvent::ToggleSidebar => this.toggle_sidebar(cx),
        });
        let herdr_sub = cx.observe(&herdr, |_, _, cx| cx.notify());
        let title_bar = cx.new(|cx| TitleBar::new(true, cx));
        let title_events =
            cx.subscribe(
                &title_bar,
                |this, _bar, event: &TitleBarEvent, cx| match event {
                    TitleBarEvent::TogglePanel => this.toggle_panel(cx),
                    TitleBarEvent::Command(cmd) => {
                        #[cfg(test)]
                        this.sent.push(cmd.clone());
                        #[cfg(not(test))]
                        tab_commands::send(cmd.clone(), cx);
                    }
                },
            );
        Self {
            spaces,
            agents,
            herdr,
            status_bar,
            title_bar,
            board_panel,
            terminal,
            sidebar_open: true,
            panel_open: true,
            #[cfg(test)]
            sent: Vec::new(),
            _toggle: toggle,
            _herdr: herdr_sub,
            _title_events: title_events,
        }
    }

    /// Shows or hides the sidebar. The header toggle reaches it through `SpacesEvent::ToggleSidebar`,
    /// and the dev control calls it directly, so its reply shows the new layout at once.
    pub(crate) fn toggle_sidebar(&mut self, cx: &mut Context<Self>) {
        self.sidebar_open = !self.sidebar_open;
        cx.notify();
    }

    /// Opens the sidebar again. The reopen button and the dev control both call it.
    pub(crate) fn reopen_sidebar(&mut self, cx: &mut Context<Self>) {
        self.sidebar_open = true;
        cx.notify();
    }

    /// Closes the board panel, or opens it again. The title bar's toggle and the dev control both
    /// call it.
    pub(crate) fn toggle_panel(&mut self, cx: &mut Context<Self>) {
        self.panel_open = !self.panel_open;
        let open = self.panel_open;
        self.title_bar
            .update(cx, |bar, cx| bar.set_panel_open(open, cx));
        cx.notify();
    }

    #[cfg(debug_assertions)]
    pub(crate) fn sidebar_open(&self) -> bool {
        self.sidebar_open
    }

    #[cfg(debug_assertions)]
    pub(crate) fn panel_open(&self) -> bool {
        self.panel_open
    }
}

/// The frame's views, as clones of their handles, for the dev control to read and drive.
#[cfg(debug_assertions)]
#[allow(dead_code)] // Not every part is read yet: the sections read them as they are built.
pub(crate) struct FrameParts {
    pub(crate) frame: Entity<Frame>,
    pub(crate) spaces: Entity<Spaces>,
    pub(crate) agents: Entity<AgentsList>,
    pub(crate) title_bar: Entity<TitleBar>,
    pub(crate) board_panel: Entity<BoardPanel>,
    pub(crate) terminal: Entity<TerminalView>,
    pub(crate) status_bar: Entity<StatusBar>,
    pub(crate) herdr: Entity<Herdr>,
}

/// The parts of the frame `this`.
#[cfg(debug_assertions)]
pub(crate) fn parts(this: &Entity<Frame>, cx: &gpui::App) -> FrameParts {
    let frame = this.read(cx);
    FrameParts {
        frame: this.clone(),
        spaces: frame.spaces.clone(),
        agents: frame.agents.clone(),
        title_bar: frame.title_bar.clone(),
        board_panel: frame.board_panel.clone(),
        terminal: frame.terminal.clone(),
        status_bar: frame.status_bar.clone(),
        herdr: frame.herdr.clone(),
    }
}

impl Render for Frame {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        // herdr's own line while herdr is not connected; the terminal view otherwise.
        let terminal_area: AnyElement = match self.herdr.read(cx).live.state.line() {
            Some(line) => herdr_state(line).into_any_element(),
            // Cached: a redraw of the frame (herdr or board) does not paint the grid again.
            None => self
                .terminal
                .clone()
                .cached(StyleRefinement::default().size_full())
                .into_any_element(),
        };
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
                                .child(div().flex_1().min_h_0().child(self.spaces.clone()))
                                .child(self.agents.clone()),
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
                                    .when(!self.sidebar_open, |bar| bar.child(reopen_button(cx)))
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
                                            .flex()
                                            .items_center()
                                            .justify_center()
                                            .bg(rgb(BG_TERMINAL))
                                            .child(terminal_area),
                                    )
                                    .when(self.panel_open, |row| {
                                        row.child(
                                            div()
                                                .debug_selector(|| "panel".into())
                                                .w(px(PANEL_WIDTH))
                                                .flex_none()
                                                .flex()
                                                .flex_col()
                                                .bg(rgb(BG_WINDOW))
                                                .border_l_1()
                                                .border_color(rgb(LINE))
                                                .child(self.board_panel.clone()),
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

/// The connection line shown in the terminal area while herdr is not connected (secondary grey).
fn herdr_state(line: String) -> impl IntoElement {
    div()
        .debug_selector(|| "herdr-state".into())
        .text_color(rgb(TEXT_2))
        .child(line)
}

/// The title bar's first child while the sidebar is closed: the window-button inset, then the
/// toggle drawing at its right edge. Clicking anywhere on it opens the sidebar again.
fn reopen_button(cx: &mut Context<Frame>) -> impl IntoElement {
    div()
        .debug_selector(|| "sidebar-reopen".into())
        .id("sidebar-reopen")
        .w(px(TRAFFIC_LIGHT_INSET + 24.))
        .h_full()
        .flex_none()
        .flex()
        .justify_end()
        .cursor_pointer()
        .on_click(cx.listener(|this, _, _, cx| this.reopen_sidebar(cx)))
        .child(toggle_box())
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::board;
    use crate::selection::Selection;
    use crate::title_bar::tests::{connected_with, herdr_in, sample_tree};
    use crate::visibility::Visibility;
    use contr_herdr::{ConnState, HerdrUpdate, LiveHerdr};
    use gpui::{Modifiers, TestAppContext, VisualTestContext, px, size};

    fn connected() -> LiveHerdr {
        LiveHerdr {
            state: ConnState::Connected {
                server_version: "0.9.3".into(),
            },
            tree: None,
        }
    }

    fn not_running() -> LiveHerdr {
        LiveHerdr {
            state: ConnState::NotRunning {
                reason: "no socket".into(),
            },
            tree: None,
        }
    }

    #[gpui::test]
    fn frame_matches_design_sizes(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, connected());
        cx.update(board::init_detached);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(Frame::new);
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
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, connected());
        cx.update(board::init_detached);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        let button = cx.debug_bounds("sidebar-toggle").unwrap();
        assert_eq!(button.origin.x, px(TRAFFIC_LIGHT_INSET));
        assert!(cx.debug_bounds("sidebar-reopen").is_none());
    }

    #[gpui::test]
    fn toggle_hides_and_reopens_the_sidebar(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, connected());
        cx.update(board::init_detached);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(Frame::new);
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
    fn herdr_line_is_centred_in_the_terminal_while_not_running(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        herdr_in(cx, not_running());
        cx.update(board::init_detached);
        let (_view, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        let line = cx.debug_bounds("herdr-state").unwrap();
        let terminal = cx.debug_bounds("terminal").unwrap();
        assert!(line.origin.x > terminal.origin.x);
        assert!(line.origin.x + line.size.width < terminal.origin.x + terminal.size.width);
        let (dx, dy) = (
            f32::from(line.center().x) - f32::from(terminal.center().x),
            f32::from(line.center().y) - f32::from(terminal.center().y),
        );
        assert!(
            dx.abs() < 1. && dy.abs() < 1.,
            "line is off centre by ({dx}, {dy})"
        );
    }

    #[gpui::test]
    fn herdr_line_follows_the_connection(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let herdr = herdr_in(cx, connected());
        cx.update(board::init_detached);
        let (_view, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("herdr-state").is_none());
        assert!(cx.debug_bounds("terminal-grid").is_some());

        let lost = HerdrUpdate::State(not_running().state);
        cx.update(|_, app| herdr.update(app, |herdr, cx| herdr.apply(lost, cx)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("herdr-state").is_some());
        assert!(cx.debug_bounds("terminal-grid").is_none());

        let connecting = HerdrUpdate::State(ConnState::Connecting);
        cx.update(|_, app| herdr.update(app, |herdr, cx| herdr.apply(connecting, cx)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("herdr-state").is_some());
        assert!(cx.debug_bounds("terminal-grid").is_none());

        let back = HerdrUpdate::State(connected().state);
        cx.update(|_, app| herdr.update(app, |herdr, cx| herdr.apply(back, cx)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("herdr-state").is_none());
        assert!(cx.debug_bounds("terminal-grid").is_some());
    }

    #[gpui::test]
    fn panel_toggle_hides_and_shows_the_panel(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, connected());
        cx.update(board::init_detached);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (_view, cx) = cx.add_window_view(Frame::new);
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

    #[gpui::test]
    fn title_bar_clicks_reach_the_command_handler_in_order(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        herdr_in(cx, connected_with(sample_tree()));
        cx.update(board::init_detached);
        let (frame, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        for selector in ["tab-1", "tab-new", "split"] {
            click(cx, selector);
        }
        let sent = frame.read_with(cx, |frame, _| frame.sent.clone());
        assert_eq!(
            sent,
            vec![
                TabCommand::Focus("w1:t2".into()),
                TabCommand::Create("w1".into()),
                TabCommand::SplitRight("w1:p1".into()),
            ]
        );
    }

    #[gpui::test]
    fn the_panel_toggle_sends_no_herdr_command(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, connected_with(sample_tree()));
        cx.update(board::init_detached);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (frame, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        click(cx, "panel-toggle");
        let sent = frame.read_with(cx, |frame, _| frame.sent.clone());
        assert!(sent.is_empty());
    }

    /// Clicks the element with `selector` at its centre, then lets the UI settle.
    fn click(cx: &mut VisualTestContext, selector: &'static str) {
        let target = cx.debug_bounds(selector).unwrap();
        cx.simulate_click(target.center(), Modifiers::none());
        cx.run_until_parked();
    }

    #[gpui::test]
    fn a_frame_redraw_does_not_render_the_terminal_again(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, connected());
        cx.update(board::init_detached);
        cx.update(|app| crate::status_bar::init_detached(app, None));
        let (frame, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        let terminal = frame.read_with(cx, |frame, _| frame.terminal.clone());
        let rendered = terminal.read_with(cx, |terminal, _| terminal.renders);

        // The frame redraws, but the terminal's bounds and content are unchanged: it is reused.
        frame.update(cx, |_, cx| cx.notify());
        cx.run_until_parked();
        assert_eq!(
            terminal.read_with(cx, |terminal, _| terminal.renders),
            rendered
        );

        // Control: a change in the terminal itself renders it again.
        terminal.update(cx, |_, cx| cx.notify());
        cx.run_until_parked();
        assert_eq!(
            terminal.read_with(cx, |terminal, _| terminal.renders),
            rendered + 1
        );
    }
}
