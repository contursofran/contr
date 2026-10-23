//! The status bar along the bottom of the frame. Its left side shows each plan's usage as the
//! percent left and a bar. Its right side shows the message slot, host CPU and memory, the contr
//! version, and the herdr version while herdr is connected.
//!
//! The bar is a cached view: it re-renders only when the plan usage, the host reading, the message
//! or the herdr version changes. A herdr change that keeps the version does not repaint it.

use crate::herdr::{GlobalHerdr, Herdr};
use crate::host::GlobalHost;
use crate::status_message::GlobalStatusMessage;
use crate::theme::*;
use crate::usage::{GlobalUsage, unix_now};
use contr_herdr::ConnState;
use contr_usage::model::{PLANS, PlanId, left_label};
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

/// One plan on the left side, e.g. "claude ▬ 62%". `left` is the whole percent left, `None`
/// without data.
#[derive(Debug, Clone, Copy, PartialEq)]
struct PlanSegment {
    plan: PlanId,
    left: Option<f64>,
}

impl PlanSegment {
    /// The item as plain text, e.g. "claude 62%". The bar has no text.
    #[cfg(test)]
    fn text(&self) -> String {
        format!("{} {}", self.plan.label(), left_label(self.left))
    }

    /// The label, the track, and the percent. The track is empty without data.
    fn view(&self) -> Div {
        let id = self.plan.id();
        let track = div()
            .debug_selector(move || format!("usage-{id}-bar"))
            .relative()
            .flex_none()
            .w(px(USAGE_BAR_WIDTH))
            .h(px(USAGE_BAR_HEIGHT))
            .rounded(px(USAGE_BAR_RADIUS))
            .bg(rgb(LINE));
        let track = match self.left {
            Some(left) => track.child(fill(id, left)),
            None => track,
        };
        div()
            .debug_selector(move || format!("usage-{id}"))
            .flex()
            .flex_none()
            .items_center()
            .gap(px(STATUS_GAP))
            .whitespace_nowrap()
            .child(div().text_color(rgb(TEXT_DIM)).child(self.plan.label()))
            .child(track)
            .child(
                div()
                    .debug_selector(move || format!("usage-{id}-pct"))
                    .text_color(rgb(TEXT_2))
                    .child(left_label(self.left)),
            )
    }
}

/// The filled part of a track: `USAGE_BAR_WIDTH` × left / 100. Under `USAGE_LOW_LEFT` it is white,
/// because the owner acts; otherwise it is dim. Its selector is `usage-{id}-low` or
/// `usage-{id}-fill`.
fn fill(id: &'static str, left: f64) -> Div {
    let low = left < f64::from(USAGE_LOW_LEFT);
    let (selector, colour) = if low {
        (format!("usage-{id}-low"), YOU_BG)
    } else {
        (format!("usage-{id}-fill"), TEXT_DIM)
    };
    let width = USAGE_BAR_WIDTH * ((left / 100.) as f32);
    div()
        .debug_selector(move || selector)
        .absolute()
        .top_0()
        .left_0()
        .h_full()
        .w(px(width))
        .rounded(px(USAGE_BAR_RADIUS))
        .bg(rgb(colour))
}

/// The left side's items in `PLANS` order, read from the usage global.
fn plan_segments(cx: &App) -> Vec<PlanSegment> {
    let usage = cx.global::<GlobalUsage>().0.read(cx);
    let now = unix_now();
    PLANS
        .into_iter()
        .map(|plan| PlanSegment {
            plan,
            left: usage.left(plan, now),
        })
        .collect()
}

/// The herdr version to show: the server's version while herdr is connected.
fn herdr_version_of(herdr: &Herdr) -> Option<String> {
    match &herdr.live.state {
        ConnState::Connected { server_version } => Some(server_version.clone()),
        _ => None,
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
    if let Some(server_version) = herdr_version_of(cx.global::<GlobalHerdr>().0.read(cx)) {
        segments.push(Segment {
            id: "herdr-version",
            tone: Tone::Meta,
            label: "herdr".into(),
            value: Some(server_version),
        });
    }
    segments
}

fn separator() -> Div {
    div().flex_none().text_color(rgb(TEXT_DIM_2)).child("·")
}

pub struct StatusBar {
    /// The herdr version the bar last repainted for. Herdr changes that keep it repaint nothing.
    herdr_version: Option<String>,
    _subs: Vec<Subscription>,
}

impl StatusBar {
    /// Repaints when the plan usage, the host reading, the message or the herdr version changes.
    /// Needs `Usage::init`, `Host::init`, `StatusMessage::init` and `Herdr::init` first.
    pub fn new(cx: &mut Context<Self>) -> Self {
        let usage = cx.global::<GlobalUsage>().0.clone();
        let host = cx.global::<GlobalHost>().0.clone();
        let message = cx.global::<GlobalStatusMessage>().0.clone();
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let herdr_version = herdr_version_of(herdr.read(cx));
        Self {
            herdr_version,
            _subs: vec![
                cx.observe(&usage, |_, _, cx| cx.notify()),
                cx.observe(&host, |_, _, cx| cx.notify()),
                cx.observe(&message, |_, _, cx| cx.notify()),
                cx.observe(&herdr, |bar, herdr, cx| {
                    let version = herdr_version_of(herdr.read(cx));
                    if version != bar.herdr_version {
                        bar.herdr_version = version;
                        cx.notify();
                    }
                }),
            ],
        }
    }
}

impl Render for StatusBar {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let mut usage = div()
            .debug_selector(|| "usage".into())
            .flex_none()
            .flex()
            .items_center()
            .gap(px(STATUS_GAP));
        for (index, segment) in plan_segments(cx).iter().enumerate() {
            if index > 0 {
                usage = usage.child(separator());
            }
            usage = usage.child(segment.view());
        }
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
            .child(usage)
            .child(right)
    }
}

