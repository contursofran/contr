//! The terminal view: the focused herdr tab, painted from the surface link and typed into through
//! it. The link starts only with `CONTR_SURFACE=1` (D38). Without it the view shows a line and
//! connects to nothing.

mod boxdraw;
pub mod colors;
pub mod grid;
mod keys;
mod paint;

use std::cell::RefCell;
use std::io;
use std::ops::Range;
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use async_channel::Sender;
use contr_herdr::NOT_RUNNING_LINE;
use contr_herdr::endpoint::link::{
    LinkHandle, LinkState, LinkTarget, LinkUpdate, SurfaceLink, spawn_link,
};
use contr_herdr::endpoint::messages::{CellPx, GridSize, InputEvent};
use contr_herdr::endpoint::surface::Surface;
use gpui::{
    App, Bounds, ClipboardItem, Context, ElementInputHandler, EntityInputHandler, FocusHandle,
    InputHandler, IntoElement, KeyDownEvent, Modifiers, MouseButton, MouseDownEvent, ParentElement,
    Pixels, Point, Render, Styled, Subscription, Task, TextInputConfiguration, UTF16Selection,
    Window, canvas, div, point, prelude::*, px, rgb, size,
};
use tracing::debug;

use crate::theme::{TERM_PADDING, TERMINAL_FONT_SIZE, TERMINAL_LINE_HEIGHT, TEXT_2, TEXT_DIM};
use grid::{Metrics, cell_px, grid_for};

/// The variable that starts the surface link (D38).
const SURFACE_ENV: &str = "CONTR_SURFACE";
/// The line shown while the link is off.
const OFF_LINE: &str = "terminal off · set CONTR_SURFACE=1";
/// A size change is sent once the size has held for this long.
const RESIZE_WAIT: Duration = Duration::from_millis(150);
/// After the owner types, a size herdr does not show is sent again at most this often (D12).
const REASSERT_WAIT: Duration = Duration::from_secs(1);
/// The grid's opacity while the link reconnects and the surface on screen may be out of date.
const STALE_OPACITY: f32 = 0.5;

/// Starts a link. The app uses `spawn_link`; the tests use fakes, so no test reaches a socket.
type Starter =
    fn(LinkTarget, GridSize, CellPx, Sender<LinkUpdate>) -> io::Result<(SurfaceLink, LinkHandle)>;

/// The terminal view. Its canvas takes the grid from the layout and paints the latest surface.
pub struct TerminalView {
    focus: FocusHandle,
    /// False unless `CONTR_SURFACE=1`. Nothing connects while it is false.
    enabled: bool,
    /// Starts the link.
    start: Starter,
    /// The link and the handle that sends through it. The handle is `None` until the link starts.
    link: Option<(SurfaceLink, LinkHandle)>,
    state: LinkState,
    surface: Option<Arc<Surface>>,
    /// The pane the shell reported as focused. Keys go there when the surface names none.
    shell_pane: Option<String>,
    /// Whether the window is active. herdr gets this as the focus state.
    window_active: bool,
    /// The grid and cell size the last layout asked for.
    grid: Option<(GridSize, CellPx)>,
    /// The metrics of the last layout. The IME places its window with them.
    metrics: Option<Metrics>,
    /// The size herdr was last told, in the hello or a resize.
    sent: Option<(GridSize, CellPx)>,
    /// When the size was last sent again after the owner typed (D12).
    reasserted_at: Option<Instant>,
    /// Sends `grid` once the size has held for `RESIZE_WAIT`.
    resize_task: Option<Task<()>>,
    /// The text the IME is composing. It is painted at the cursor, and sent when it is committed.
    marked: Option<String>,
    /// Shaped text per row, reused while herdr sends the same row. The canvas paints it.
    rows_cache: Rc<RefCell<paint::RowCache>>,
    _drain: Option<Task<()>>,
    /// Tells herdr when the window is activated or deactivated.
    _activation: Subscription,
    /// Every resize the view sent, in order. Tests read it; the link is not needed for that.
    #[cfg(test)]
    sends: Vec<(GridSize, CellPx)>,
    /// How often `render` ran. Tests read it to see whether a parent's redraw renders the view.
    #[cfg(test)]
    pub(crate) renders: u32,
}

impl TerminalView {
    /// Starts with no link and takes the keyboard. `CONTR_SURFACE=1` lets the link start on the
    /// first layout. Tests never start it: it would connect to the local herdr.
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let focus = cx.focus_handle();
        window.focus(&focus, cx);
        let _activation = cx.observe_window_activation(window, |this, window, cx| {
            this.set_window_active(window.is_window_active(), cx);
        });
        Self {
            focus,
            enabled: !cfg!(test) && std::env::var(SURFACE_ENV).is_ok_and(|v| v == "1"),
            start: spawn_link,
            link: None,
            state: LinkState::Connecting,
            surface: None,
            shell_pane: None,
            window_active: window.is_window_active(),
            grid: None,
            metrics: None,
            sent: None,
            reasserted_at: None,
            resize_task: None,
            marked: None,
            rows_cache: Rc::default(),
            _drain: None,
            _activation,
            #[cfg(test)]
            sends: Vec::new(),
            #[cfg(test)]
            renders: 0,
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
        self.metrics = Some(m);
        let width = f32::from(bounds.size.width);
        let height = f32::from(bounds.size.height);
        if let Some(size) = grid_for(width, height, &m) {
            self.set_grid((size, cell_px(&m, window.scale_factor())), cx);
        }
        Some(m)
    }

