//! The status bar's message slot: one short line of action feedback that clears itself.

use gpui::{App, Context, Entity, Global, SharedString, Task, prelude::*};
use std::time::Duration;

/// How long a message stays in the bar before it clears.
pub const MESSAGE_TTL: Duration = Duration::from_secs(6);

/// The message shown in the status bar, if any.
pub struct StatusMessage {
    pub text: Option<SharedString>,
    _clear: Option<Task<()>>,
}

/// The app-wide handle to the message slot, created by `StatusMessage::init`.
pub struct GlobalStatusMessage(pub Entity<StatusMessage>);

impl Global for GlobalStatusMessage {}

impl StatusMessage {
    /// Registers the message slot as a global. It starts empty.
    pub fn init(cx: &mut App) {
        let message = cx.new(|_| Self {
            text: None,
            _clear: None,
        });
        cx.set_global(GlobalStatusMessage(message));
    }

    /// Replaces the message and restarts the `MESSAGE_TTL` countdown. Notifies only when the text
    /// changes, so a repeated message repaints nothing.
    pub fn show(&mut self, text: impl Into<SharedString>, cx: &mut Context<Self>) {
        let text = text.into();
        if self.text.as_ref() != Some(&text) {
            self.text = Some(text);
            cx.notify();
        }
        // Replacing the task drops the old countdown, so only the newest one clears the message.
        self._clear = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(MESSAGE_TTL).await;
            let _ = this.update(cx, |message, cx| message.clear(cx));
        }));
    }

    fn clear(&mut self, cx: &mut Context<Self>) {
        if self.text.take().is_some() {
            cx.notify();
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use gpui::TestAppContext;
    use std::cell::Cell;
    use std::rc::Rc;

    fn message(cx: &mut TestAppContext) -> Entity<StatusMessage> {
        cx.read(|app| app.global::<GlobalStatusMessage>().0.clone())
    }

    fn shown(cx: &mut TestAppContext, message: &Entity<StatusMessage>) -> Option<String> {
        cx.read(|app| message.read(app).text.as_ref().map(|t| t.to_string()))
    }

    fn show(cx: &mut TestAppContext, message: &Entity<StatusMessage>, text: &'static str) {
        cx.update(|app| message.update(app, |m, cx| m.show(text, cx)));
    }

    /// Runs what is due, then moves the test clock by `by` and runs what that makes due.
    fn advance(cx: &mut TestAppContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    #[gpui::test]
    fn starts_empty(cx: &mut TestAppContext) {
        cx.update(StatusMessage::init);
        let message = message(cx);
        assert_eq!(shown(cx, &message), None);
    }

    #[gpui::test]
    fn show_replaces_the_text(cx: &mut TestAppContext) {
        cx.update(StatusMessage::init);
        let message = message(cx);
        show(cx, &message, "D7 → By type · sent");
        assert_eq!(shown(cx, &message).as_deref(), Some("D7 → By type · sent"));
        show(cx, &message, "T12 done");
        assert_eq!(shown(cx, &message).as_deref(), Some("T12 done"));
    }

    #[gpui::test]
    fn clears_exactly_after_the_ttl(cx: &mut TestAppContext) {
        cx.update(StatusMessage::init);
        let message = message(cx);
        show(cx, &message, "sent");

        advance(cx, MESSAGE_TTL - Duration::from_millis(1));
        assert_eq!(shown(cx, &message).as_deref(), Some("sent"));
        advance(cx, Duration::from_millis(1));
        assert_eq!(shown(cx, &message), None);
    }

    #[gpui::test]
    fn a_second_show_restarts_the_countdown(cx: &mut TestAppContext) {
        cx.update(StatusMessage::init);
        let message = message(cx);
        show(cx, &message, "first");
        advance(cx, Duration::from_secs(4));

        show(cx, &message, "second");
        advance(cx, Duration::from_secs(4));
        // The first countdown would have ended at 6 s; the second ends at 10 s.
        assert_eq!(shown(cx, &message).as_deref(), Some("second"));

        advance(cx, Duration::from_secs(2));
        assert_eq!(shown(cx, &message), None);
    }

    #[gpui::test]
    fn notifies_only_when_the_text_changes(cx: &mut TestAppContext) {
        cx.update(StatusMessage::init);
        let message = message(cx);
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _subscription =
            cx.update(|app| app.observe(&message, move |_, _| counter.set(counter.get() + 1)));

        show(cx, &message, "sent");
        assert_eq!(notified.get(), 1);

        // Same text: the countdown restarts, but nothing shown changes.
        show(cx, &message, "sent");
        cx.run_until_parked();
        assert_eq!(notified.get(), 1);

        show(cx, &message, "saved");
        assert_eq!(notified.get(), 2);
    }

    #[gpui::test]
    fn clearing_notifies_once(cx: &mut TestAppContext) {
        cx.update(StatusMessage::init);
        let message = message(cx);
        show(cx, &message, "sent");
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _subscription =
            cx.update(|app| app.observe(&message, move |_, _| counter.set(counter.get() + 1)));

        advance(cx, MESSAGE_TTL);
        assert_eq!(notified.get(), 1);
        assert_eq!(shown(cx, &message), None);
    }
}