#[cfg(test)]
impl StatusBar {
    /// The texts of the right side's items, in display order, without separators.
    pub fn right_texts(cx: &App) -> Vec<String> {
        right_segments(cx).iter().map(Segment::text).collect()
    }

    /// The texts of the left side's plans, in `PLANS` order, e.g. "claude 62%".
    pub fn left_texts(cx: &App) -> Vec<String> {
        plan_segments(cx).iter().map(PlanSegment::text).collect()
    }
}

/// Registers a detached host (a fixed reading, no sampling), an empty message slot and plan usage
/// with no data, for the views' tests.
#[cfg(test)]
pub fn init_detached(cx: &mut App, stats: Option<crate::host::HostStats>) {
    let host = cx.new(|cx| crate::host::Host::detached(stats, cx));
    cx.set_global(GlobalHost(host));
    crate::status_message::StatusMessage::init(cx);
    let usage = cx.new(|cx| crate::usage::Usage::detached(crate::usage::unread(), cx));
    cx.set_global(GlobalUsage(usage));
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
    use crate::usage::{Plans, fixture};
    use contr_herdr::{HerdrTree, HerdrUpdate, LiveHerdr};
    use gpui::{AppContext, Entity, Subscription, TestAppContext, px, size};
    use std::cell::Cell;
    use std::rc::Rc;
    use std::sync::Arc;
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

    /// Sets the usage's plans, as a read would, and repaints.
    fn set_plans(cx: &mut gpui::VisualTestContext, plans: Plans) {
        let usage = cx.read(|app| app.global::<GlobalUsage>().0.clone());
        cx.update(|_, app| {
            usage.update(app, |usage, cx| {
                usage.plans = plans;
                cx.notify();
            })
        });
        cx.run_until_parked();
    }

    fn left_texts(cx: &mut gpui::VisualTestContext) -> Vec<String> {
        cx.read(StatusBar::left_texts)
    }

    /// The width of a rendered element, in pixels.
    fn width_of(cx: &mut gpui::VisualTestContext, id: &'static str) -> Option<f32> {
        cx.debug_bounds(id)
            .map(|bounds| f32::from(bounds.size.width))
    }

    /// A status bar outside any window, with a count of its notifications. Its text is read from
    /// the app's state, so the count is the repaint.
    struct CountedBar {
        // Kept alive: the subscription stops when the bar is dropped.
        _bar: Entity<StatusBar>,
        notified: Rc<Cell<usize>>,
        _subscription: Subscription,
    }

    /// Registers the globals as `init_detached` does, with a host at 40 % and 50 % and a herdr in
    /// `state`, then creates a status bar and counts its notifications.
    fn counted_bar(cx: &mut TestAppContext, state: ConnState) -> CountedBar {
        cx.update(|app| init_detached(app, Some(stats(40., 50, 100))));
        cx.update(|app| {
            let live = LiveHerdr { state, tree: None };
            let herdr = app.new(|cx| Herdr::detached(live, cx));
            app.set_global(GlobalHerdr(herdr));
        });
        let bar = cx.update(|app| app.new(StatusBar::new));
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let subscription =
            cx.update(|app| app.observe(&bar, move |_, _| counter.set(counter.get() + 1)));
        CountedBar {
            _bar: bar,
            notified,
            _subscription: subscription,
        }
    }

    /// Applies one herdr update to the app's herdr, as the reader would.
    fn apply_herdr(cx: &mut TestAppContext, update: HerdrUpdate) {
        let herdr = cx.read(|app| app.global::<GlobalHerdr>().0.clone());
        cx.update(|app| herdr.update(app, |herdr, cx| herdr.apply(update, cx)));
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
        assert_eq!(texts(cx), ["CPU 40%", "MEM 50%", CONTR_TEXT]);
    }

    #[gpui::test]
    fn a_changed_reading_repaints_the_bar_once(cx: &mut TestAppContext) {
        let counted = counted_bar(cx, connected());
        assert_eq!(cx.read(StatusBar::right_texts)[0], "CPU 40%");

        let host = cx.read(|app| app.global::<GlobalHost>().0.clone());
        cx.update(|app| {
            host.update(app, |host, cx| {
                host.stats = Some(stats(55., 50, 100));
                cx.notify();
            })
        });
        cx.run_until_parked();
        assert_eq!(counted.notified.get(), 1);
        assert_eq!(cx.read(StatusBar::right_texts)[0], "CPU 55%");
    }

    #[gpui::test]
    fn a_herdr_change_that_keeps_the_version_does_not_repaint_the_bar(cx: &mut TestAppContext) {
        let counted = counted_bar(cx, connected());
        let tree = HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: None,
            workspaces: Vec::new(),
        };
        apply_herdr(cx, HerdrUpdate::Tree(Arc::new(tree)));
        assert_eq!(
            counted.notified.get(),
            0,
            "the tree changed, the version did not"
        );
        assert_eq!(
            cx.read(StatusBar::right_texts),
            ["CPU 40%", "MEM 50%", CONTR_TEXT, "herdr 0.9.3"]
        );
    }

    #[gpui::test]
    fn a_new_herdr_version_repaints_the_bar_once(cx: &mut TestAppContext) {
        let counted = counted_bar(cx, connected());
        apply_herdr(
            cx,
            HerdrUpdate::State(ConnState::Connected {
                server_version: "0.9.4".into(),
            }),
        );
        assert_eq!(counted.notified.get(), 1);
        assert_eq!(cx.read(StatusBar::right_texts)[3], "herdr 0.9.4");

        apply_herdr(
            cx,
            HerdrUpdate::State(ConnState::NotRunning {
                reason: "gone".into(),
            }),
        );
        assert_eq!(counted.notified.get(), 2);
        assert_eq!(
            cx.read(StatusBar::right_texts),
            ["CPU 40%", "MEM 50%", CONTR_TEXT]
        );
    }

    #[gpui::test]
    fn each_plan_shows_its_left_percent_and_a_bar_to_match(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 1440.);
        set_plans(cx, fixture(Some(38.4), Some(60.)));
        assert_eq!(left_texts(cx), ["claude 62%", "codex 40%"]);
        // The track is 36 px: 62 % of it is 22.32 px, 40 % is 14.4 px.
        let claude = width_of(cx, "usage-claude-fill").unwrap();
        let codex = width_of(cx, "usage-codex-fill").unwrap();
        assert!((claude - 22.32).abs() < 0.5, "claude fill is {claude} px");
        assert!((codex - 14.4).abs() < 0.5, "codex fill is {codex} px");
        assert!(cx.debug_bounds("usage-claude-pct").is_some());
    }

    #[gpui::test]
    fn a_low_plan_fills_white_and_the_others_stay_grey(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 1440.);
        // Claude 62 % left, Codex 12 % left (under 20).
        set_plans(cx, fixture(Some(38.4), Some(88.)));
        assert!(cx.debug_bounds("usage-codex-low").is_some());
        assert!(cx.debug_bounds("usage-codex-fill").is_none());
        assert!(cx.debug_bounds("usage-claude-low").is_none());
        assert!(cx.debug_bounds("usage-claude-fill").is_some());

        // Exactly 20 % left is not under the limit.
        set_plans(cx, fixture(Some(38.4), Some(80.)));
        assert!(cx.debug_bounds("usage-codex-low").is_none());
        assert!(cx.debug_bounds("usage-codex-fill").is_some());
    }

    #[gpui::test]
    fn a_plan_without_data_shows_a_dash_and_an_empty_track(cx: &mut TestAppContext) {
        let cx = frame(cx, Some(stats(40., 50, 100)), 1440.);
        assert_eq!(left_texts(cx), ["claude –", "codex –"]);
        assert!(cx.debug_bounds("usage-claude-bar").is_some());
        assert!(cx.debug_bounds("usage-claude-fill").is_none());
        assert!(cx.debug_bounds("usage-claude-low").is_none());

        set_plans(cx, fixture(Some(38.4), None));
        assert_eq!(left_texts(cx), ["claude 62%", "codex –"]);
        set_plans(cx, fixture(None, None));
        assert_eq!(left_texts(cx), ["claude –", "codex –"]);
        assert!(cx.debug_bounds("usage-claude-fill").is_none());
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
