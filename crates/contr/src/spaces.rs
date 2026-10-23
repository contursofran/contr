//! The sidebar's spaces column: the header (window-button inset, toggle, wordmark) and the body
//! that scrolls. Repos, worktrees and the host card fill the body in later slices.

use crate::theme::{TEXT_2, TEXT_DIM, TITLE_BAR_HEIGHT, TRAFFIC_LIGHT_INSET};
use gpui::{
    Context, Div, EventEmitter, FontWeight, IntoElement, MouseButton, ParentElement, Render,
    Stateful, Styled, Subscription, Window, div, prelude::*, px, rgb,
};

/// Sent by the header toggle; the frame shows or hides the sidebar.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SpacesEvent {
    ToggleSidebar,
}

pub struct Spaces {
    _subs: Vec<Subscription>,
}

impl EventEmitter<SpacesEvent> for Spaces {}

impl Spaces {
    pub fn new(_cx: &mut Context<Self>) -> Self {
        Self { _subs: Vec::new() }
    }
}

impl Render for Spaces {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .debug_selector(|| "sidebar-spaces".into())
            .size_full()
            .flex()
            .flex_col()
            .child(header(cx))
            .child(
                div()
                    .debug_selector(|| "spaces-body".into())
                    .id("spaces-body")
                    .flex_1()
                    .min_h_0()
                    .overflow_y_scroll(),
            )
    }
}

/// The header: an inset that clears the native window buttons, then the toggle and the wordmark.
/// Dragging its background moves the window.
fn header(cx: &mut Context<Spaces>) -> impl IntoElement {
    div()
        .debug_selector(|| "sidebar-header".into())
        .h(px(TITLE_BAR_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(8.))
        .pl(px(TRAFFIC_LIGHT_INSET))
        .on_mouse_down(MouseButton::Left, |_, window, _| window.start_window_move())
        .child(
            toggle("sidebar-toggle")
                .on_click(cx.listener(|_, _, _, cx| cx.emit(SpacesEvent::ToggleSidebar))),
        )
        .child(
            div()
                .debug_selector(|| "wordmark".into())
                .text_color(rgb(TEXT_2))
                .font_weight(FontWeight::BOLD)
                .child("contr"),
        )
}

/// The sidebar toggle: a clickable 16×16 box. Mouse-down stops here, so the header's window drag
/// does not take the click. The caller adds the click handler.
fn toggle(id: &'static str) -> Stateful<Div> {
    toggle_box()
        .id(id)
        .debug_selector(move || id.into())
        .cursor_pointer()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
}

/// The toggle drawing, not clickable: a 14×12 outline with its left 4 px filled, centred in a
/// 16×16 box.
pub fn toggle_box() -> Div {
    div()
        .size(px(TOGGLE_BOX))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .child(
            div()
                .w(px(TOGGLE_WIDTH))
                .h(px(TOGGLE_HEIGHT))
                .rounded(px(2.))
                .border_1()
                .border_color(rgb(TEXT_DIM))
                .overflow_hidden()
                .flex()
                .child(div().w(px(TOGGLE_FILL)).h_full().bg(rgb(TEXT_DIM))),
        )
}

const TOGGLE_BOX: f32 = 16.;
const TOGGLE_WIDTH: f32 = 14.;
const TOGGLE_HEIGHT: f32 = 12.;
const TOGGLE_FILL: f32 = 4.;

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use gpui::{Entity, Modifiers, TestAppContext, VisualTestContext, size};
    use std::cell::RefCell;
    use std::rc::Rc;

    /// Records every event `spaces` emits.
    fn record_events(
        cx: &mut VisualTestContext,
        spaces: &Entity<Spaces>,
    ) -> Rc<RefCell<Vec<SpacesEvent>>> {
        let events = Rc::new(RefCell::new(Vec::new()));
        let seen = events.clone();
        cx.update(|_, cx| {
            cx.subscribe(spaces, move |_, event: &SpacesEvent, _| {
                seen.borrow_mut().push(*event)
            })
            .detach()
        });
        events
    }

    #[gpui::test]
    fn toggle_click_emits_toggle_sidebar_once(cx: &mut TestAppContext) {
        let (spaces, cx) = cx.add_window_view(|_, cx| Spaces::new(cx));
        cx.simulate_resize(size(px(280.), px(900.)));
        cx.run_until_parked();
        let events = record_events(cx, &spaces);
        let button = cx.debug_bounds("sidebar-toggle").unwrap();
        cx.simulate_click(button.center(), Modifiers::none());
        cx.run_until_parked();
        assert_eq!(*events.borrow(), vec![SpacesEvent::ToggleSidebar]);
    }

    #[gpui::test]
    fn click_on_the_body_emits_nothing(cx: &mut TestAppContext) {
        // A header click would call `Window::start_window_move`, which the test platform leaves
        // unimplemented; the body has no handler, so it covers the "no event" path.
        let (spaces, cx) = cx.add_window_view(|_, cx| Spaces::new(cx));
        cx.simulate_resize(size(px(280.), px(900.)));
        cx.run_until_parked();
        let events = record_events(cx, &spaces);
        let body = cx.debug_bounds("spaces-body").unwrap();
        cx.simulate_click(body.center(), Modifiers::none());
        cx.run_until_parked();
        assert!(events.borrow().is_empty());
    }
}
