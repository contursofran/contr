//! The status bar along the bottom of the frame. Its right side shows the message slot, host CPU
//! and memory, the contr version, and the herdr version while herdr is connected. The left side
//! (plan usage) comes with the usage entity.
//!
//! The bar is a cached view: it re-renders only when the host reading, the message or the herdr
//! connection changes.

use crate::herdr::GlobalHerdr;
use crate::host::GlobalHost;
use crate::status_message::GlobalStatusMessage;
use crate::theme::*;
use contr_herdr::ConnState;
use gpui::{
    App, Context, Div, IntoElement, ParentElement, Render, Styled, Subscription, Window, div,
    prelude::*, px, rgb,
};

/// Shown in place of a reading that has not arrived yet.
const NO_READING: &str = "–";

/// How a segment is coloured (DESIGN.md: labels dim, numbers secondary, versions dim-2).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Tone {
    /// The message slot. It truncates before anything else when the window is narrow.
    Message,
    /// A live reading: dim label, secondary value.
    Reading,
    /// A version: dim-2 label and value.
    Meta,
}

/// One item on the right side of the bar.
#[derive(Debug, Clone, PartialEq)]
struct Segment {
    id: &'static str,
    tone: Tone,
    /// "CPU", "MEM", "contr", "herdr", or the whole message text.
    label: String,
    /// "38%", "–", "0.1.0", "0.9.3". None for the message.
    value: Option<String>,
}

impl Segment {
    /// The item as plain text, e.g. "CPU 38%".
    fn text(&self) -> String {
        match &self.value {
            Some(value) => format!("{} {value}", self.label),
            None => self.label.clone(),
        }
    }

    fn view(&self) -> Div {
        let id = self.id;
        let row = div()
            .debug_selector(move || id.into())
            .flex()
            .items_center()
            .gap(px(STATUS_GAP))
            .whitespace_nowrap();
        match self.tone {
            Tone::Message => row
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .text_color(rgb(TEXT_2))
                .child(self.label.clone()),
            Tone::Reading => {
                let row = row
                    .flex_none()
                    .child(div().text_color(rgb(TEXT_DIM)).child(self.label.clone()));
                match &self.value {
                    Some(value) => row.child(div().text_color(rgb(TEXT_2)).child(value.clone())),
                    None => row,
                }
            }
            Tone::Meta => row
                .flex_none()
                .text_color(rgb(TEXT_DIM_2))
                .child(self.text()),
        }
    }
}

/// The right side's items in display order, read from the app's globals.
fn right_segments(cx: &App) -> Vec<Segment> {
    let host = cx.global::<GlobalHost>().0.read(cx).stats;
    let message = cx.global::<GlobalStatusMessage>().0.read(cx).text.clone();
    let (cpu, mem) = match host {
        Some(stats) => (stats.cpu_label(), stats.mem_label()),
        None => (NO_READING.into(), NO_READING.into()),
    };
    let mut segments = Vec::with_capacity(4);
    if let Some(text) = message {
        segments.push(Segment {
            id: "status-message",
            tone: Tone::Message,
            label: text.to_string(),
            value: None,
        });
    }
    segments.push(Segment {
        id: "host-cpu-status",
        tone: Tone::Reading,
        label: "CPU".into(),
        value: Some(cpu),
    });
    segments.push(Segment {
        id: "host-mem-status",
        tone: Tone::Reading,
        label: "MEM".into(),
        value: Some(mem),
    });
    segments.push(Segment {
        id: "contr-version",
        tone: Tone::Meta,
        label: "contr".into(),
        value: Some(env!("CARGO_PKG_VERSION").into()),
    });
    if let ConnState::Connected { server_version } =
        &cx.global::<GlobalHerdr>().0.read(cx).live.state
    {
        segments.push(Segment {
            id: "herdr-version",
            tone: Tone::Meta,
            label: "herdr".into(),
            value: Some(server_version.clone()),
        });
    }
    segments
}

fn separator() -> Div {
    div().flex_none().text_color(rgb(TEXT_DIM_2)).child("·")
}

pub struct StatusBar {
    _subs: Vec<Subscription>,
}

impl StatusBar {
    /// Repaints when the host reading, the message or the herdr connection changes. Needs
    /// `Host::init`, `StatusMessage::init` and `Herdr::init` first.
    pub fn new(cx: &mut Context<Self>) -> Self {
        let host = cx.global::<GlobalHost>().0.clone();
        let message = cx.global::<GlobalStatusMessage>().0.clone();
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        Self {
            _subs: vec![
                cx.observe(&host, |_, _, cx| cx.notify()),
                cx.observe(&message, |_, _, cx| cx.notify()),
                cx.observe(&herdr, |_, _, cx| cx.notify()),
            ],
        }
    }
}

