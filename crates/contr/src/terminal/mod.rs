//! The terminal view: the focused herdr tab, painted from the surface link. The link starts only
//! with `CONTR_SURFACE=1` (D38). Without it the view shows a line and connects to nothing.

pub mod colors;
pub mod grid;
mod paint;

use std::cell::RefCell;
use std::rc::Rc;
use std::sync::Arc;
use std::time::Duration;

use contr_herdr::NOT_RUNNING_LINE;
use contr_herdr::endpoint::link::{
    LinkHandle, LinkState, LinkTarget, LinkUpdate, SurfaceLink, spawn_link,
};
use contr_herdr::endpoint::messages::{CellPx, GridSize};
use contr_herdr::endpoint::surface::Surface;
use gpui::{
    Bounds, Context, FocusHandle, IntoElement, ParentElement, Pixels, Render, Styled, Task, Window,
    canvas, div, font, point, prelude::*, px, rgb,
};
use tracing::debug;

use crate::theme::{
    FONT_FAMILY, TERM_PADDING, TERMINAL_FONT_SIZE, TERMINAL_LINE_HEIGHT, TEXT_2, TEXT_DIM,
};
use grid::{Metrics, cell_px, grid_for};

/// The variable that starts the surface link (D38).
const SURFACE_ENV: &str = "CONTR_SURFACE";
/// The line shown while the link is off.
const OFF_LINE: &str = "terminal off · set CONTR_SURFACE=1";
/// A size change is sent once the size has held for this long.
const RESIZE_WAIT: Duration = Duration::from_millis(150);

/// The terminal view. Its canvas takes the grid from the layout and paints the latest surface.
pub struct TerminalView {
    focus: FocusHandle,
    /// False unless `CONTR_SURFACE=1`. Nothing connects while it is false.
    enabled: bool,
    /// The link and the handle that sends through it. The handle is `None` until the link starts.
    link: Option<(SurfaceLink, LinkHandle)>,
    state: LinkState,
    surface: Option<Arc<Surface>>,
    /// The grid and cell size the last layout asked for.
    grid: Option<(GridSize, CellPx)>,
    /// The size herdr was last told, in the hello or a resize.
    sent: Option<(GridSize, CellPx)>,
    /// Sends `grid` once the size has held for `RESIZE_WAIT`.
    resize_task: Option<Task<()>>,
    /// Shaped text per row, reused while herdr sends the same row. The canvas paints it.
    rows_cache: Rc<RefCell<paint::RowCache>>,
    _drain: Option<Task<()>>,
    /// Every resize the view sent, in order. Tests read it; the link is not needed for that.
    #[cfg(test)]
    sends: Vec<(GridSize, CellPx)>,
}

impl TerminalView {
    /// Starts with no link. `CONTR_SURFACE=1` lets the link start on the first layout. Tests never
    /// start it: it would connect to the local herdr.
    pub fn new(_window: &mut Window, cx: &mut Context<Self>) -> Self {
        Self {
            focus: cx.focus_handle(),
            enabled: !cfg!(test) && std::env::var(SURFACE_ENV).is_ok_and(|v| v == "1"),
            link: None,
            state: LinkState::Connecting,
            surface: None,
            grid: None,
            sent: None,
            resize_task: None,
            rows_cache: Rc::default(),
            _drain: None,
            #[cfg(test)]
            sends: Vec::new(),
        }
    }

