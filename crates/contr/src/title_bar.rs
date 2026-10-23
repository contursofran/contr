//! The title bar: the agent tabs of herdr's current workspace, "+", the split button and the panel
//! toggle. The whole bar is one view, so a spinning `Working` tab icon redraws only this view
//! (the frame embeds it cached). It shows the `TabStrip` it is given; herdr wiring comes later.

use crate::status_icon::{Status, StatusIcon};
use crate::theme::{
    BG_TERMINAL, ICON_SIZE_SMALL, SIDEBAR_TOGGLE_RADIUS, TAB_GAP, TAB_MAX_WIDTH, TAB_MIN_WIDTH,
    TAB_PADDING_X, TAB_UNDERLINE, TEXT, TEXT_2, TEXT_DIM, TITLE_BUTTON_SIZE, TITLE_GLYPH_FILL,
    TITLE_GLYPH_HEIGHT, TITLE_GLYPH_WIDTH,
};
use contr_herdr::{TabItem, TabStrip};
use gpui::{
    Context, Div, EventEmitter, IntoElement, ParentElement, Render, Role, Stateful, Styled, Window,
    div, prelude::*, px, rgb,
};

/// What a click asks herdr to do. The ids are herdr's: a tab, a workspace, a pane.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TabCommand {
    Focus(String),
    Create(String),
    SplitRight(String),
}

/// Sent to the frame. The frame owns the panel; herdr commands go through `TabCommand`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TitleBarEvent {
    TogglePanel,
    Command(TabCommand),
}

/// The title bar's state: the strip it shows and whether the panel is open.
pub struct TitleBar {
    strip: TabStrip,
    panel_open: bool,
}

impl EventEmitter<TitleBarEvent> for TitleBar {}

impl TitleBar {
    /// An empty strip: no tabs, no "+", no split. The panel toggle draws `panel_open`.
    pub fn new(panel_open: bool, _cx: &mut Context<Self>) -> Self {
        Self {
            strip: TabStrip::default(),
            panel_open,
        }
    }

    /// Redraws only when the strip changes.
    pub fn set_strip(&mut self, strip: TabStrip, cx: &mut Context<Self>) {
        if self.strip != strip {
            self.strip = strip;
            cx.notify();
        }
    }

    /// Redraws only when the value changes.
    pub fn set_panel_open(&mut self, open: bool, cx: &mut Context<Self>) {
        if self.panel_open != open {
            self.panel_open = open;
            cx.notify();
        }
    }
}

impl Render for TitleBar {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let strip = &self.strip;
        div()
            .debug_selector(|| "title-strip".into())
            .size_full()
            .flex()
            .items_stretch()
            .child(tabs_row(&strip.tabs, cx))
            .when_some(strip.workspace_id.clone(), |bar, workspace| {
                bar.child(new_tab_button(workspace, cx))
            })
            .child(
                div()
                    .id("title-drag")
                    .debug_selector(|| "title-drag".into())
                    .flex_1()
                    .min_w_0()
                    .h_full(),
            )
            .child(right_group(
                strip.split_pane_id.clone(),
                self.panel_open,
                cx,
            ))
    }
}

/// The tabs of the current workspace, in herdr's order.
///
/// Overflow (D62, owner question open; the recommended option is built): each tab shrinks to
/// `TAB_MIN_WIDTH` (set in `tab`) and its label ends in an ellipsis; this row clips what does not
/// fit. A different answer changes `TAB_MIN_WIDTH` in `theme.rs` and this function.
fn tabs_row(tabs: &[TabItem], cx: &mut Context<TitleBar>) -> Div {
    div()
        .debug_selector(|| "tabs".into())
        .flex()
        .min_w_0()
        .flex_shrink_1()
        .overflow_hidden()
        .children(tabs.iter().enumerate().map(|(i, item)| tab(i, item, cx)))
}

