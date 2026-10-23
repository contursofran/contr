//! The ten status icons from docs/DESIGN.md, one set for sidebar, tabs, board stages and needs-you rows.

use crate::theme::{ICON_FAINT, ICON_GREY, ICON_SIZE, ICON_YOU, SPIN_PERIOD_MS};
use gpui::{
    Animation, AnimationExt, App, ElementId, IntoElement, ParentElement, Pixels, RenderOnce, Role,
    StatefulInteractiveElement, Styled, Transformation, Window, div, percentage, prelude::*, px,
    rgb, svg,
};
use std::time::Duration;

static NEEDS_YOU: &[u8] = include_bytes!("../assets/icons/needs_you.svg");
static DECIDE: &[u8] = include_bytes!("../assets/icons/decide.svg");
static DO: &[u8] = include_bytes!("../assets/icons/do.svg");
static BLOCKED: &[u8] = include_bytes!("../assets/icons/blocked.svg");
static WORKING_RING: &[u8] = include_bytes!("../assets/icons/working_ring.svg");
static WORKING_ARC: &[u8] = include_bytes!("../assets/icons/working_arc.svg");
static WAITING: &[u8] = include_bytes!("../assets/icons/waiting.svg");
static IDLE: &[u8] = include_bytes!("../assets/icons/idle.svg");
static DONE: &[u8] = include_bytes!("../assets/icons/done.svg");
static FYI: &[u8] = include_bytes!("../assets/icons/fyi.svg");
static NOT_STARTED: &[u8] = include_bytes!("../assets/icons/not_started.svg");

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Status {
    NeedsYou,
    Decide,
    Do,
    Blocked,
    Working,
    Waiting,
    Idle,
    Done,
    Fyi,
    NotStarted,
}

impl Status {
    /// Legend order (docs/DESIGN.md table order).
    pub const ALL: [Status; 10] = [
        Status::NeedsYou,
        Status::Decide,
        Status::Do,
        Status::Blocked,
        Status::Working,
        Status::Waiting,
        Status::Idle,
        Status::Done,
        Status::Fyi,
        Status::NotStarted,
    ];

    /// Accessible label; also the legend text.
    pub fn label(self) -> &'static str {
        match self {
            Status::NeedsYou => "needs you",
            Status::Decide => "decide",
            Status::Do => "do",
            Status::Blocked => "blocked",
            Status::Working => "working",
            Status::Waiting => "waiting",
            Status::Idle => "idle",
            Status::Done => "done",
            Status::Fyi => "fyi",
            Status::NotStarted => "not started",
        }
    }

    /// Kebab id for element ids and debug selectors.
    pub fn slug(self) -> &'static str {
        match self {
            Status::NeedsYou => "needs-you",
            Status::Decide => "decide",
            Status::Do => "do",
            Status::Blocked => "blocked",
            Status::Working => "working",
            Status::Waiting => "waiting",
            Status::Idle => "idle",
            Status::Done => "done",
            Status::Fyi => "fyi",
            Status::NotStarted => "not-started",
        }
    }

    /// Static glyph and colour. `Working` returns its ring (the arc is drawn on top).
    fn glyph(self) -> (&'static [u8], u32) {
        match self {
            Status::NeedsYou => (NEEDS_YOU, ICON_YOU),
            Status::Decide => (DECIDE, ICON_YOU),
            Status::Do => (DO, ICON_YOU),
            Status::Blocked => (BLOCKED, ICON_YOU),
            Status::Working => (WORKING_RING, ICON_FAINT),
            Status::Waiting => (WAITING, ICON_GREY),
            Status::Idle => (IDLE, ICON_GREY),
            Status::Done => (DONE, ICON_GREY),
            Status::Fyi => (FYI, ICON_GREY),
            Status::NotStarted => (NOT_STARTED, ICON_FAINT),
        }
    }
}

/// One status icon. `id` must be unique among its siblings (GPUI composes it with ancestor ids).
///
/// A visible `Working` icon re-renders the view that contains it every frame; put it in a small
/// view embedded with `.cached(..)` so the rest of the window is replayed.
#[derive(IntoElement)]
pub struct StatusIcon {
    id: ElementId,
    status: Status,
    size: Pixels,
}

impl StatusIcon {
    pub fn new(id: impl Into<ElementId>, status: Status) -> Self {
        Self {
            id: id.into(),
            status,
            size: px(ICON_SIZE),
        }
    }

    /// 12–14px; default ICON_SIZE (14).
    pub fn size(mut self, size: Pixels) -> Self {
        self.size = size;
        self
    }
}