    /// The canvas's layout step: the grid for `bounds`. Returns the cell metrics the paint uses,
    /// or `None` when the font gives no metrics.
    fn layout(
        &mut self,
        bounds: Bounds<Pixels>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<Metrics> {
        let m = cell_metrics(window)?;
        let width = f32::from(bounds.size.width);
        let height = f32::from(bounds.size.height);
        if let Some(size) = grid_for(width, height, &m) {
            self.set_grid((size, cell_px(&m, window.scale_factor())), cx);
        }
        Some(m)
    }

    /// Records the grid the view wants. The first link starts at this size. A size that differs from
    /// the one herdr has is sent at once if none was sent yet, or after `RESIZE_WAIT` otherwise.
    fn set_grid(&mut self, want: (GridSize, CellPx), cx: &mut Context<Self>) {
        if self.grid == Some(want) {
            return;
        }
        self.grid = Some(want);
        if self.link.is_none() && self.enabled {
            self.start_link(want, cx);
        }
        if self.sent == Some(want) {
            // Back at the size herdr has: a pending change is no longer needed.
            self.resize_task = None;
            return;
        }
        if self.sent.is_none() {
            self.send_resize(want);
            return;
        }
        self.resize_task = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(RESIZE_WAIT).await;
            let _ = this.update(cx, |view, _| view.flush_resize());
        }));
    }

    /// Sends the wanted size if herdr does not have it yet. Called when the wait is over.
    fn flush_resize(&mut self) {
        if let Some(want) = self.grid
            && self.sent != Some(want)
        {
            self.send_resize(want);
        }
    }

    /// Records `size` as sent and queues it on the link. Without a live link the size is kept for
    /// the next hello, and the `Live` state sends it again.
    fn send_resize(&mut self, (size, cell): (GridSize, CellPx)) {
        self.sent = Some((size, cell));
        #[cfg(test)]
        self.sends.push((size, cell));
        if let Some((_, handle)) = &self.link
            && let Err(e) = handle.resize(size, cell)
        {
            debug!("terminal resize not queued: {e:?}");
        }
    }

    /// Starts the link at `(size, cell)`. The hello carries that size, so it counts as sent.
    fn start_link(&mut self, (size, cell): (GridSize, CellPx), cx: &mut Context<Self>) {
        let (tx, rx) = async_channel::unbounded();
        match spawn_link(LinkTarget::Local, size, cell, tx) {
            Ok(link) => {
                self.sent = Some((size, cell));
                self._drain = Some(cx.spawn(async move |this, cx| {
                    while let Ok(update) = rx.recv().await {
                        if this
                            .update(cx, |view, cx| view.on_update(update, cx))
                            .is_err()
                        {
                            break; // the view is gone
                        }
                    }
                }));
                self.link = Some(link);
            }
            Err(e) => self.set_state(LinkState::NotRunning(e.to_string()), cx),
        }
    }

    /// Applies one update from the link.
    fn on_update(&mut self, update: LinkUpdate, cx: &mut Context<Self>) {
        match update {
            LinkUpdate::State(state) => self.set_state(state, cx),
            LinkUpdate::Surface(_) => {
                // The notice says a surface is waiting. The newest one is taken from the link.
                let next = self
                    .link
                    .as_ref()
                    .and_then(|(_, handle)| handle.take_latest());
                if let Some(next) = next {
                    self.accept_surface(next, cx);
                }
            }
            // The snapshot names the focused pane. Input, which needs it, comes in a later slice.
            LinkUpdate::Snapshot(_) => {}
        }
    }

    /// Shows `next` unless it is the surface already shown. A new revision notifies once.
    fn accept_surface(&mut self, next: Arc<Surface>, cx: &mut Context<Self>) {
        let same = self.surface.as_ref().is_some_and(|held| {
            held.boot_id == next.boot_id
                && held.projection_revision == next.projection_revision
                && held.revision == next.revision
                && held.size == next.size
        });
        if same {
            return;
        }
        self.surface = Some(next);
        cx.notify();
    }

    /// Sets the link state. A state that changes notifies once. A lost link drops the surface, and a
    /// live one gets the wanted size again, since the hello may have carried an older one.
    fn set_state(&mut self, state: LinkState, cx: &mut Context<Self>) {
        if self.state == state {
            return;
        }
        self.state = state;
        if matches!(
            self.state,
            LinkState::NotRunning(_) | LinkState::Incompatible(_)
        ) {
            self.surface = None;
            self.rows_cache.borrow_mut().clear();
        }
        if matches!(self.state, LinkState::Live { .. })
            && let Some(want) = self.grid
        {
            self.send_resize(want);
        }
        cx.notify();
    }
}

/// The line the view shows instead of the grid, with its colour. `None` when the grid speaks for
/// itself. With the link off, the line says so; with the link on, it says what the link reports.
fn status_line(enabled: bool, has_surface: bool, state: &LinkState) -> Option<(String, u32)> {
    if has_surface {
        return None;
    }
    if !enabled {
        return Some((OFF_LINE.to_owned(), TEXT_DIM));
    }
    match state {
        LinkState::Connecting | LinkState::Live { .. } => None,
        LinkState::NotRunning(_) => Some((NOT_RUNNING_LINE.to_owned(), TEXT_2)),
        LinkState::Incompatible(message) => Some((message.clone(), TEXT_2)),
    }
}

/// The cell metrics of the terminal font: the advance of `m` at the terminal size, and the
/// terminal line height. `None` when the font has no advance for `m`.
fn cell_metrics(window: &Window) -> Option<Metrics> {
    let text = window.text_system();
    let font_id = text.resolve_font(&font(FONT_FAMILY));
    let advance = text.advance(font_id, px(TERMINAL_FONT_SIZE), 'm').ok()?;
    Some(Metrics {
        cell_w: f32::from(advance.width),
        cell_h: TERMINAL_LINE_HEIGHT,
        pad: TERM_PADDING,
    })
}