/// One tab: the status icon, then herdr's label. The active tab has the terminal's background and
/// a white underline, and clicking it does nothing. An inactive tab asks herdr to focus it.
fn tab(i: usize, item: &TabItem, cx: &mut Context<TitleBar>) -> Stateful<Div> {
    let status = Status::from(item.status);
    let mut element = div()
        .id(("tab", i))
        .debug_selector(move || format!("tab-{i}"))
        .relative()
        .flex()
        .items_center()
        .gap(px(TAB_GAP))
        .px(px(TAB_PADDING_X))
        .min_w(px(TAB_MIN_WIDTH))
        .max_w(px(TAB_MAX_WIDTH))
        .flex_shrink_1()
        .cursor_pointer()
        .role(Role::Tab)
        .aria_label(format!("{}, {}", item.label, status.label()))
        .child(
            div()
                .debug_selector(move || format!("tab-{i}-icon-{}", status.slug()))
                .flex_none()
                .child(StatusIcon::new(("tab-icon", i), status).size(px(ICON_SIZE_SMALL))),
        )
        .child(
            div()
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .whitespace_nowrap()
                .child(item.label.clone()),
        );
    if item.active {
        element = element.bg(rgb(BG_TERMINAL)).text_color(rgb(TEXT)).child(
            div()
                .debug_selector(move || format!("tab-{i}-underline"))
                .absolute()
                .bottom_0()
                .left_0()
                .right_0()
                .h(px(TAB_UNDERLINE))
                .bg(rgb(TEXT)),
        );
    } else {
        let id = item.id.clone();
        element = element
            .text_color(rgb(TEXT_2))
            .hover(|style| style.text_color(rgb(TEXT)))
            .on_click(cx.listener(move |_, _, _, cx| {
                cx.emit(TitleBarEvent::Command(TabCommand::Focus(id.clone())))
            }));
    }
    element
}

/// "+": a new focused tab in `workspace`. Shown only when the strip has a workspace.
fn new_tab_button(workspace: String, cx: &mut Context<TitleBar>) -> Stateful<Div> {
    div()
        .id("tab-new")
        .debug_selector(|| "tab-new".into())
        .w(px(TITLE_BUTTON_SIZE))
        .h_full()
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .text_color(rgb(TEXT_DIM))
        .hover(|style| style.text_color(rgb(TEXT)))
        .cursor_pointer()
        .on_click(cx.listener(move |_, _, _, cx| {
            cx.emit(TitleBarEvent::Command(TabCommand::Create(
                workspace.clone(),
            )))
        }))
        .child("+")
}

/// The split button (when there is a pane to split) and the panel toggle, at the right edge.
fn right_group(split_pane: Option<String>, panel_open: bool, cx: &mut Context<TitleBar>) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .pr(px(8.))
        .when_some(split_pane, |group, pane| {
            group.child(split_button(pane, cx))
        })
        .child(panel_button(panel_open, cx))
}

/// A 28 px square button. Its children can restyle on hover through the group named after `id`.
fn button(id: &'static str) -> Stateful<Div> {
    div()
        .id(id)
        .debug_selector(move || id.into())
        .group(id)
        .w(px(TITLE_BUTTON_SIZE))
        .h(px(TITLE_BUTTON_SIZE))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .cursor_pointer()
}

/// The split button: an outline box with a vertical line in the middle. Splits `pane` to the
/// right (D20).
fn split_button(pane: String, cx: &mut Context<TitleBar>) -> Stateful<Div> {
    button("split")
        .on_click(cx.listener(move |_, _, _, cx| {
            cx.emit(TitleBarEvent::Command(TabCommand::SplitRight(pane.clone())))
        }))
        .child(
            glyph_outline("split").justify_center().child(
                div()
                    .w(px(1.))
                    .h_full()
                    .bg(rgb(TEXT_DIM))
                    .group_hover("split", |style| style.bg(rgb(TEXT))),
            ),
        )
}