impl Render for StatusBar {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let mut right = div()
            .debug_selector(|| "status-right".into())
            .flex_1()
            .min_w_0()
            .flex()
            .items_center()
            .justify_end()
            .gap(px(STATUS_GAP));
        for (index, segment) in right_segments(cx).iter().enumerate() {
            if index > 0 {
                right = right.child(separator());
            }
            right = right.child(segment.view());
        }
        div()
            .debug_selector(|| "status-bar".into())
            .size_full()
            .flex()
            .items_center()
            .justify_between()
            .px(px(STATUS_PAD_X))
            .bg(rgb(BG_BARS))
            .border_t_1()
            .border_color(rgb(LINE))
            .child(div().debug_selector(|| "usage".into()).flex_none())
            .child(right)
    }
}

#[cfg(test)]
impl StatusBar {
    /// The texts of the right side's items, in display order, without separators.
    pub fn right_texts(cx: &App) -> Vec<String> {
        right_segments(cx).iter().map(Segment::text).collect()
    }
}

/// Registers a detached host (a fixed reading, no sampling) and an empty message slot, for the
/// views' tests.
#[cfg(test)]
pub fn init_detached(cx: &mut App, stats: Option<crate::host::HostStats>) {
    let host = cx.new(|cx| crate::host::Host::detached(stats, cx));
    cx.set_global(GlobalHost(host));
    crate::status_message::StatusMessage::init(cx);
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::frame::Frame;
    use crate::herdr::{GlobalHerdr, Herdr};
    use crate::host::HostStats;
    use crate::selection::Selection;
    use crate::status_message::{GlobalStatusMessage, MESSAGE_TTL, StatusMessage};
    use contr_herdr::{HerdrUpdate, LiveHerdr};
    use gpui::{AppContext, Entity, TestAppContext, px, size};
    use std::time::Duration;

    /// The contr version item, as the bar shows it.
    const CONTR_TEXT: &str = concat!("contr ", env!("CARGO_PKG_VERSION"));

    fn stats(cpu_percent: f32, mem_used: u64, mem_total: u64) -> HostStats {
        HostStats {
            cpu_percent,
            mem_used,
            mem_total,
        }
    }

    fn connected() -> ConnState {
        ConnState::Connected {
            server_version: "0.9.3".into(),
        }
    }

    /// Opens the frame (which holds the status bar) at `width` × 900 with a detached host and a
    /// detached herdr that is connected.
    fn frame(
        cx: &mut TestAppContext,
        host: Option<HostStats>,
        width: f32,
    ) -> &mut gpui::VisualTestContext {
        frame_with(cx, host, width, connected())
    }

    /// As `frame`, with the detached herdr in `state`.
    fn frame_with(
        cx: &mut TestAppContext,
        host: Option<HostStats>,
        width: f32,
        state: ConnState,
    ) -> &mut gpui::VisualTestContext {
        cx.update(Selection::init);
        cx.update(|app| init_detached(app, host));
        cx.update(|app| {
            let live = LiveHerdr { state, tree: None };
            let herdr = app.new(|cx| Herdr::detached(live, cx));
            app.set_global(GlobalHerdr(herdr));
        });
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(width), px(900.)));
        cx.run_until_parked();
        cx
    }

    /// Applies `state` to the app's herdr, as the reader's update would.
    fn set_herdr(cx: &mut gpui::VisualTestContext, state: ConnState) {
        let herdr = cx.read(|app| app.global::<GlobalHerdr>().0.clone());
        cx.update(|_, app| {
            herdr.update(app, |herdr, cx| herdr.apply(HerdrUpdate::State(state), cx))
        });
        cx.run_until_parked();
    }

    fn message(cx: &mut gpui::VisualTestContext) -> Entity<StatusMessage> {
        cx.read(|app| app.global::<GlobalStatusMessage>().0.clone())
    }

    fn show(cx: &mut gpui::VisualTestContext, text: &str) {
        let message = message(cx);
        let text = text.to_string();
        cx.update(|_, app| message.update(app, |m, cx| m.show(text, cx)));
        cx.run_until_parked();
    }

    fn texts(cx: &mut gpui::VisualTestContext) -> Vec<String> {
        cx.read(StatusBar::right_texts)
    }

    fn advance(cx: &mut gpui::VisualTestContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    #[gpui::test]
    fn shows_cpu_and_memory_then_the_versions(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(37.6, 62, 100)), 1440.);
        assert_eq!(texts(cx), ["CPU 38%", "MEM 62%", CONTR_TEXT, "herdr 0.9.3"]);
        assert!(cx.debug_bounds("host-cpu-status").is_some());
        let mem = cx.debug_bounds("host-mem-status").unwrap();
        let version = cx.debug_bounds("contr-version").unwrap();
        assert!(
            version.origin.x > mem.origin.x,
            "the version is right of MEM"
        );
    }

    #[gpui::test]
    fn no_reading_shows_dashes(cx: &mut TestAppContext) {
        let cx = frame(cx, None, 1440.);
        assert_eq!(texts(cx), ["CPU –", "MEM –", CONTR_TEXT, "herdr 0.9.3"]);
    }

    #[gpui::test]
    fn a_connected_herdr_shows_its_version_after_contr(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 1440.);
        assert_eq!(texts(cx)[3], "herdr 0.9.3");
        let contr = cx.debug_bounds("contr-version").unwrap();
        let herdr = cx.debug_bounds("herdr-version").unwrap();
        assert!(herdr.origin.x > contr.origin.x, "herdr is right of contr");
    }

    #[gpui::test]
    fn not_running_herdr_shows_no_version(cx: &mut TestAppContext) {
        let state = ConnState::NotRunning {
            reason: "no socket".into(),
        };
        let cx = frame_with(cx, Some(stats(40., 50, 100)), 1440., state);
        assert_eq!(texts(cx), ["CPU 40%", "MEM 50%", CONTR_TEXT]);
        assert!(cx.debug_bounds("herdr-version").is_none());
    }

    #[gpui::test]
    fn herdr_version_follows_the_connection(cx: &mut TestAppContext) {
        let cx = frame_with(cx, Some(stats(40., 50, 100)), 1440., ConnState::Connecting);
        assert!(cx.debug_bounds("herdr-version").is_none());

        set_herdr(cx, connected());
        assert_eq!(texts(cx)[3], "herdr 0.9.3");
        assert!(
            cx.debug_bounds("herdr-version").is_some(),
            "the bar repaints when herdr connects"
        );

        set_herdr(
            cx,
            ConnState::Incompatible {
                message: "herdr replied in a form contr does not read".into(),
            },
        );
        assert_eq!(texts(cx).len(), 3);
        assert!(cx.debug_bounds("herdr-version").is_none());

        set_herdr(
            cx,
            ConnState::NotRunning {
                reason: "gone".into(),
            },
        );
        assert!(cx.debug_bounds("herdr-version").is_none());
    }

    #[gpui::test]
    fn a_changed_reading_repaints_the_bar(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 1440.);
        assert_eq!(texts(cx)[0], "CPU 40%");

        let host = cx.read(|app| app.global::<GlobalHost>().0.clone());
        cx.update(|_, app| {
            host.update(app, |host, cx| {
                host.stats = Some(stats(55., 50, 100));
                cx.notify();
            })
        });
        cx.run_until_parked();
        assert_eq!(texts(cx)[0], "CPU 55%");
    }

    #[gpui::test]
    fn message_sits_left_of_the_readings_and_clears_after_its_ttl(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 1440.);
        show(cx, "D7 → By type · sent");
        assert_eq!(texts(cx)[0], "D7 → By type · sent");
        let message_bounds = cx.debug_bounds("status-message").unwrap();
        let cpu = cx.debug_bounds("host-cpu-status").unwrap();
        assert!(message_bounds.origin.x < cpu.origin.x);

        advance(cx, MESSAGE_TTL);
        assert_eq!(texts(cx), ["CPU 40%", "MEM 50%", CONTR_TEXT, "herdr 0.9.3"]);
        assert!(cx.debug_bounds("status-message").is_none());
    }

    #[gpui::test]
    fn narrow_window_keeps_the_readings_inside_the_bar(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 960.);
        show(cx, &"a very long message ".repeat(20));
        let message = cx.debug_bounds("status-message").unwrap();
        let cpu = cx.debug_bounds("host-cpu-status").unwrap();
        assert!(
            message.origin.x + message.size.width <= cpu.origin.x,
            "message truncates, not CPU"
        );
        let right = cx.debug_bounds("status-right").unwrap();
        assert!(right.origin.x + right.size.width <= px(960.));
        for id in [
            "host-cpu-status",
            "host-mem-status",
            "contr-version",
            "herdr-version",
        ] {
            let bounds = cx.debug_bounds(id).unwrap();
            assert!(bounds.origin.x + bounds.size.width <= px(960.), "{id} fits");
        }
        assert_eq!(cx.debug_bounds("status-bar").unwrap().size.height, px(26.));
    }
}
