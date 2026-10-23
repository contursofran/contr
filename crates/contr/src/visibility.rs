//! Whether the frame window is presented to the user. Background work (the host sampler) follows
//! this flag, so a minimized, covered or sleeping window costs no wake-ups.

use gpui::{App, Context, Entity, Global, Window, prelude::*};

/// `visible` is false while the window is minimized, fully covered, on another Space or on a
/// sleeping display (see `Window::is_visible`).
pub struct Visibility {
    pub visible: bool,
}

/// The app-wide handle to the frame window's visibility.
pub struct GlobalVisibility(pub Entity<Visibility>);

impl Global for GlobalVisibility {}

impl Visibility {
    /// Creates the entity, visible until a window says otherwise, and registers it as a global.
    pub fn init(cx: &mut App) {
        let visibility = cx.new(|_| Self { visible: true });
        cx.set_global(GlobalVisibility(visibility));
    }

    /// Sets `visible` from the window now, then follows its visibility changes for the life of the
    /// window. Needs `Visibility::init` first.
    pub fn watch(window: &mut Window, cx: &mut App) {
        let visibility = cx.global::<GlobalVisibility>().0.clone();
        visibility.update(cx, |this, cx| this.set(window.is_visible(), cx));
        window
            .observe_window_visibility(move |state, _, cx| {
                visibility.update(cx, |this, cx| this.set(state.is_visible(), cx));
            })
            .detach();
    }

    /// Stores the new value and notifies observers only when it changed.
    pub fn set(&mut self, visible: bool, cx: &mut Context<Self>) {
        if self.visible != visible {
            self.visible = visible;
            cx.notify();
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use gpui::{EmptyView, TestAppContext, WindowVisibility};
    use std::cell::Cell;
    use std::rc::Rc;

    fn visibility(cx: &mut TestAppContext) -> Entity<Visibility> {
        cx.read(|app| app.global::<GlobalVisibility>().0.clone())
    }

    fn set(cx: &mut TestAppContext, visibility: &Entity<Visibility>, visible: bool) {
        cx.update(|app| visibility.update(app, |v, cx| v.set(visible, cx)));
    }

    #[gpui::test]
    fn set_notifies_once_per_change(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        let visibility = visibility(cx);
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _subscription =
            cx.update(|app| app.observe(&visibility, move |_, _| counter.set(counter.get() + 1)));

        // Starts visible, so visible again is no change.
        set(cx, &visibility, true);
        assert_eq!(notified.get(), 0);

        set(cx, &visibility, false);
        set(cx, &visibility, false);
        assert_eq!(notified.get(), 1);
        assert!(!cx.read(|app| visibility.read(app).visible));

        set(cx, &visibility, true);
        assert_eq!(notified.get(), 2);
    }

    #[gpui::test]
    fn watch_follows_the_window(cx: &mut TestAppContext) {
        cx.update(Visibility::init);
        let (_view, cx) = cx.add_window_view(|window, cx| {
            Visibility::watch(window, cx);
            EmptyView
        });
        let visibility = visibility(cx);
        assert!(cx.read(|app| visibility.read(app).visible));

        cx.simulate_visibility_change(WindowVisibility::Hidden);
        assert!(!cx.read(|app| visibility.read(app).visible));

        cx.simulate_visibility_change(WindowVisibility::Visible);
        assert!(cx.read(|app| visibility.read(app).visible));
    }
}