/// The panel toggle: an outline box with its right part filled while the panel is open.
fn panel_button(panel_open: bool, cx: &mut Context<TitleBar>) -> Stateful<Div> {
    let mark = if panel_open {
        "panel-toggle-open"
    } else {
        "panel-toggle-closed"
    };
    button("panel-toggle")
        .on_click(cx.listener(|_, _, _, cx| cx.emit(TitleBarEvent::TogglePanel)))
        .child(
            glyph_outline("panel-toggle")
                .debug_selector(move || mark.into())
                .justify_end()
                .when(panel_open, |glyph| {
                    glyph.child(
                        div()
                            .w(px(TITLE_GLYPH_FILL))
                            .h_full()
                            .bg(rgb(TEXT_DIM))
                            .group_hover("panel-toggle", |style| style.bg(rgb(TEXT))),
                    )
                }),
        )
}

/// The outline both glyphs draw: 1px `TEXT_DIM`, `TEXT` while the button in `group` is hovered.
fn glyph_outline(group: &'static str) -> Div {
    div()
        .w(px(TITLE_GLYPH_WIDTH))
        .h(px(TITLE_GLYPH_HEIGHT))
        .rounded(px(SIDEBAR_TOGGLE_RADIUS))
        .border_1()
        .border_color(rgb(TEXT_DIM))
        .group_hover(group, |style| style.border_color(rgb(TEXT)))
        .overflow_hidden()
        .flex()
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use contr_herdr::AgentState;
    use gpui::{Entity, Modifiers, TestAppContext, VisualTestContext, size};
    use std::cell::{Cell, RefCell};
    use std::rc::Rc;

    /// The selectors of the 16 tabs in `many_tabs_shrink_to_the_minimum_then_clip`.
    const TAB_SELECTORS: [&str; 16] = [
        "tab-0", "tab-1", "tab-2", "tab-3", "tab-4", "tab-5", "tab-6", "tab-7", "tab-8", "tab-9",
        "tab-10", "tab-11", "tab-12", "tab-13", "tab-14", "tab-15",
    ];

    fn item(id: &str, label: &str, status: AgentState, active: bool) -> TabItem {
        TabItem {
            id: id.into(),
            label: label.into(),
            status,
            active,
        }
    }

    /// w1 with three tabs: t1 working and active, t2 blocked, t3 unknown. Split target p1.
    fn three_tabs() -> TabStrip {
        TabStrip {
            workspace_id: Some("w1".into()),
            tabs: vec![
                item("w1:t1", "api", AgentState::Working, true),
                item("w1:t2", "docs", AgentState::Blocked, false),
                item("w1:t3", "3", AgentState::Unknown, false),
            ],
            split_pane_id: Some("w1:p1".into()),
        }
    }

    /// A bare title bar in a 1160×40 window, showing `strip`.
    fn window_with(
        cx: &mut TestAppContext,
        strip: TabStrip,
        panel_open: bool,
    ) -> (Entity<TitleBar>, &mut VisualTestContext) {
        let (bar, cx) = cx.add_window_view(|_, cx| TitleBar::new(panel_open, cx));
        cx.simulate_resize(size(px(1160.), px(40.)));
        bar.update(cx, |bar, cx| bar.set_strip(strip, cx));
        cx.run_until_parked();
        (bar, cx)
    }

    /// Records every event the title bar emits.
    fn record_events(
        cx: &mut VisualTestContext,
        bar: &Entity<TitleBar>,
    ) -> Rc<RefCell<Vec<TitleBarEvent>>> {
        let events = Rc::new(RefCell::new(Vec::new()));
        let seen = events.clone();
        cx.update(|_, app| {
            app.subscribe(bar, move |_, event: &TitleBarEvent, _| {
                seen.borrow_mut().push(event.clone())
            })
            .detach()
        });
        events
    }

    /// Counts how often `bar` notifies its observers.
    fn count_notifications(cx: &mut VisualTestContext, bar: &Entity<TitleBar>) -> Rc<Cell<usize>> {
        let count = Rc::new(Cell::new(0));
        let seen = count.clone();
        cx.update(|_, app| {
            app.observe(bar, move |_, _| seen.set(seen.get() + 1))
                .detach()
        });
        count
    }

    fn click(cx: &mut VisualTestContext, selector: &'static str) {
        let target = cx.debug_bounds(selector).unwrap();
        cx.simulate_click(target.center(), Modifiers::none());
        cx.run_until_parked();
    }

    #[gpui::test]
    fn tabs_show_their_icons_and_only_the_active_one_is_underlined(cx: &mut TestAppContext) {
        let (_bar, cx) = window_with(cx, three_tabs(), true);
        assert!(cx.debug_bounds("tab-0").is_some());
        assert!(cx.debug_bounds("tab-1").is_some());
        assert!(cx.debug_bounds("tab-2").is_some());
        assert!(cx.debug_bounds("tab-3").is_none());

        let strip = cx.debug_bounds("title-strip").unwrap();
        let underline = cx.debug_bounds("tab-0-underline").unwrap();
        assert_eq!(underline.size.height, px(TAB_UNDERLINE));
        assert_eq!(underline.bottom(), strip.bottom());
        assert!(cx.debug_bounds("tab-1-underline").is_none());
        assert!(cx.debug_bounds("tab-2-underline").is_none());

        assert!(cx.debug_bounds("tab-0-icon-working").is_some());
        assert!(cx.debug_bounds("tab-1-icon-blocked").is_some());
        assert!(cx.debug_bounds("tab-2-icon-idle").is_some()); // unknown shows idle (D19)
    }

    #[gpui::test]
    fn clicking_an_inactive_tab_asks_to_focus_it(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, three_tabs(), true);
        let events = record_events(cx, &bar);
        click(cx, "tab-1");
        assert_eq!(
            *events.borrow(),
            vec![TitleBarEvent::Command(TabCommand::Focus("w1:t2".into()))]
        );
    }

    #[gpui::test]
    fn clicking_the_active_tab_emits_nothing(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, three_tabs(), true);
        let events = record_events(cx, &bar);
        click(cx, "tab-0");
        assert!(events.borrow().is_empty());
    }

    #[gpui::test]
    fn plus_asks_for_a_tab_in_the_workspace(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, three_tabs(), true);
        let events = record_events(cx, &bar);
        click(cx, "tab-new");
        assert_eq!(
            *events.borrow(),
            vec![TitleBarEvent::Command(TabCommand::Create("w1".into()))]
        );
    }

    #[gpui::test]
    fn split_asks_to_split_the_active_pane_right(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, three_tabs(), true);
        let events = record_events(cx, &bar);
        click(cx, "split");
        assert_eq!(
            *events.borrow(),
            vec![TitleBarEvent::Command(TabCommand::SplitRight(
                "w1:p1".into()
            ))]
        );
    }

    #[gpui::test]
    fn panel_toggle_emits_toggle_panel(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, three_tabs(), true);
        let events = record_events(cx, &bar);
        assert!(cx.debug_bounds("panel-toggle-open").is_some());
        click(cx, "panel-toggle");
        assert_eq!(*events.borrow(), vec![TitleBarEvent::TogglePanel]);
    }

    #[gpui::test]
    fn empty_strip_shows_only_the_panel_toggle(cx: &mut TestAppContext) {
        let (_bar, cx) = window_with(cx, TabStrip::default(), true);
        assert!(cx.debug_bounds("tab-0").is_none());
        assert!(cx.debug_bounds("tab-new").is_none());
        assert!(cx.debug_bounds("split").is_none());
        assert!(cx.debug_bounds("panel-toggle").is_some());
        assert!(cx.debug_bounds("title-drag").is_some());
    }

    #[gpui::test]
    fn workspace_without_a_pane_hides_split_but_keeps_plus(cx: &mut TestAppContext) {
        let strip = TabStrip {
            workspace_id: Some("w1".into()),
            tabs: Vec::new(),
            split_pane_id: None,
        };
        let (bar, cx) = window_with(cx, strip, true);
        let events = record_events(cx, &bar);
        assert!(cx.debug_bounds("split").is_none());
        assert!(cx.debug_bounds("tab-new").is_some());
        click(cx, "tab-new");
        assert_eq!(
            *events.borrow(),
            vec![TitleBarEvent::Command(TabCommand::Create("w1".into()))]
        );
    }

    #[gpui::test]
    fn panel_glyph_follows_the_panel_state(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, TabStrip::default(), false);
        assert!(cx.debug_bounds("panel-toggle-closed").is_some());
        assert!(cx.debug_bounds("panel-toggle-open").is_none());
        bar.update(cx, |bar, cx| bar.set_panel_open(true, cx));
        cx.run_until_parked();
        assert!(cx.debug_bounds("panel-toggle-open").is_some());
        assert!(cx.debug_bounds("panel-toggle-closed").is_none());
    }

    #[gpui::test]
    fn set_strip_notifies_only_when_the_strip_changes(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, TabStrip::default(), true);
        let notified = count_notifications(cx, &bar);
        bar.update(cx, |bar, cx| bar.set_strip(three_tabs(), cx));
        cx.run_until_parked();
        assert_eq!(notified.get(), 1);
        bar.update(cx, |bar, cx| bar.set_strip(three_tabs(), cx));
        cx.run_until_parked();
        assert_eq!(notified.get(), 1, "an equal strip does not redraw");
    }

    #[gpui::test]
    fn set_panel_open_notifies_only_when_the_value_changes(cx: &mut TestAppContext) {
        let (bar, cx) = window_with(cx, TabStrip::default(), true);
        let notified = count_notifications(cx, &bar);
        bar.update(cx, |bar, cx| bar.set_panel_open(true, cx));
        cx.run_until_parked();
        assert_eq!(notified.get(), 0, "the same value does not redraw");
        bar.update(cx, |bar, cx| bar.set_panel_open(false, cx));
        cx.run_until_parked();
        assert_eq!(notified.get(), 1);
    }

    #[gpui::test]
    fn many_tabs_shrink_to_the_minimum_then_clip(cx: &mut TestAppContext) {
        // 16 tabs at the minimum (16 × 72 = 1152) do not fit the 1068 px left after "+" and the
        // buttons, so the row clips. 12 would fit; the clip is what this test exercises.
        let tabs = (0..16)
            .map(|n| {
                item(
                    &format!("w1:t{n}"),
                    &format!("agent-number-{n}"),
                    AgentState::Idle,
                    n == 0,
                )
            })
            .collect();
        let strip = TabStrip {
            workspace_id: Some("w1".into()),
            tabs,
            split_pane_id: Some("w1:p1".into()),
        };
        let (_bar, cx) = window_with(cx, strip, true);
        for selector in TAB_SELECTORS {
            let tab = cx.debug_bounds(selector).unwrap();
            assert!(
                tab.size.width >= px(TAB_MIN_WIDTH),
                "{selector} is {:?}",
                tab.size.width
            );
        }
        let row = cx.debug_bounds("tabs").unwrap();
        let last = cx.debug_bounds("tab-15").unwrap();
        assert!(
            last.right() > row.right(),
            "the last tab runs past the clipped row"
        );
        // The toggle keeps its place at the right edge: 8 px of padding, then its 28 px button.
        let toggle = cx.debug_bounds("panel-toggle").unwrap();
        assert_eq!(toggle.right(), px(1160. - 8.));
        assert_eq!(toggle.size.width, px(TITLE_BUTTON_SIZE));
    }
}