    /// Records the grid the view wants. A size that differs from the one herdr has is sent at once
    /// if none was sent yet, or after `RESIZE_WAIT` otherwise.
    fn set_grid(&mut self, want: (GridSize, CellPx), cx: &mut Context<Self>) {
        if self.grid == Some(want) {
            return;
        }
        self.grid = Some(want);
        self.ensure_link(cx);
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

    /// Starts the link at the wanted grid if it is not running. This runs on a grid change and on
    /// each click or key, so a failed start is tried again without a timer: layout runs every frame.
    fn ensure_link(&mut self, cx: &mut Context<Self>) {
        if self.link.is_some() || !self.enabled {
            return;
        }
        if let Some(want) = self.grid {
            self.start_link(want, cx);
        }
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
        match (self.start)(LinkTarget::Local, size, cell, tx) {
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
            LinkUpdate::Snapshot(snapshot) => self.shell_pane = snapshot.focused_pane_id.clone(),
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

    /// Sets the link state. A state that changes notifies once. A lost link drops the surface, and
    /// a link that is not running also forgets the shell's pane. A live one gets the focus state
    /// and the wanted size again: the hello may have carried an older size, and herdr may not know
    /// the window is active.
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
        if matches!(self.state, LinkState::NotRunning(_)) {
            self.shell_pane = None;
        }
        if matches!(self.state, LinkState::Live { .. }) {
            self.send_focus();
            if let Some(want) = self.grid {
                self.send_resize(want);
            }
        }
        cx.notify();
    }

    /// The window was activated or deactivated: herdr gets the new focus state, and the cursor is
    /// painted again.
    fn set_window_active(&mut self, active: bool, cx: &mut Context<Self>) {
        if self.window_active == active {
            return;
        }
        self.window_active = active;
        self.send_focus();
        cx.notify();
    }

    /// Queues the window's focus state on the link.
    fn send_focus(&self) {
        if let Some((_, handle)) = &self.link
            && let Err(e) = handle.set_focus(self.window_active)
        {
            debug!("terminal focus not queued: {e:?}");
        }
    }

    /// The keyboard focus of the view, for the dev control's `terminal` click.
    #[cfg(debug_assertions)]
    pub(crate) fn focus_handle(&self) -> &FocusHandle {
        &self.focus
    }

    /// A click takes the keyboard, and tries a failed link start again.
    fn click(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        window.focus(&self.focus, cx);
        self.ensure_link(cx);
    }

    /// A key the terminal takes is sent to the focused pane, and the event stops there. Cmd-V alone
    /// pastes the clipboard's text. Other keys reach the app.
    fn key_down(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.ensure_link(cx);
        let keystroke = &event.keystroke;
        let taken = if keystroke.key == "v" && keystroke.modifiers == Modifiers::command() {
            if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
                self.send(&[InputEvent::Paste(text)], Instant::now());
            }
            true
        } else if let Some(ev) = keys::key_event(keystroke) {
            self.send(&[ev], Instant::now());
            true
        } else {
            false
        };
        if taken {
            cx.stop_propagation();
            window.prevent_default();
        }
    }

    /// Sends `events` to the focused pane. Nothing is sent without a link or a pane. After the
    /// owner types, the size may be sent again (D12).
    fn send(&mut self, events: &[InputEvent], now: Instant) {
        let Some(pane) = self.focused_pane() else {
            debug!("terminal input not sent: no focused pane");
            return;
        };
        let Some((_, handle)) = &self.link else {
            return;
        };
        match handle.send_input(&pane, events) {
            // D12: the size is re-asserted only when the input was queued.
            Ok(()) => self.reassert_size(now),
            Err(e) => debug!("terminal input not queued: {e:?}"),
        }
    }

    /// Sends committed IME text as text. An empty commit sends nothing.
    fn commit(&mut self, text: &str, cx: &mut Context<Self>) {
        self.ensure_link(cx);
        if !text.is_empty() {
            self.send(&[InputEvent::Text(text.to_owned())], Instant::now());
        }
    }

    /// The pane keys go to: the surface's focused pane (D13), else the pane the shell reported.
    fn focused_pane(&self) -> Option<String> {
        self.surface
            .as_ref()
            .and_then(|surface| surface.focused_pane())
            .map(|pane| pane.pane_id.clone())
            .or_else(|| self.shell_pane.clone())
    }

    /// D12: when herdr shows a size other than the one contr sent, the sent size is sent again. It
    /// runs after the owner types, and at most once a second.
    fn reassert_size(&mut self, now: Instant) {
        let Some(sent) = self.sent else {
            return;
        };
        let shown_differently = self.surface.as_ref().is_some_and(|s| s.size != sent.0);
        if shown_differently && reassert_due(self.reasserted_at, now) {
            self.reasserted_at = Some(now);
            self.send_resize(sent);
        }
    }

    /// Sets the composing text. A change notifies, so the view paints it again.
    fn set_marked(&mut self, text: Option<String>, cx: &mut Context<Self>) {
        if self.marked != text {
            self.marked = text;
            cx.notify();
        }
    }

    /// The cursor cell in window coordinates, for the IME's window. `bounds` is the grid's bounds.
    fn cursor_bounds(&self, bounds: Bounds<Pixels>) -> Option<Bounds<Pixels>> {
        let m = self.metrics?;
        let surface = self.surface.as_ref()?;
        let (x, y) = paint::cursor_cell(surface.cursor?, surface.size);
        let at = bounds.origin
            + point(
                px(m.pad + f32::from(x) * m.cell_w),
                px(m.pad + f32::from(y) * m.cell_h),
            );
        Some(Bounds::new(at, size(px(m.cell_w), px(m.cell_h))))
    }
}

impl EntityInputHandler for TerminalView {
    fn text_for_range(
        &mut self,
        _range: Range<usize>,
        _adjusted: &mut Option<Range<usize>>,
        _window: &mut Window,
        _cx: &mut Context<Self>,
    ) -> Option<String> {
        None
    }