impl Render for TerminalView {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let view = cx.entity();
        let surface = self.surface.clone();
        let rows = Rc::clone(&self.rows_cache);
        let focus = self.focus.clone();
        let grid = div()
            .debug_selector(|| "terminal-grid".into())
            .absolute()
            .size_full()
            .track_focus(&self.focus)
            .child(
                canvas(
                    move |bounds, window, cx| {
                        view.update(cx, |this, cx| this.layout(bounds, window, cx))
                    },
                    move |bounds, metrics, window, cx| {
                        let (Some(m), Some(surface)) = (metrics, surface.as_deref()) else {
                            return;
                        };
                        let origin = bounds.origin + point(px(m.pad), px(m.pad));
                        let focused = focus.is_focused(window) && window.is_window_active();
                        paint::paint_surface(
                            window,
                            cx,
                            origin,
                            m,
                            surface,
                            focused,
                            &mut rows.borrow_mut(),
                        );
                    },
                )
                .size_full(),
            );
        let root = div()
            .size_full()
            .relative()
            .flex()
            .items_center()
            .justify_center()
            .child(grid);
        match status_line(self.enabled, self.surface.is_some(), &self.state) {
            Some((line, colour)) => root.child(
                div()
                    .debug_selector(|| "surface-state".into())
                    .text_color(rgb(colour))
                    .child(line),
            ),
            None => root,
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use std::cell::Cell as Counter;
    use std::time::Duration;

    use contr_herdr::endpoint::surface::{Cell, Cursor};
    use gpui::{Entity, Subscription, TestAppContext, VisualTestContext, size};

    use super::*;

    const NOT_RUNNING: &str = "gone";
    /// Longer than `RESIZE_WAIT`, so a pending size change is sent.
    const SETTLED: Duration = Duration::from_millis(200);

    fn surface(revision: u64, first: &str) -> Arc<Surface> {
        let cell = |text: &str| Cell {
            text: text.to_owned(),
            fg: 0,
            bg: 0,
            attrs: 0,
            wide_tail: false,
        };
        Arc::new(Surface {
            boot_id: "boot".to_owned(),
            projection_revision: 1,
            revision,
            size: GridSize { cols: 3, rows: 2 },
            rows: vec![
                Arc::from(vec![cell(first), cell("b"), cell("c")]),
                Arc::from(vec![cell("d"), cell("e"), cell("f")]),
            ],
            cursor: Some(Cursor {
                x: 1,
                y: 0,
                visible: true,
                shape: 0,
            }),
            panes: Vec::new(),
        })
    }

    /// Counts the notifications the view sends, from now on. The test keeps the subscription.
    fn count_notices(
        cx: &mut VisualTestContext,
        view: &Entity<TerminalView>,
    ) -> (Rc<Counter<u32>>, Subscription) {
        let notices = Rc::new(Counter::new(0));
        let counted = Rc::clone(&notices);
        let subscription =
            cx.update(|_, app| app.observe(view, move |_, _| counted.set(counted.get() + 1)));
        (notices, subscription)
    }

    #[test]
    fn the_status_line_follows_the_link_and_the_surface() {
        let not_running = LinkState::NotRunning(NOT_RUNNING.to_owned());
        // The surface speaks for itself, whatever the link says.
        assert_eq!(status_line(true, true, &not_running), None);
        assert_eq!(status_line(false, true, &not_running), None);
        // With the link off, the line says how to turn it on.
        assert_eq!(
            status_line(false, false, &not_running),
            Some((OFF_LINE.to_owned(), TEXT_DIM))
        );
        assert_eq!(status_line(true, false, &LinkState::Connecting), None);
        assert_eq!(
            status_line(
                true,
                false,
                &LinkState::Live {
                    server_version: "0.9.3".to_owned(),
                    untested: false,
                }
            ),
            None
        );
        assert_eq!(
            status_line(true, false, &not_running),
            Some((NOT_RUNNING_LINE.to_owned(), TEXT_2))
        );
        assert_eq!(
            status_line(
                true,
                false,
                &LinkState::Incompatible("update contr".to_owned())
            ),
            Some(("update contr".to_owned(), TEXT_2))
        );
    }

    #[gpui::test]
    fn the_grid_fills_the_view_and_the_surface_replaces_the_line(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        cx.simulate_resize(size(px(800.), px(600.)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("surface-state").is_some());

        view.update(cx, |view, cx| view.accept_surface(surface(1, "a"), cx));
        cx.run_until_parked();
        let grid = cx.debug_bounds("terminal-grid").unwrap();
        assert_eq!(grid.size, size(px(800.), px(600.)));
        assert!(cx.debug_bounds("surface-state").is_none());
    }

    #[gpui::test]
    fn the_line_shows_while_the_link_is_off(cx: &mut TestAppContext) {
        let (_view, cx) = cx.add_window_view(TerminalView::new);
        cx.simulate_resize(size(px(800.), px(600.)));
        cx.run_until_parked();
        assert!(cx.debug_bounds("surface-state").is_some());
        assert!(cx.debug_bounds("terminal-grid").is_some());
    }

    #[gpui::test]
    fn a_surface_notifies_once_per_revision(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        let (notices, _observer) = count_notices(cx, &view);
        let shown = surface(1, "a");
        view.update(cx, |view, cx| view.accept_surface(Arc::clone(&shown), cx));
        cx.run_until_parked();
        assert_eq!(notices.get(), 1);

        view.update(cx, |view, cx| view.accept_surface(shown, cx));
        cx.run_until_parked();
        assert_eq!(notices.get(), 1, "the same surface is not shown again");

        view.update(cx, |view, cx| view.accept_surface(surface(2, "x"), cx));
        cx.run_until_parked();
        assert_eq!(notices.get(), 2);
    }

    #[gpui::test]
    fn a_state_change_notifies_once_and_a_lost_link_drops_the_surface(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        view.update(cx, |view, cx| view.accept_surface(surface(1, "a"), cx));
        cx.run_until_parked();
        let (notices, _observer) = count_notices(cx, &view);

        // Connecting is the state it already has: no notice.
        view.update(cx, |view, cx| {
            view.on_update(LinkUpdate::State(LinkState::Connecting), cx)
        });
        cx.run_until_parked();
        assert_eq!(notices.get(), 0);

        let lost = LinkState::NotRunning(NOT_RUNNING.to_owned());
        view.update(cx, |view, cx| {
            view.on_update(LinkUpdate::State(lost.clone()), cx)
        });
        cx.run_until_parked();
        assert_eq!(notices.get(), 1);
        view.read_with(cx, |view, _| assert!(view.surface.is_none()));
        assert!(cx.debug_bounds("surface-state").is_some());

        view.update(cx, |view, cx| view.on_update(LinkUpdate::State(lost), cx));
        cx.run_until_parked();
        assert_eq!(notices.get(), 1);
    }

    /// Sizes the window to `width` × `height` and lets the view settle: the layout runs, and a size
    /// change has waited out `RESIZE_WAIT`. Afterwards the sends hold the sizes herdr has.
    fn settle(cx: &mut VisualTestContext, width: f32, height: f32) {
        cx.simulate_resize(size(px(width), px(height)));
        cx.run_until_parked();
        cx.executor().advance_clock(SETTLED);
        cx.run_until_parked();
    }

    #[gpui::test]
    fn two_size_changes_within_the_wait_send_one_resize(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let before = view.read_with(cx, |view, _| view.sends.clone());

        cx.simulate_resize(size(px(900.), px(600.)));
        cx.run_until_parked();
        cx.simulate_resize(size(px(1000.), px(600.)));
        cx.run_until_parked();
        cx.executor().advance_clock(Duration::from_millis(100));
        cx.run_until_parked();
        assert_eq!(
            view.read_with(cx, |view, _| view.sends.len()),
            before.len(),
            "still inside the wait"
        );

        cx.executor().advance_clock(Duration::from_millis(100));
        cx.run_until_parked();
        let sends = view.read_with(cx, |view, _| view.sends.clone());
        assert_eq!(sends.len(), before.len() + 1);
        // The one send is the last size, 1000 px wide, not the 900 px in between.
        let (last, held) = (sends[sends.len() - 1].0, before[before.len() - 1].0);
        assert!(last.cols > held.cols);
    }

    #[gpui::test]
    fn a_size_that_returns_within_the_wait_sends_nothing_more(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let before = view.read_with(cx, |view, _| view.sends.len());

        cx.simulate_resize(size(px(900.), px(600.)));
        cx.run_until_parked();
        cx.simulate_resize(size(px(800.), px(600.)));
        cx.run_until_parked();
        cx.executor().advance_clock(Duration::from_millis(500));
        cx.run_until_parked();
        assert_eq!(view.read_with(cx, |view, _| view.sends.len()), before);
    }

    #[gpui::test]
    fn a_live_link_gets_the_wanted_size_again(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let before = view.read_with(cx, |view, _| view.sends.clone());
        let live = LinkState::Live {
            server_version: "0.9.3".to_owned(),
            untested: false,
        };
        view.update(cx, |view, cx| view.on_update(LinkUpdate::State(live), cx));
        cx.run_until_parked();
        let sends = view.read_with(cx, |view, _| view.sends.clone());
        assert_eq!(sends.len(), before.len() + 1);
        assert_eq!(sends[sends.len() - 1], before[before.len() - 1]);
    }

    #[gpui::test]
    fn a_view_too_small_for_a_grid_sends_no_size(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let before = view.read_with(cx, |view, _| view.sends.len());

        settle(cx, 20., 20.);
        assert_eq!(view.read_with(cx, |view, _| view.sends.len()), before);
    }
}