impl RenderOnce for StatusIcon {
    fn render(self, _window: &mut Window, _cx: &mut App) -> impl IntoElement {
        let status = self.status;
        let (glyph, color) = status.glyph();
        div()
            .debug_selector(move || format!("icon-{}", status.slug()))
            .id(self.id)
            .role(Role::Image)
            .aria_label(status.label())
            .relative()
            .flex_none()
            .size(self.size)
            .child(
                svg()
                    .data(glyph)
                    .absolute()
                    .size_full()
                    .text_color(rgb(color)),
            )
            .when(status == Status::Working, |icon| {
                icon.child(
                    svg()
                        .data(WORKING_ARC)
                        .absolute()
                        .size_full()
                        .text_color(rgb(ICON_YOU))
                        .with_animation(
                            "spin",
                            Animation::new(Duration::from_millis(SPIN_PERIOD_MS)).repeat_synced(),
                            |arc, turn| {
                                arc.with_transformation(Transformation::rotate(percentage(turn)))
                            },
                        ),
                )
            })
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::{
        BLOCKED, DECIDE, DO, DONE, FYI, IDLE, NEEDS_YOU, NOT_STARTED, Status, StatusIcon, WAITING,
        WORKING_ARC, WORKING_RING,
    };
    use crate::theme::ICON_SIZE;
    use gpui::{
        Context, DevicePixels, IntoElement, ParentElement, Render, Styled, SvgSize, TestAppContext,
        Window, div, px, size,
    };
    use std::collections::HashSet;

    const ALL_SVGS: [&[u8]; 11] = [
        NEEDS_YOU,
        DECIDE,
        DO,
        BLOCKED,
        WORKING_RING,
        WORKING_ARC,
        WAITING,
        IDLE,
        DONE,
        FYI,
        NOT_STARTED,
    ];

    /// Shows one icon, so the tests can measure it.
    struct One(Status);

    impl Render for One {
        fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
            div().flex().child(StatusIcon::new("i", self.0))
        }
    }

    #[test]
    fn labels_are_unique_and_match_design() {
        let labels = Status::ALL.map(Status::label);
        assert_eq!(
            labels,
            [
                "needs you",
                "decide",
                "do",
                "blocked",
                "working",
                "waiting",
                "idle",
                "done",
                "fyi",
                "not started",
            ]
        );
        let unique_labels: HashSet<&str> = labels.into_iter().collect();
        assert_eq!(unique_labels.len(), 10);
        let unique_slugs: HashSet<&str> = Status::ALL.map(Status::slug).into_iter().collect();
        assert_eq!(unique_slugs.len(), 10);
    }

    #[gpui::test]
    fn svgs_parse(cx: &mut TestAppContext) {
        let renderer = cx.update(|cx| cx.svg_renderer());
        for bytes in ALL_SVGS {
            assert!(renderer.parse_svg(bytes).is_ok());
        }
        // The check above is only meaningful if bad input is rejected.
        assert!(renderer.parse_svg(b"<svg").is_err());
    }

    #[gpui::test]
    fn needs_you_has_a_cutout(cx: &mut TestAppContext) {
        let renderer = cx.update(|cx| cx.svg_renderer());
        let parsed = renderer.parse_svg(NEEDS_YOU).unwrap();
        let rendered = renderer
            .render_parsed(
                &parsed,
                SvgSize::ExactSize(size(DevicePixels(32), DevicePixels(32))),
            )
            .unwrap();
        let bytes = rendered.as_bytes(0).unwrap();
        let alpha = |x: usize, y: usize| bytes[(y * 32 + x) * 4 + 3];
        assert_eq!(alpha(15, 12), 0); // inside the `!` bar: a hole
        assert_eq!(alpha(5, 16), 255); // circle body
    }

    #[gpui::test]
    fn icon_is_14px(cx: &mut TestAppContext) {
        let (_view, cx) = cx.add_window_view(|_, _| One(Status::Idle));
        cx.run_until_parked();
        let bounds = cx.debug_bounds("icon-idle").unwrap();
        assert_eq!(bounds.size, size(px(ICON_SIZE), px(ICON_SIZE)));
    }

    #[gpui::test]
    fn idle_icon_requests_no_frames(cx: &mut TestAppContext) {
        let (_view, cx) = cx.add_window_view(|_, _| One(Status::Idle));
        cx.run_until_parked();
        assert_eq!(cx.update(|w, cx| w.simulate_next_frame(cx)), 0);
    }

    #[gpui::test]
    fn working_icon_requests_a_frame(cx: &mut TestAppContext) {
        let (_view, cx) = cx.add_window_view(|_, _| One(Status::Working));
        cx.run_until_parked();
        assert_eq!(cx.update(|w, cx| w.simulate_next_frame(cx)), 1);
    }
}