    fn selected_text_range(
        &mut self,
        _ignore_disabled_input: bool,
        _window: &mut Window,
        _cx: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        Some(UTF16Selection {
            range: 0..0,
            reversed: false,
        })
    }

    fn marked_text_range(
        &self,
        _window: &mut Window,
        _cx: &mut Context<Self>,
    ) -> Option<Range<usize>> {
        self.marked
            .as_ref()
            .map(|text| 0..text.encode_utf16().count())
    }

    fn unmark_text(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        // The system ends a composition by committing it, for example when focus moves away. The
        // composed text is what it commits, so it is sent as a commit is.
        if let Some(text) = self.marked.take() {
            cx.notify();
            self.commit(&text, cx);
        }
    }

    fn replace_text_in_range(
        &mut self,
        _range: Option<Range<usize>>,
        text: &str,
        _window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        // A commit ends the composition. Text is sent as text, so the shell gets what the IME made.
        self.set_marked(None, cx);
        self.commit(text, cx);
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        _range: Option<Range<usize>>,
        text: &str,
        _selected: Option<Range<usize>>,
        _window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.set_marked((!text.is_empty()).then(|| text.to_owned()), cx);
    }

    fn bounds_for_range(
        &mut self,
        _range: Range<usize>,
        element_bounds: Bounds<Pixels>,
        _window: &mut Window,
        _cx: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        self.cursor_bounds(element_bounds)
    }

    fn character_index_for_point(
        &mut self,
        _point: Point<Pixels>,
        _window: &mut Window,
        _cx: &mut Context<Self>,
    ) -> Option<usize> {
        None
    }
}

/// The input handler the view registers while it has focus. It is `ElementInputHandler`, with one
/// change: `apple_press_and_hold_enabled` is false, so macOS does not show the accent menu for a
/// held key and the key repeats, as it does in a terminal.
struct TerminalInput(ElementInputHandler<TerminalView>);

impl InputHandler for TerminalInput {
    fn selected_text_range(
        &mut self,
        ignore_disabled_input: bool,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<UTF16Selection> {
        self.0
            .selected_text_range(ignore_disabled_input, window, cx)
    }

    fn marked_text_range(&mut self, window: &mut Window, cx: &mut App) -> Option<Range<usize>> {
        self.0.marked_text_range(window, cx)
    }

    fn text_for_range(
        &mut self,
        range_utf16: Range<usize>,
        adjusted_range: &mut Option<Range<usize>>,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<String> {
        self.0
            .text_for_range(range_utf16, adjusted_range, window, cx)
    }

    fn replace_text_in_range(
        &mut self,
        replacement_range: Option<Range<usize>>,
        text: &str,
        window: &mut Window,
        cx: &mut App,
    ) {
        self.0
            .replace_text_in_range(replacement_range, text, window, cx);
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        range_utf16: Option<Range<usize>>,
        new_text: &str,
        new_selected_range: Option<Range<usize>>,
        window: &mut Window,
        cx: &mut App,
    ) {
        self.0.replace_and_mark_text_in_range(
            range_utf16,
            new_text,
            new_selected_range,
            window,
            cx,
        );
    }

    fn unmark_text(&mut self, window: &mut Window, cx: &mut App) {
        self.0.unmark_text(window, cx);
    }

    fn paste(&mut self, item: ClipboardItem, window: &mut Window, cx: &mut App) {
        self.0.paste(item, window, cx);
    }

    fn bounds_for_range(
        &mut self,
        range_utf16: Range<usize>,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<Bounds<Pixels>> {
        self.0.bounds_for_range(range_utf16, window, cx)
    }

    fn character_index_for_point(
        &mut self,
        point: Point<Pixels>,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<usize> {
        self.0.character_index_for_point(point, window, cx)
    }

    fn set_selected_text_range(
        &mut self,
        range_utf16: Range<usize>,
        window: &mut Window,
        cx: &mut App,
    ) {
        self.0.set_selected_text_range(range_utf16, window, cx);
    }

    fn element_bounds(&mut self, window: &mut Window, cx: &mut App) -> Option<Bounds<Pixels>> {
        self.0.element_bounds(window, cx)
    }

    fn text_length_utf16(&mut self, window: &mut Window, cx: &mut App) -> Option<usize> {
        self.0.text_length_utf16(window, cx)
    }

    fn apple_press_and_hold_enabled(&mut self) -> bool {
        false
    }

    fn accepts_text_input(&mut self, window: &mut Window, cx: &mut App) -> bool {
        self.0.accepts_text_input(window, cx)
    }

    fn text_input_editable_range(
        &mut self,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<Range<usize>> {
        self.0.text_input_editable_range(window, cx)
    }

    fn prefers_ime_for_printable_keys(&mut self, window: &mut Window, cx: &mut App) -> bool {
        self.0.prefers_ime_for_printable_keys(window, cx)
    }

    fn text_input_configuration(
        &mut self,
        window: &mut Window,
        cx: &mut App,
    ) -> TextInputConfiguration {
        self.0.text_input_configuration(window, cx)
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

/// True while the view shows a surface that the link no longer confirms: the link is reconnecting.
fn is_stale(has_surface: bool, state: &LinkState) -> bool {
    has_surface && *state == LinkState::Connecting
}

/// Whether a size may be sent again now: none was sent before, or the last one was `REASSERT_WAIT`
/// or more ago.
fn reassert_due(last: Option<Instant>, now: Instant) -> bool {
    last.is_none_or(|at| now.saturating_duration_since(at) >= REASSERT_WAIT)
}

/// The cell metrics of the terminal font: the advance of `m` at the terminal size, and the
/// terminal line height. `None` when the font has no advance for `m`.
fn cell_metrics(window: &Window) -> Option<Metrics> {
    let text = window.text_system();
    let font_id = text.resolve_font(&paint::terminal_font());
    let advance = text.advance(font_id, px(TERMINAL_FONT_SIZE), 'm').ok()?;
    Some(Metrics {
        cell_w: f32::from(advance.width),
        cell_h: TERMINAL_LINE_HEIGHT,
        pad: TERM_PADDING,
    })
}

impl Render for TerminalView {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        #[cfg(test)]
        {
            self.renders += 1;
        }
        let view = cx.entity();
        let input_view = view.clone();
        let surface = self.surface.clone();
        let marked = self.marked.clone();
        let rows = Rc::clone(&self.rows_cache);
        let focus = self.focus.clone();
        let stale = is_stale(self.surface.is_some(), &self.state);
        let grid = div()
            .debug_selector(|| "terminal-grid".into())
            .absolute()
            .size_full()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(|this, event: &KeyDownEvent, window, cx| {
                this.key_down(event, window, cx);
            }))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, _: &MouseDownEvent, window, cx| this.click(window, cx)),
            )
            .when(stale, |grid| grid.opacity(STALE_OPACITY))
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
                        let input =
                            TerminalInput(ElementInputHandler::new(bounds, input_view.clone()));
                        window.handle_input(&focus, input, cx);
                        let typing = paint::Typing {
                            focused,
                            marked: marked.as_deref(),
                        };
                        paint::paint_surface(
                            window,
                            cx,
                            origin,
                            m,
                            surface,
                            typing,
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

    use async_channel::Receiver;
    use contr_herdr::endpoint::link::test_link;
    use contr_herdr::endpoint::messages::{ClientMsg, KeyCode, ShellSnapshot, mods};
    use contr_herdr::endpoint::surface::{Cell, Cursor, Rect, SurfacePane};
    use gpui::{
        ClipboardItem, Entity, Modifiers, Subscription, TestAppContext, VisualTestContext, size,
    };

    use super::*;

    const NOT_RUNNING: &str = "gone";
    /// The pane a surface from `shown` names.
    const PANE: &str = "p1";
    /// Longer than `RESIZE_WAIT`, so a pending size change is sent.
    const SETTLED: Duration = Duration::from_millis(200);
    /// Where a click lands: inside the grid of the 800 × 600 window.
    const CENTRE: (f32, f32) = (400., 300.);

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

    /// A surface of `size` with no rows, whose focused pane is `pane` when it is given.
    fn shown(revision: u64, size: GridSize, pane: Option<&str>) -> Arc<Surface> {
        Arc::new(Surface {
            boot_id: "boot".to_owned(),
            projection_revision: 1,
            revision,
            size,
            rows: Vec::new(),
            cursor: None,
            panes: pane
                .map(|id| SurfacePane {
                    pane_id: id.to_owned(),
                    rect: Rect::default(),
                    inner_rect: Rect::default(),
                    focused: true,
                    alternate_screen: false,
                })
                .into_iter()
                .collect(),
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

    /// Sizes the window to `width` × `height` and lets the view settle: the layout runs, and a size
    /// change has waited out `RESIZE_WAIT`. Afterwards the sends hold the sizes herdr has.
    fn settle(cx: &mut VisualTestContext, width: f32, height: f32) {
        cx.simulate_resize(size(px(width), px(height)));
        cx.run_until_parked();
        cx.executor().advance_clock(SETTLED);
        cx.run_until_parked();
    }

    /// The view has a link whose frames the test reads, and a surface of the size herdr has, with
    /// the focused pane `PANE`. Nothing is queued yet when it returns.
    fn attach(cx: &mut VisualTestContext, view: &Entity<TerminalView>) -> Receiver<Vec<u8>> {
        let (link, handle, frames) = test_link();
        let size = view.read_with(cx, |view, _| {
            view.sent
                .map_or(GridSize { cols: 3, rows: 2 }, |(size, _)| size)
        });
        view.update(cx, |view, cx| {
            view.link = Some((link, handle));
            view.accept_surface(shown(1, size, Some(PANE)), cx);
        });
        cx.run_until_parked();
        assert!(queued(&frames).is_empty());
        frames
    }

    /// Every frame queued on the test link so far, in order.
    fn queued(frames: &Receiver<Vec<u8>>) -> Vec<Vec<u8>> {
        std::iter::from_fn(|| frames.try_recv().ok()).collect()
    }

    /// The frame that sends `events` to `pane`.
    fn input(pane: &str, events: &[InputEvent]) -> Vec<u8> {
        ClientMsg::Input {
            pane_id: pane,
            events,
        }
        .encode()
    }

    fn key(code: KeyCode, flags: u8) -> InputEvent {
        InputEvent::Key {
            code,
            mods: flags,
            repeat: false,
        }
    }

    fn text(s: &str) -> InputEvent {
        InputEvent::Text(s.to_owned())
    }

    /// The size and cell the view last sent. Tests use it to name the resize herdr should get.
    fn sent_size(cx: &mut VisualTestContext, view: &Entity<TerminalView>) -> (GridSize, CellPx) {
        view.read_with(cx, |view, _| view.sent.unwrap())
    }

    /// A start that fails the way a thread that cannot be made does.
    fn failing_start(
        _: LinkTarget,
        _: GridSize,
        _: CellPx,
        _: Sender<LinkUpdate>,
    ) -> io::Result<(SurfaceLink, LinkHandle)> {
        Err(io::Error::other("no thread"))
    }

    thread_local! {
        /// How many times `counting_failing_start` ran. Only one test uses that starter.
        static STARTS: Counter<u32> = const { Counter::new(0) };
    }

    /// `failing_start`, which also counts its calls, so a test can see how often a start ran.
    fn counting_failing_start(
        _: LinkTarget,
        _: GridSize,
        _: CellPx,
        _: Sender<LinkUpdate>,
    ) -> io::Result<(SurfaceLink, LinkHandle)> {
        STARTS.with(|n| n.set(n.get() + 1));
        Err(io::Error::other("no thread"))
    }

    /// A start that gives a link with no socket behind it.
    fn working_start(
        _: LinkTarget,
        _: GridSize,
        _: CellPx,
        _: Sender<LinkUpdate>,
    ) -> io::Result<(SurfaceLink, LinkHandle)> {
        let (link, handle, _frames) = test_link();
        Ok((link, handle))
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

    #[test]
    fn a_surface_is_stale_only_while_the_link_reconnects() {
        assert!(is_stale(true, &LinkState::Connecting));
        assert!(!is_stale(false, &LinkState::Connecting));
        let live = LinkState::Live {
            server_version: "0.9.3".to_owned(),
            untested: false,
        };
        assert!(!is_stale(true, &live));
        assert!(!is_stale(
            true,
            &LinkState::NotRunning(NOT_RUNNING.to_owned())
        ));
    }

    #[test]
    fn a_size_is_sent_again_only_a_second_after_the_last_one() {
        let t0 = Instant::now();
        assert!(reassert_due(None, t0));
        assert!(!reassert_due(Some(t0), t0 + Duration::from_millis(999)));
        assert!(reassert_due(Some(t0), t0 + Duration::from_secs(1)));
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
    fn a_surface_with_box_glyphs_renders(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        cx.simulate_resize(size(px(800.), px(600.)));
        // The first cell is a box glyph, so the quad pass runs for it on the frame.
        view.update(cx, |view, cx| view.accept_surface(surface(1, "│"), cx));
        cx.run_until_parked();
        assert!(cx.debug_bounds("terminal-grid").is_some());
        // The grid is set only when the layout had metrics, and the paint needs them too.
        view.read_with(cx, |view, _| assert!(view.grid.is_some()));
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
    fn a_live_link_is_told_the_focus_state_and_the_size(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let (link, handle, frames) = test_link();
        view.update(cx, |view, _| view.link = Some((link, handle)));
        let live = LinkState::Live {
            server_version: "0.9.3".to_owned(),
            untested: false,
        };
        view.update(cx, |view, cx| view.on_update(LinkUpdate::State(live), cx));
        cx.run_until_parked();

        let (active, (size_sent, cell)) = (
            view.read_with(cx, |view, _| view.window_active),
            sent_size(cx, &view),
        );
        assert_eq!(
            queued(&frames),
            vec![
                ClientMsg::Focus(active).encode(),
                ClientMsg::Resize {
                    size: size_sent,
                    cell,
                }
                .encode(),
            ]
        );
    }

    #[gpui::test]
    fn a_view_too_small_for_a_grid_sends_no_size(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let before = view.read_with(cx, |view, _| view.sends.len());

        settle(cx, 20., 20.);
        assert_eq!(view.read_with(cx, |view, _| view.sends.len()), before);
    }

    #[gpui::test]
    fn a_failed_start_is_tried_again_on_a_click(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        view.update(cx, |view, _| {
            view.enabled = true;
            view.start = failing_start;
        });
        settle(cx, 800., 600.);
        let state = view.read_with(cx, |view, _| {
            assert!(view.link.is_none());
            view.state.clone()
        });
        assert!(matches!(state, LinkState::NotRunning(_)));
        let (notices, _observer) = count_notices(cx, &view);

        // The start fails again: the view keeps its state and does not notify.
        cx.simulate_click(point(px(CENTRE.0), px(CENTRE.1)), Modifiers::none());
        cx.run_until_parked();
        assert!(view.read_with(cx, |view, _| view.link.is_none()));
        assert_eq!(notices.get(), 0);

        // When the start works, the next click starts the link.
        view.update(cx, |view, _| view.start = working_start);
        cx.simulate_click(point(px(CENTRE.0), px(CENTRE.1)), Modifiers::none());
        cx.run_until_parked();
        assert!(view.read_with(cx, |view, _| view.link.is_some()));
    }

    #[gpui::test]
    fn the_terminal_has_the_keyboard_on_open_and_a_click_takes_it_back(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let focus = view.read_with(cx, |view, _| view.focus.clone());
        assert!(cx.update(|window, _| focus.is_focused(window)));

        cx.update(|window, cx| window.blur(cx));
        assert!(!cx.update(|window, _| focus.is_focused(window)));

        cx.simulate_click(point(px(CENTRE.0), px(CENTRE.1)), Modifiers::none());
        assert!(cx.update(|window, _| focus.is_focused(window)));
    }

    #[gpui::test]
    fn the_keys_the_terminal_takes_are_sent_to_the_focused_pane(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let frames = attach(cx, &view);

        cx.simulate_keystrokes("ctrl-c");
        assert_eq!(
            queued(&frames),
            vec![input(PANE, &[key(KeyCode::Char('c'), mods::CONTROL)])]
        );

        let cases = [
            ("enter", key(KeyCode::Enter, 0)),
            ("shift-enter", key(KeyCode::Enter, mods::SHIFT)),
            ("escape", key(KeyCode::Esc, 0)),
            ("up", key(KeyCode::Up, 0)),
            ("alt-backspace", key(KeyCode::Backspace, mods::ALT)),
            ("shift-tab", key(KeyCode::BackTab, mods::SHIFT)),
        ];
        for (keystroke, event) in cases {
            cx.simulate_keystrokes(keystroke);
            assert_eq!(
                queued(&frames),
                vec![input(PANE, std::slice::from_ref(&event))],
                "{keystroke}"
            );
        }
    }

    #[gpui::test]
    fn typed_text_is_sent_as_text_and_option_never_makes_a_key(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let frames = attach(cx, &view);

        cx.simulate_input("é");
        assert_eq!(queued(&frames), vec![input(PANE, &[text("é")])]);

        cx.simulate_keystrokes("alt-e");
        assert!(queued(&frames).is_empty(), "Option types characters (D11)");
    }

    #[gpui::test]
    fn cmd_v_pastes_the_clipboard_text_as_one_paste(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let frames = attach(cx, &view);

        cx.simulate_keystrokes("cmd-v");
        assert!(
            queued(&frames).is_empty(),
            "an empty clipboard pastes nothing"
        );

        cx.write_to_clipboard(ClipboardItem::new_string("one\ntwo".to_owned()));
        cx.simulate_keystrokes("cmd-v");
        assert_eq!(
            queued(&frames),
            vec![input(PANE, &[InputEvent::Paste("one\ntwo".to_owned())])]
        );
    }

    #[gpui::test]
    fn cmd_shift_v_and_cmd_alt_v_are_not_a_paste(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let frames = attach(cx, &view);
        cx.write_to_clipboard(ClipboardItem::new_string("one".to_owned()));

        cx.simulate_keystrokes("cmd-shift-v");
        cx.simulate_keystrokes("cmd-alt-v");
        assert!(queued(&frames).is_empty(), "only Cmd-V pastes");

        // Cmd-V still pastes: the two chords above were not sent by accident.
        cx.simulate_keystrokes("cmd-v");
        assert_eq!(
            queued(&frames),
            vec![input(PANE, &[InputEvent::Paste("one".to_owned())])]
        );
    }

    #[gpui::test]
    fn composed_text_is_painted_and_sent_once_when_committed(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let frames = attach(cx, &view);

        cx.update(|window, app| {
            view.update(app, |view, cx| {
                view.replace_and_mark_text_in_range(None, "ni", None, window, cx)
            })
        });
        cx.update(|window, app| {
            view.update(app, |view, cx| {
                assert_eq!(view.marked_text_range(window, cx), Some(0..2));
            })
        });
        assert!(queued(&frames).is_empty(), "composing sends nothing");

        cx.update(|window, app| {
            view.update(app, |view, cx| {
                view.replace_text_in_range(None, "你", window, cx)
            })
        });
        assert_eq!(queued(&frames), vec![input(PANE, &[text("你")])]);
        view.read_with(cx, |view, _| assert!(view.marked.is_none()));
    }

    #[gpui::test]
    fn an_unmarked_composition_is_sent_as_text_once(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let frames = attach(cx, &view);

        cx.update(|window, app| {
            view.update(app, |view, cx| {
                view.replace_and_mark_text_in_range(None, "ni", None, window, cx)
            })
        });
        // The system commits the composition by unmarking it: the composed text is sent.
        cx.update(|window, app| view.update(app, |view, cx| view.unmark_text(window, cx)));
        view.read_with(cx, |view, _| assert!(view.marked.is_none()));
        assert_eq!(queued(&frames), vec![input(PANE, &[text("ni")])]);

        // With no composition left, another unmark sends nothing.
        cx.update(|window, app| view.update(app, |view, cx| view.unmark_text(window, cx)));
        assert!(queued(&frames).is_empty());
    }

    #[gpui::test]
    fn the_ime_window_is_placed_at_the_cursor_cell(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let element = cx.debug_bounds("terminal-grid").unwrap();
        let no_surface = cx.update(|window, app| {
            view.update(app, |view, cx| {
                view.bounds_for_range(0..0, element, window, cx)
            })
        });
        assert!(no_surface.is_none());

        view.update(cx, |view, cx| view.accept_surface(surface(1, "a"), cx));
        cx.run_until_parked();
        let m = view.read_with(cx, |view, _| view.metrics.unwrap());
        let at = cx
            .update(|window, app| {
                view.update(app, |view, cx| {
                    view.bounds_for_range(0..0, element, window, cx)
                })
            })
            .unwrap();
        // The cursor is in column 1, row 0.
        assert_eq!(at.size, size(px(m.cell_w), px(m.cell_h)));
        assert_eq!(at.origin.x, element.origin.x + px(m.pad + m.cell_w));
        assert_eq!(at.origin.y, element.origin.y + px(m.pad));
    }

    #[gpui::test]
    fn a_key_with_no_pane_sends_nothing(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let (link, handle, frames) = test_link();
        view.update(cx, |view, cx| {
            view.link = Some((link, handle));
            // A surface that names no pane, and no snapshot yet: there is nowhere to send.
            view.accept_surface(shown(1, GridSize { cols: 3, rows: 2 }, None), cx);
        });
        cx.run_until_parked();

        cx.simulate_keystrokes("ctrl-c");
        assert!(queued(&frames).is_empty());
    }

    #[gpui::test]
    fn a_key_without_a_link_is_dropped_and_the_view_keeps_running(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        view.update(cx, |view, cx| {
            view.accept_surface(shown(1, GridSize { cols: 3, rows: 2 }, Some(PANE)), cx)
        });
        cx.run_until_parked();

        let before = view.read_with(cx, |view, _| view.sends.len());
        cx.simulate_keystrokes("ctrl-c");
        assert_eq!(view.read_with(cx, |view, _| view.sends.len()), before);
        assert!(view.read_with(cx, |view, _| view.link.is_none()));
    }

    #[gpui::test]
    fn the_shell_pane_takes_keys_when_the_surface_names_none(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let (link, handle, frames) = test_link();
        let snapshot = ShellSnapshot {
            focused_pane_id: Some("p9".to_owned()),
            ..Default::default()
        };
        let (sent, _) = sent_size(cx, &view);
        view.update(cx, |view, cx| {
            view.link = Some((link, handle));
            view.on_update(LinkUpdate::Snapshot(Arc::new(snapshot)), cx);
            view.accept_surface(shown(1, sent, None), cx);
        });
        cx.run_until_parked();

        cx.simulate_keystrokes("ctrl-c");
        assert_eq!(
            queued(&frames),
            vec![input("p9", &[key(KeyCode::Char('c'), mods::CONTROL)])]
        );
    }

    #[gpui::test]
    fn input_that_is_not_queued_does_not_re_send_the_size(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let (size, cell) = sent_size(cx, &view);
        let resize = ClientMsg::Resize { size, cell }.encode();

        // The link's frames have no receiver left, so each queue fails with NotConnected.
        let (link, handle, frames) = test_link();
        drop(frames);
        let other = GridSize { cols: 3, rows: 2 };
        view.update(cx, |view, cx| {
            view.link = Some((link, handle));
            // herdr shows another size: a re-send would be due if the input were queued.
            view.accept_surface(shown(1, other, Some(PANE)), cx);
        });
        cx.run_until_parked();
        let t0 = Instant::now();
        view.update(cx, |view, _| view.send(&[text("a")], t0));
        view.read_with(cx, |view, _| {
            assert!(view.reasserted_at.is_none(), "nothing was queued");
        });

        // The link works now. The size was not re-sent by the failed input, so the next one is.
        let (link, handle, frames) = test_link();
        view.update(cx, |view, _| view.link = Some((link, handle)));
        view.update(cx, |view, _| view.send(&[text("b")], t0));
        assert_eq!(queued(&frames), vec![input(PANE, &[text("b")]), resize]);
    }

    #[gpui::test]
    fn a_lost_link_forgets_the_shell_pane(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let snapshot = ShellSnapshot {
            focused_pane_id: Some("p9".to_owned()),
            ..Default::default()
        };
        view.update(cx, |view, cx| {
            view.on_update(LinkUpdate::Snapshot(Arc::new(snapshot)), cx)
        });
        view.read_with(cx, |view, _| {
            assert_eq!(view.shell_pane.as_deref(), Some("p9"));
        });

        view.update(cx, |view, cx| {
            view.on_update(
                LinkUpdate::State(LinkState::NotRunning(NOT_RUNNING.to_owned())),
                cx,
            )
        });
        view.read_with(cx, |view, _| assert!(view.shell_pane.is_none()));
    }

    #[gpui::test]
    fn the_window_focus_state_goes_to_herdr(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        // The test window starts inactive. Activating it is the observed change.
        cx.update(|window, _| window.activate_window());
        cx.run_until_parked();
        let frames = attach(cx, &view);
        assert!(view.read_with(cx, |view, _| view.window_active));

        cx.deactivate_window();
        assert_eq!(queued(&frames), vec![ClientMsg::Focus(false).encode()]);

        cx.update(|window, _| window.activate_window());
        cx.run_until_parked();
        assert_eq!(queued(&frames), vec![ClientMsg::Focus(true).encode()]);
    }

    #[gpui::test]
    fn typing_sends_the_size_again_only_when_herdr_shows_another_size(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        settle(cx, 800., 600.);
        let (link, handle, frames) = test_link();
        let (sent_size, sent_cell) = sent_size(cx, &view);
        view.update(cx, |view, _| view.link = Some((link, handle)));
        let resize = ClientMsg::Resize {
            size: sent_size,
            cell: sent_cell,
        }
        .encode();

        // herdr shows the size contr sent: typing sends the text only.
        view.update(cx, |view, cx| {
            view.accept_surface(shown(1, sent_size, Some(PANE)), cx)
        });
        cx.run_until_parked();
        let t0 = Instant::now();
        view.update(cx, |view, _| view.send(&[text("a")], t0));
        assert_eq!(queued(&frames), vec![input(PANE, &[text("a")])]);

        // herdr shows another size: the first key sends it again, and a key within a second does not.
        let other = GridSize { cols: 3, rows: 2 };
        view.update(cx, |view, cx| {
            view.accept_surface(shown(2, other, Some(PANE)), cx)
        });
        cx.run_until_parked();
        view.update(cx, |view, _| {
            view.send(&[text("b")], t0 + Duration::from_millis(500))
        });
        assert_eq!(
            queued(&frames),
            vec![input(PANE, &[text("b")]), resize.clone()]
        );
        view.update(cx, |view, _| {
            view.send(&[text("c")], t0 + Duration::from_millis(900))
        });
        assert_eq!(queued(&frames), vec![input(PANE, &[text("c")])]);

        // A second later it is sent again.
        view.update(cx, |view, _| {
            view.send(&[text("d")], t0 + Duration::from_secs(2))
        });
        assert_eq!(queued(&frames), vec![input(PANE, &[text("d")]), resize]);
    }

    #[gpui::test]
    fn a_failed_start_is_not_retried_on_a_timer(cx: &mut TestAppContext) {
        let (view, cx) = cx.add_window_view(TerminalView::new);
        view.update(cx, |view, _| {
            view.enabled = true;
            view.start = counting_failing_start;
        });
        settle(cx, 800., 600.);
        assert_eq!(
            STARTS.with(Counter::get),
            1,
            "the layout starts the link once"
        );
        // Time passes with no user event: the start is not tried again.
        cx.executor().advance_clock(Duration::from_secs(10));
        cx.run_until_parked();
        assert_eq!(STARTS.with(Counter::get), 1, "no timer starts it again");
        assert!(view.read_with(cx, |view, _| view.link.is_none()));
    }
}
