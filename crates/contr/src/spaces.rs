//! The sidebar's spaces column: the header (window-button inset, toggle, wordmark) and the body
//! that scrolls. The state keeps the repo groups in step with herdr and git; rows fill the body in
//! a later slice.

use crate::agents_list::{FocusFn, focus_in_background};
use crate::git::{self, Scan};
use crate::herdr::{GlobalHerdr, Herdr};
use crate::selection::{GlobalSelection, Selected, Selection};
use crate::spaces_model::{self, Badge, RepoGroup, WorktreeRow};
use crate::status_icon::StatusIcon;
use crate::theme::{
    BADGE_PAD_X, BADGE_RADIUS, BG_CARD, CARD_BORDER, CARD_INSET_X, CARD_RADIUS, HOST_DOT_SIZE,
    ICON_FAINT, ICON_SIZE_SMALL, LINE_HEIGHT, ROW_INDENT, SIDEBAR_PAD_X, SIDEBAR_TOGGLE_BOX,
    SIDEBAR_TOGGLE_FILL, SIDEBAR_TOGGLE_HEIGHT, SIDEBAR_TOGGLE_RADIUS, SIDEBAR_TOGGLE_WIDTH,
    SPACES_GAP, SPACES_ROW_HEIGHT, TEXT, TEXT_2, TEXT_DIM, TEXT_DIM_2, TITLE_BAR_HEIGHT,
    TRAFFIC_LIGHT_INSET, WORKTREE_ROW_HEIGHT, YOU_BG, YOU_TEXT,
};
use crate::visibility::GlobalVisibility;
use async_channel::Receiver;
use contr_herdr::ConnState;
use gpui::{
    App, AsyncApp, Context, Div, Entity, EventEmitter, FontWeight, IntoElement, MouseButton,
    ParentElement, Render, Stateful, Styled, Subscription, Task, WeakEntity, Window, div,
    prelude::*, px, rgb,
};
use std::rc::Rc;
use std::time::{Duration, Instant};

/// How often the sidebar rescans git while the window is shown, so a changed branch appears.
pub const RESCAN_EVERY: Duration = Duration::from_secs(10);

/// Sent by the header toggle; the frame shows or hides the sidebar.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SpacesEvent {
    ToggleSidebar,
}

pub struct Spaces {
    herdr: Entity<Herdr>,
    selection: Entity<Selection>,
    /// Starts a focus call for a pane. The app sends `pane.focus`; the tests record the ids.
    focus: FocusFn,
    /// The repo groups the body shows. Empty while herdr has no tree.
    pub groups: Vec<RepoGroup>,
    /// herdr's connection is up. The host card's dot shows it.
    online: bool,
    /// The checkouts found by the last finished scan.
    scan: Scan,
    /// The pane cwds the last scan was started for.
    scanned_cwds: Vec<String>,
    /// When the last scan started. `None` before the first one.
    last_scan: Option<Instant>,
    /// The scan in flight. Starting another one drops this task, so only the newest result lands.
    scanning: Option<Task<()>>,
    /// Scans started, for the tests.
    #[cfg(test)]
    scans: usize,
    _subs: Vec<Subscription>,
    /// The rescan timer. It waits while the window is hidden.
    _tick: Task<()>,
}

impl EventEmitter<SpacesEvent> for Spaces {}

impl Spaces {
    /// Needs `Herdr::init`, `Selection::init` and `Visibility::init` first. Scans at once when
    /// herdr already has a tree, then follows herdr's changes and the rescan timer.
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self::with_focus(Rc::new(focus_in_background), cx)
    }

    /// `new` with the focus call given, so the tests can record the pane ids that a click focuses.
    fn with_focus(focus: FocusFn, cx: &mut Context<Self>) -> Self {
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let selection = cx.global::<GlobalSelection>().0.clone();
        let visibility = cx.global::<GlobalVisibility>().0.clone();
        let herdr_sub = cx.observe(&herdr, |this, _, cx| this.on_herdr_changed(cx));
        // The rows draw the selection, so a change to it repaints the sidebar.
        let selection_sub = cx.observe(&selection, |_, _, cx| cx.notify());
        // Every visibility change is sent; the timer loop keeps the last one.
        let (changes_tx, changes_rx) = async_channel::unbounded::<bool>();
        let visibility_sub = cx.observe(&visibility, move |_, visibility, cx| {
            let _ = changes_tx.try_send(visibility.read(cx).visible);
        });
        let visible = visibility.read(cx).visible;
        let tick = cx.spawn(async move |this, cx| tick(this, cx, changes_rx, visible).await);
        let mut spaces = Self {
            herdr,
            selection,
            focus,
            groups: Vec::new(),
            online: false,
            scan: Scan::default(),
            scanned_cwds: Vec::new(),
            last_scan: None,
            scanning: None,
            #[cfg(test)]
            scans: 0,
            _subs: vec![herdr_sub, visibility_sub, selection_sub],
            _tick: tick,
        };
        spaces.on_herdr_changed(cx);
        spaces
    }

    /// herdr's state changed. Rescans when a pane sits in another directory, or when the last scan
    /// is due by age and the window is shown. Then rebuilds the groups from the tree and the scan
    /// we have. With no pane cwd there is nothing to scan.
    fn on_herdr_changed(&mut self, cx: &mut Context<Self>) {
        let cwds = self.tree_cwds(cx);
        if cwds.is_empty() {
            self.forget_scan();
        } else if cwds != self.scanned_cwds || (self.is_stale(cx) && shown(cx)) {
            self.start_scan(cwds, cx);
        }
        self.rebuild(cx);
    }

    /// The timer's rescan: when the last scan is due by age, the same cwds again, because a branch
    /// may have changed since. Nothing is scanned without cwds.
    fn rescan_if_stale(&mut self, cx: &mut Context<Self>) {
        if self.scanned_cwds.is_empty() || !self.is_stale(cx) {
            return;
        }
        let cwds = self.scanned_cwds.clone();
        self.start_scan(cwds, cx);
    }

    /// How long the timer waits from `shown_at`: until one interval after the later of the last
    /// scan and the moment the window was shown. Without cwds nothing is scanned, so it waits one
    /// interval. Otherwise the wait is zero only when the scan is due, and then it runs at once.
    fn rescan_wait(&self, shown_at: Instant, cx: &App) -> Duration {
        if self.scanned_cwds.is_empty() {
            return RESCAN_EVERY;
        }
        let base = self.last_scan.map_or(shown_at, |last| last.max(shown_at));
        let since = cx
            .background_executor()
            .now()
            .saturating_duration_since(base);
        RESCAN_EVERY.saturating_sub(since)
    }

    /// Whether the last scan is `RESCAN_EVERY` old or more, or there is none. It reads the
    /// executor's clock, the same clock the rescan timer waits on, so one interval is one interval.
    fn is_stale(&self, cx: &App) -> bool {
        self.last_scan.is_none_or(|at| {
            cx.background_executor().now().saturating_duration_since(at) >= RESCAN_EVERY
        })
    }

    /// Drops the scan when no pane has a cwd. Nothing is scanned until one has.
    fn forget_scan(&mut self) {
        self.scanning = None;
        self.scan = Scan::default();
        self.scanned_cwds.clear();
        self.last_scan = None;
    }

    /// The pane cwds of the herdr tree, in tree order. Empty without a tree.
    fn tree_cwds(&self, cx: &App) -> Vec<String> {
        self.herdr
            .read(cx)
            .live
            .tree
            .as_deref()
            .map(spaces_model::pane_cwds)
            .unwrap_or_default()
    }

    /// Scans `cwds` on the background executor and rebuilds when the result is in. The scan runs
    /// off the UI thread, so the sidebar never waits for git.
    fn start_scan(&mut self, cwds: Vec<String>, cx: &mut Context<Self>) {
        self.last_scan = Some(cx.background_executor().now());
        self.scanned_cwds = cwds.clone();
        #[cfg(test)]
        {
            self.scans += 1;
        }
        self.scanning = Some(cx.spawn(async move |this, cx| {
            let scan = cx
                .background_executor()
                .spawn(async move { git::scan(&cwds) })
                .await;
            // The sidebar may have been dropped while the scan ran; then nothing is left to update.
            let _ = this.update(cx, |spaces, cx| {
                spaces.scan = scan;
                spaces.rebuild(cx);
            });
        }));
    }

    /// Rebuilds `groups` from the herdr tree and the scan, and the connection flag. Notifies only
    /// when one of them changed. Then clears a selected worktree that no row has any more.
    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let live = &self.herdr.read(cx).live;
        let groups = live
            .tree
            .as_deref()
            .map(|tree| spaces_model::build(tree, &self.scan))
            .unwrap_or_default();
        let online = matches!(live.state, ConnState::Connected { .. });
        if groups != self.groups || online != self.online {
            self.groups = groups;
            self.online = online;
            cx.notify();
        }
        self.clear_gone_selection(cx);
    }

    /// A worktree row was clicked: it becomes the selection, and its top holder's pane is focused
    /// in herdr. A row without a holder is selected and nothing is focused. A second click focuses
    /// again.
    fn activate(&mut self, row_id: String, focus_pane: Option<String>, cx: &mut Context<Self>) {
        self.selection.update(cx, |selection, cx| {
            selection.select(Selected::Worktree(row_id), cx)
        });
        if let Some(pane_id) = focus_pane {
            (self.focus)(pane_id, cx);
        }
    }

    /// Clears the selection when it names a worktree that no group lists any more. An agent
    /// selection is not a worktree, so it stays.
    fn clear_gone_selection(&self, cx: &mut Context<Self>) {
        let selection = self.selection.read(cx);
        let Some(Selected::Worktree(id)) = &selection.current else {
            return;
        };
        let listed = self
            .groups
            .iter()
            .flat_map(|group| group.rows.iter())
            .any(|row| row.id() == *id);
        if listed {
            return;
        }
        self.selection
            .update(cx, |selection, cx| selection.clear(cx));
    }
}

/// Whether the window is shown. A scan that is due only by age waits while it is hidden.
fn shown(cx: &App) -> bool {
    cx.global::<GlobalVisibility>().0.read(cx).visible
}

/// The rescan timer. Shown: it wakes one interval after the later of the last scan and the moment
/// the window was shown, then rescans if the scan is due. Hidden: it waits for the next visibility
/// change and starts no timer, as the host sampler does.
async fn tick(
    this: WeakEntity<Spaces>,
    cx: &mut AsyncApp,
    changes: Receiver<bool>,
    mut visible: bool,
) {
    let mut shown_at = cx.background_executor().now();
    loop {
        while !visible {
            match changes.recv().await {
                Ok(now_visible) => {
                    visible = now_visible;
                    if visible {
                        shown_at = cx.background_executor().now();
                    }
                }
                Err(_) => return, // the sidebar was dropped
            }
        }
        let Ok(wait) = this.update(cx, |spaces, cx| spaces.rescan_wait(shown_at, cx)) else {
            return; // the sidebar was dropped
        };
        cx.background_executor().timer(wait).await;
        // Visibility changes that arrived during the wait; the last one wins. A change to shown
        // starts the interval again.
        while let Ok(now_visible) = changes.try_recv() {
            if now_visible && !visible {
                shown_at = cx.background_executor().now();
            }
            visible = now_visible;
        }
        if visible
            && this
                .update(cx, |spaces, cx| spaces.rescan_if_stale(cx))
                .is_err()
        {
            return; // the sidebar was dropped
        }
    }
}

impl Render for Spaces {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let selected = self.selection.read(cx).current.clone();
        let groups: Vec<Div> = self
            .groups
            .iter()
            .enumerate()
            .map(|(g, group)| {
                let rows: Vec<Stateful<Div>> = group
                    .rows
                    .iter()
                    .enumerate()
                    .map(|(r, row)| {
                        let is_selected = selected.as_ref() == Some(&Selected::Worktree(row.id()));
                        worktree_row(g, r, row, is_selected, cx)
                    })
                    .collect();
                div().child(repo_line(g, group)).children(rows)
            })
            .collect();
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
                    .overflow_y_scroll()
                    .child(spaces_header())
                    .child(host_card(self.online))
                    .children(groups),
            )
    }
}

/// The `Spaces` label, and `+ new`. The button has no action yet (D50), so it has no pointer.
fn spaces_header() -> Div {
    div()
        .debug_selector(|| "spaces-header".into())
        .h(px(SPACES_ROW_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .justify_between()
        .px(px(SIDEBAR_PAD_X))
        .text_color(rgb(TEXT_DIM))
        .child("Spaces")
        .child(
            div()
                .debug_selector(|| "spaces-new".into())
                .text_color(rgb(TEXT_DIM_2))
                .child("+ new"),
        )
}

/// The host card: `Local`, and a dot that is lit while herdr is connected. CPU and memory come
/// later.
fn host_card(online: bool) -> Div {
    div()
        .debug_selector(|| "host-local".into())
        .mx(px(CARD_INSET_X))
        .h(px(SPACES_ROW_HEIGHT))
        .flex()
        .items_center()
        .justify_between()
        .px(px(SIDEBAR_PAD_X - CARD_INSET_X))
        .rounded(px(CARD_RADIUS))
        .bg(rgb(BG_CARD))
        .border_1()
        .border_color(rgb(CARD_BORDER))
        .child(div().text_color(rgb(TEXT)).child("Local"))
        .child(
            div()
                .debug_selector(|| "host-online".into())
                .size(px(HOST_DOT_SIZE))
                .rounded_full()
                .bg(rgb(online_colour(online))),
        )
}

/// The dot's colour: `TEXT_2` while herdr is connected, the faint icon grey otherwise.
fn online_colour(online: bool) -> u32 {
    if online { TEXT_2 } else { ICON_FAINT }
}

/// A repo's line: its name, then its branch when it has one.
fn repo_line(g: usize, group: &RepoGroup) -> Div {
    div()
        .debug_selector(move || format!("repo-{g}"))
        .h(px(SPACES_ROW_HEIGHT))
        .flex()
        .items_center()
        .gap(px(SPACES_GAP))
        .px(px(SIDEBAR_PAD_X))
        .child(
            div()
                .min_w_0()
                .overflow_hidden()
                .text_ellipsis()
                .whitespace_nowrap()
                .text_color(rgb(TEXT))
                .child(group.name.clone()),
        )
        .when_some(group.branch.clone(), |line, branch| {
            line.child(
                div()
                    .min_w_0()
                    .overflow_hidden()
                    .text_ellipsis()
                    .whitespace_nowrap()
                    .text_color(rgb(TEXT_DIM))
                    .child(branch),
            )
        })
}

/// A worktree's name colour: white when a tab holds it, dim when it is only a checkout.
fn name_colour(held: bool) -> u32 {
    if held { TEXT } else { TEXT_DIM }
}

/// One worktree row, two lines: the name with the status icon, and the hold line. The selected row
/// draws its card behind the content. A click selects the row and focuses its top holder's pane.
fn worktree_row(
    g: usize,
    r: usize,
    row: &WorktreeRow,
    selected: bool,
    cx: &mut Context<Spaces>,
) -> Stateful<Div> {
    let index = g * 1000 + r;
    let row_id = row.id();
    let focus_pane = row.focus_pane.clone();
    let held = !row.holders.is_empty();
    div()
        .id(("wt-row", index))
        .debug_selector(move || format!("wt-{g}-{r}"))
        .relative()
        .w_full()
        .h(px(WORKTREE_ROW_HEIGHT))
        .flex()
        .items_center()
        .gap(px(SPACES_GAP))
        .pl(px(SIDEBAR_PAD_X + ROW_INDENT))
        .pr(px(SIDEBAR_PAD_X))
        .cursor_pointer()
        .on_click(cx.listener(move |spaces, _, _, cx| {
            spaces.activate(row_id.clone(), focus_pane.clone(), cx)
        }))
        .when(selected, |line| line.child(selected_card(g, r)))
        .child(StatusIcon::new(("wt", index), row.status).size(px(ICON_SIZE_SMALL)))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .child(
                    div()
                        .h(px(LINE_HEIGHT))
                        .overflow_hidden()
                        .text_ellipsis()
                        .whitespace_nowrap()
                        .text_color(rgb(name_colour(held)))
                        .child(row.name.clone()),
                )
                .child(hold_line(g, r, row.hold.as_deref())),
        )
        .when_some(row.badge.as_ref(), |line, badge| {
            line.child(badge_chip(g, r, badge))
        })
}

/// The card of the selected worktree row. It sits first, so the row's content paints over it.
fn selected_card(g: usize, r: usize) -> Div {
    div()
        .debug_selector(move || format!("wt-{g}-{r}-selected"))
        .absolute()
        .top_0()
        .bottom_0()
        .left(px(CARD_INSET_X))
        .right(px(CARD_INSET_X))
        .rounded(px(CARD_RADIUS))
        .bg(rgb(BG_CARD))
        .border_1()
        .border_color(rgb(CARD_BORDER))
}

/// The second line: the top holder's tab and state. A row without a holder keeps the line, empty.
fn hold_line(g: usize, r: usize, hold: Option<&str>) -> Div {
    let line = div()
        .h(px(LINE_HEIGHT))
        .overflow_hidden()
        .text_ellipsis()
        .whitespace_nowrap()
        .text_color(rgb(TEXT_DIM_2));
    match hold {
        Some(text) => line
            .debug_selector(move || format!("wt-{g}-{r}-hold"))
            .child(text.to_string()),
        None => line,
    }
}

/// The row's mark: `#640` for a pull request, or a white chip for a value that waits on the owner.
fn badge_chip(g: usize, r: usize, badge: &Badge) -> Div {
    let chip = div()
        .debug_selector(move || format!("wt-{g}-{r}-badge"))
        .flex_none()
        .px(px(BADGE_PAD_X))
        .rounded(px(BADGE_RADIUS))
        .whitespace_nowrap();
    match badge {
        Badge::Pr(number) => chip.text_color(rgb(TEXT_DIM)).child(format!("#{number}")),
        Badge::You(text) => chip
            .bg(rgb(YOU_BG))
            .text_color(rgb(YOU_TEXT))
            .font_weight(FontWeight::BOLD)
            .child(text.clone()),
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
        .size(px(SIDEBAR_TOGGLE_BOX))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .child(
            div()
                .w(px(SIDEBAR_TOGGLE_WIDTH))
                .h(px(SIDEBAR_TOGGLE_HEIGHT))
                .rounded(px(SIDEBAR_TOGGLE_RADIUS))
                .border_1()
                .border_color(rgb(TEXT_DIM))
                .overflow_hidden()
                .flex()
                .child(div().w(px(SIDEBAR_TOGGLE_FILL)).h_full().bg(rgb(TEXT_DIM))),
        )
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::git::fake::{linked, main_repo, write};
    use crate::status_icon::Status;
    use crate::theme::{SIDEBAR_WIDTH, WINDOW_HEIGHT};
    use crate::visibility::Visibility;
    use contr_herdr::{
        AgentState, ConnState, HerdrTree, HerdrUpdate, LiveHerdr, Pane, Tab, Workspace,
    };
    use gpui::{AppContext, Entity, Modifiers, TestAppContext, VisualTestContext, size};
    use std::cell::{Cell, RefCell};
    use std::fs;
    use std::path::{Path, PathBuf};
    use std::rc::Rc;
    use std::sync::Arc;

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
        register(cx, None);
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
        register(cx, None);
        let (spaces, cx) = cx.add_window_view(|_, cx| Spaces::new(cx));
        cx.simulate_resize(size(px(280.), px(900.)));
        cx.run_until_parked();
        let events = record_events(cx, &spaces);
        let body = cx.debug_bounds("spaces-body").unwrap();
        cx.simulate_click(body.center(), Modifiers::none());
        cx.run_until_parked();
        assert!(events.borrow().is_empty());
    }

    /// Registers what a `Spaces` reads: a shown window, the selection, and a detached herdr that
    /// starts with `tree`. Returns the herdr, so a test can send it updates.
    fn register(cx: &mut TestAppContext, tree: Option<HerdrTree>) -> Entity<Herdr> {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        cx.update(|app| {
            let live = LiveHerdr {
                state: ConnState::Connected {
                    server_version: "0.9.3".into(),
                },
                tree: tree.map(Arc::new),
            };
            let herdr = app.new(|cx| Herdr::detached(live, cx));
            app.set_global(GlobalHerdr(herdr.clone()));
            herdr
        })
    }

    fn pane(id: &str, cwd: &str, state: Option<AgentState>) -> Pane {
        Pane {
            id: id.into(),
            label: None,
            cwd: Some(cwd.into()),
            focused: false,
            agent: state.map(|status| contr_herdr::AgentInfo {
                name: "claude".into(),
                status,
            }),
            tokens: Default::default(),
        }
    }

    /// One workspace with one tab, `t1`, holding `panes`.
    fn tree(panes: Vec<Pane>) -> HerdrTree {
        HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: None,
            workspaces: vec![Workspace {
                id: "w1".into(),
                label: "contr".into(),
                status: AgentState::Idle,
                focused: true,
                tabs: vec![Tab {
                    id: "t1".into(),
                    label: "a".into(),
                    status: AgentState::Idle,
                    focused: true,
                    panes,
                }],
            }],
        }
    }

    /// Sends the herdr connection a new tree, as the reader does.
    fn send_tree(cx: &mut TestAppContext, herdr: &Entity<Herdr>, tree: HerdrTree) {
        let update = HerdrUpdate::Tree(Arc::new(tree));
        cx.update(|app| herdr.update(app, |herdr, cx| herdr.apply(update, cx)));
    }

    /// Sends the herdr connection `NotRunning`, which leaves no tree.
    fn lose_herdr(cx: &mut TestAppContext, herdr: &Entity<Herdr>) {
        let lost = HerdrUpdate::State(ConnState::NotRunning {
            reason: "gone".into(),
        });
        cx.update(|app| herdr.update(app, |herdr, cx| herdr.apply(lost, cx)));
    }

    /// A repo at `base/repo` with a linked worktree at `base/wt1` on branch `feature`. Returns
    /// the worktree's root and its admin dir.
    fn repo_with_worktree(base: &Path) -> (PathBuf, PathBuf) {
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        let root = base.join("wt1");
        let admin = linked(&git, "w1", &root, "ref: refs/heads/feature\n");
        (root, admin)
    }

    /// Two worktrees of one repo at `base/repo`: `base/wt1` on `feature`, `base/wt2` on `other`.
    fn repo_with_two_worktrees(base: &Path) -> (PathBuf, PathBuf) {
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        let wt1 = base.join("wt1");
        linked(&git, "w1", &wt1, "ref: refs/heads/feature\n");
        let wt2 = base.join("wt2");
        linked(&git, "w2", &wt2, "ref: refs/heads/other\n");
        (wt1, wt2)
    }

    /// Registers herdr with one tab whose pane `w1:p2` runs a `done` agent in `wt1`. Row 0 is then
    /// `wt1` (held) and row 1 is `wt2` (unheld). Returns the temp dir, which the test keeps alive,
    /// and `wt1`.
    fn register_held_worktree(cx: &mut TestAppContext) -> (tempfile::TempDir, PathBuf) {
        let (guard, base) = temp_dir();
        let (wt1, _wt2) = repo_with_two_worktrees(&base);
        register(
            cx,
            Some(tree(vec![pane(
                "w1:p2",
                &text(&wt1),
                Some(AgentState::Done),
            )])),
        );
        (guard, wt1)
    }

    /// A `Spaces` in a window as wide as the sidebar and as tall as the reference window. Clicks on
    /// its rows record the pane ids they focus, in order.
    fn show_spaces(
        cx: &mut TestAppContext,
    ) -> (
        Entity<Spaces>,
        Rc<RefCell<Vec<String>>>,
        &mut VisualTestContext,
    ) {
        let focused = Rc::new(RefCell::new(Vec::new()));
        let log = focused.clone();
        let focus: FocusFn =
            Rc::new(move |pane_id: String, _: &mut App| log.borrow_mut().push(pane_id));
        let (spaces, cx) = cx.add_window_view(|_, cx| Spaces::with_focus(focus, cx));
        cx.simulate_resize(size(px(SIDEBAR_WIDTH), px(WINDOW_HEIGHT)));
        cx.run_until_parked();
        (spaces, focused, cx)
    }

    /// Clicks the centre of the row with the given selector, as the mouse would.
    fn click_row(cx: &mut VisualTestContext, row: &'static str) {
        let bounds = cx.debug_bounds(row).unwrap();
        cx.simulate_click(bounds.center(), Modifiers::none());
        cx.run_until_parked();
    }

    /// The selection as the sidebar sees it.
    fn selected_in_view(cx: &mut VisualTestContext) -> Option<Selected> {
        cx.update(|_, app| app.global::<GlobalSelection>().0.read(app).current.clone())
    }

    /// Sets the selection from outside the sidebar, as another view would.
    fn select_in_view(cx: &mut VisualTestContext, selected: Selected) {
        let selection = cx.update(|_, app| app.global::<GlobalSelection>().0.clone());
        selection.update(cx, |selection, cx| selection.select(selected, cx));
        cx.run_until_parked();
    }

    /// A temp dir, and its path in canonical form (the form the scan's rows use).
    fn temp_dir() -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let path = fs::canonicalize(dir.path()).unwrap();
        (dir, path)
    }

    fn text(path: &Path) -> String {
        path.to_string_lossy().into_owned()
    }

    fn make_spaces(cx: &mut TestAppContext) -> Entity<Spaces> {
        cx.new(Spaces::new)
    }

    fn groups_of(cx: &mut TestAppContext, spaces: &Entity<Spaces>) -> Vec<RepoGroup> {
        spaces.read_with(cx, |spaces, _| spaces.groups.clone())
    }

    fn scans_of(cx: &mut TestAppContext, spaces: &Entity<Spaces>) -> usize {
        spaces.read_with(cx, |spaces, _| spaces.scans)
    }

    fn selection_of(cx: &mut TestAppContext) -> Option<Selected> {
        let selection = cx.read(|app| app.global::<GlobalSelection>().0.clone());
        selection.read_with(cx, |selection, _| selection.current.clone())
    }

    fn select(cx: &mut TestAppContext, selected: Selected) {
        let selection = cx.read(|app| app.global::<GlobalSelection>().0.clone());
        cx.update(|app| selection.update(app, |s, cx| s.select(selected, cx)));
    }

    fn set_visible(cx: &mut TestAppContext, visible: bool) {
        cx.update(|app| {
            let visibility = app.global::<GlobalVisibility>().0.clone();
            visibility.update(app, |v, cx| v.set(visible, cx));
        });
    }

    /// Runs what is due, moves the test clock by `by`, and runs what that makes due.
    fn advance(cx: &mut TestAppContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    #[gpui::test]
    fn the_scan_fills_the_groups_once_the_tree_is_known(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        let got = groups_of(cx, &spaces);
        assert_eq!(got.len(), 1);
        assert_eq!(got[0].name, "repo");
        // The main checkout is not held, so only the worktree has a row.
        assert_eq!(got[0].rows.len(), 1);
        assert_eq!(got[0].rows[0].id(), text(&wt));
        assert_eq!(scans_of(cx, &spaces), 1);
    }

    #[gpui::test]
    fn a_herdr_change_with_the_same_cwds_rebuilds_without_a_scan(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 1);

        let working = pane("w1:p1", &cwd, Some(AgentState::Working));
        send_tree(cx, &herdr, tree(vec![working]));
        cx.run_until_parked();
        assert_eq!(
            scans_of(cx, &spaces),
            1,
            "same cwds, last scan fresh: no scan"
        );
        assert_eq!(groups_of(cx, &spaces)[0].rows[0].status, Status::Working);
    }

    #[gpui::test]
    fn a_new_cwd_starts_one_more_scan(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();

        let main_cwd = text(&base.join("repo"));
        send_tree(
            cx,
            &herdr,
            tree(vec![
                pane("w1:p1", &cwd, None),
                pane("w1:p2", &main_cwd, None),
            ]),
        );
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 2);
        let got = groups_of(cx, &spaces);
        // The held main checkout comes first, then the worktree.
        assert_eq!(got[0].rows.len(), 2);
        assert!(got[0].rows[0].is_main);
        assert_eq!(got[0].rows[1].id(), cwd);
    }

    #[gpui::test]
    fn a_change_that_moves_no_row_does_not_notify_the_view(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _sub =
            cx.update(|app| app.observe(&spaces, move |_, _| counter.set(counter.get() + 1)));

        // Only the focus flag differs: herdr notices, the rows do not change.
        let mut focused = pane("w1:p1", &cwd, None);
        focused.focused = true;
        send_tree(cx, &herdr, tree(vec![focused]));
        cx.run_until_parked();
        let tree_changed = cx.read(|app| {
            let live = &herdr.read(app).live;
            live.tree
                .as_ref()
                .is_some_and(|t| t.workspaces[0].tabs[0].panes[0].focused)
        });
        assert!(tree_changed, "herdr took the update");
        assert_eq!(notified.get(), 0);
    }

    #[gpui::test]
    fn losing_the_tree_empties_the_groups(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert_eq!(groups_of(cx, &spaces).len(), 1);

        lose_herdr(cx, &herdr);
        cx.run_until_parked();
        assert!(groups_of(cx, &spaces).is_empty());
    }

    #[gpui::test]
    fn a_cwd_outside_git_gives_no_group(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        fs::create_dir_all(base.join("plain")).unwrap();
        register(
            cx,
            Some(tree(vec![pane("w1:p1", &text(&base.join("plain")), None)])),
        );
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert!(groups_of(cx, &spaces).is_empty());
        assert_eq!(
            scans_of(cx, &spaces),
            1,
            "it was still scanned, and found nothing"
        );
    }

    #[gpui::test]
    fn a_selected_worktree_that_stays_listed_stays_selected(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let _spaces = make_spaces(cx);
        cx.run_until_parked();
        select(cx, Selected::Worktree(cwd.clone()));

        send_tree(
            cx,
            &herdr,
            tree(vec![pane("w1:p1", &cwd, Some(AgentState::Done))]),
        );
        cx.run_until_parked();
        assert_eq!(selection_of(cx), Some(Selected::Worktree(cwd)));
    }

    #[gpui::test]
    fn a_selected_worktree_that_is_pruned_is_cleared(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let herdr = register(
            cx,
            Some(tree(vec![
                pane("w1:p1", &text(&base.join("repo")), None),
                pane("w1:p2", &text(&wt), None),
            ])),
        );
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        select(cx, Selected::Worktree(text(&wt)));

        // The worktree's checkout is gone, so git no longer lists it. A new cwd forces a scan.
        fs::remove_file(wt.join(".git")).unwrap();
        send_tree(
            cx,
            &herdr,
            tree(vec![
                pane("w1:p1", &text(&base.join("repo")), None),
                pane("w1:p3", &text(&base), None),
            ]),
        );
        cx.run_until_parked();
        let got = groups_of(cx, &spaces);
        assert_eq!(got[0].rows.len(), 1, "only the main checkout is left");
        assert!(got[0].rows[0].is_main);
        assert_eq!(selection_of(cx), None);
    }

    #[gpui::test]
    fn losing_the_tree_clears_a_worktree_selection(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let _spaces = make_spaces(cx);
        cx.run_until_parked();
        select(cx, Selected::Worktree(text(&wt)));

        lose_herdr(cx, &herdr);
        cx.run_until_parked();
        assert_eq!(selection_of(cx), None);
    }

    #[gpui::test]
    fn an_agent_selection_is_left_alone(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let _spaces = make_spaces(cx);
        cx.run_until_parked();
        select(cx, Selected::Agent("w1:p1".into()));

        lose_herdr(cx, &herdr);
        cx.run_until_parked();
        assert_eq!(selection_of(cx), Some(Selected::Agent("w1:p1".into())));
    }

    #[gpui::test]
    fn the_timer_rescans_while_the_window_is_shown(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 1);

        advance(cx, RESCAN_EVERY);
        assert_eq!(scans_of(cx, &spaces), 2);
        advance(cx, RESCAN_EVERY);
        assert_eq!(scans_of(cx, &spaces), 3);
    }

    #[gpui::test]
    fn the_timer_is_off_while_the_window_is_hidden(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();

        set_visible(cx, false);
        advance(cx, RESCAN_EVERY * 6);
        assert_eq!(scans_of(cx, &spaces), 1, "hidden: no rescan, however long");

        set_visible(cx, true);
        cx.run_until_parked();
        assert_eq!(
            scans_of(cx, &spaces),
            1,
            "shown: the first rescan waits one interval"
        );
        advance(cx, RESCAN_EVERY);
        assert_eq!(scans_of(cx, &spaces), 2);
    }

    #[gpui::test]
    fn an_old_scan_is_not_refreshed_by_herdr_while_hidden(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        // The last scan now looks old, as it would after a long wait.
        spaces.update(cx, |spaces, _| spaces.last_scan = None);
        set_visible(cx, false);
        cx.run_until_parked();

        send_tree(
            cx,
            &herdr,
            tree(vec![pane("w1:p1", &cwd, Some(AgentState::Working))]),
        );
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 1, "hidden: herdr starts no rescan");
        assert_eq!(
            groups_of(cx, &spaces)[0].rows[0].status,
            Status::Working,
            "the rows still follow herdr"
        );

        set_visible(cx, true);
        advance(cx, RESCAN_EVERY);
        assert_eq!(scans_of(cx, &spaces), 2, "shown: the timer rescans");
    }

    #[gpui::test]
    fn a_branch_change_shows_after_the_next_timer_rescan(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, admin) = repo_with_worktree(&base);
        register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert_eq!(
            groups_of(cx, &spaces)[0].rows[0].branch.as_deref(),
            Some("feature")
        );

        write(&admin.join("HEAD"), "ref: refs/heads/renamed\n");
        advance(cx, RESCAN_EVERY);
        assert_eq!(
            groups_of(cx, &spaces)[0].rows[0].branch.as_deref(),
            Some("renamed")
        );
    }

    #[gpui::test]
    fn an_old_scan_starts_one_on_a_herdr_change_while_shown(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        // The last scan now looks old, as it would after a long wait. The window is shown.
        spaces.update(cx, |spaces, _| spaces.last_scan = None);

        send_tree(
            cx,
            &herdr,
            tree(vec![pane("w1:p1", &cwd, Some(AgentState::Working))]),
        );
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 2, "shown: the old scan starts one");
        assert_eq!(groups_of(cx, &spaces)[0].rows[0].status, Status::Working);
    }

    #[gpui::test]
    fn the_timer_waits_a_full_interval_after_a_fresh_scan(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let cwd = text(&wt);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &cwd, None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 1, "the first scan, at 0 s");

        // A new cwd starts a scan at 5 s, half an interval before the timer is due.
        cx.executor().advance_clock(RESCAN_EVERY / 2);
        let main_cwd = text(&base.join("repo"));
        send_tree(
            cx,
            &herdr,
            tree(vec![
                pane("w1:p1", &cwd, None),
                pane("w1:p2", &main_cwd, None),
            ]),
        );
        cx.run_until_parked();
        assert_eq!(scans_of(cx, &spaces), 2, "the new cwd, at 5 s");

        // At 10 s the timer is due, but the scan at 5 s is still fresh: nothing starts.
        advance(cx, RESCAN_EVERY / 2);
        assert_eq!(scans_of(cx, &spaces), 2, "10 s: the scan at 5 s is fresh");

        // The timer rescans a full interval after the scan at 5 s.
        advance(cx, RESCAN_EVERY / 2);
        assert_eq!(scans_of(cx, &spaces), 3, "15 s: a full interval after 5 s");
    }

    #[gpui::test]
    fn no_tree_starts_no_scan_however_long_the_timer_runs(cx: &mut TestAppContext) {
        register(cx, None);
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        advance(cx, RESCAN_EVERY * 3);
        assert_eq!(scans_of(cx, &spaces), 0);
        assert!(groups_of(cx, &spaces).is_empty());
    }

    #[gpui::test]
    fn a_pane_without_a_cwd_starts_no_scan(cx: &mut TestAppContext) {
        let bare = Pane {
            cwd: None,
            ..pane("w1:p1", "", None)
        };
        register(cx, Some(tree(vec![bare])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        advance(cx, RESCAN_EVERY * 2);
        assert_eq!(scans_of(cx, &spaces), 0);
    }

    #[gpui::test]
    fn losing_the_tree_stops_the_timer_scans(cx: &mut TestAppContext) {
        let (_guard, base) = temp_dir();
        let (wt, _admin) = repo_with_worktree(&base);
        let herdr = register(cx, Some(tree(vec![pane("w1:p1", &text(&wt), None)])));
        let spaces = make_spaces(cx);
        cx.run_until_parked();

        lose_herdr(cx, &herdr);
        cx.run_until_parked();
        advance(cx, RESCAN_EVERY * 3);
        assert_eq!(
            scans_of(cx, &spaces),
            1,
            "the lost tree drops its scan, and no later scan starts"
        );
    }

    #[gpui::test]
    fn a_connection_change_alone_repaints_the_sidebar(cx: &mut TestAppContext) {
        let herdr = register(cx, None);
        let spaces = make_spaces(cx);
        cx.run_until_parked();
        assert!(spaces.read_with(cx, |spaces, _| spaces.online));

        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _sub =
            cx.update(|app| app.observe(&spaces, move |_, _| counter.set(counter.get() + 1)));
        lose_herdr(cx, &herdr);
        cx.run_until_parked();
        assert!(!spaces.read_with(cx, |spaces, _| spaces.online));
        assert_eq!(
            notified.get(),
            1,
            "the dot changed, so the sidebar repaints"
        );
    }

    #[test]
    fn the_dot_and_the_name_colours_follow_the_connection_and_the_hold() {
        assert_eq!(online_colour(true), TEXT_2);
        assert_eq!(online_colour(false), ICON_FAINT);
        assert_eq!(name_colour(true), TEXT);
        assert_eq!(name_colour(false), TEXT_DIM);
    }

    #[gpui::test]
    fn the_rows_show_the_repo_its_worktrees_and_the_host_card(cx: &mut TestAppContext) {
        let (_guard, _wt1) = register_held_worktree(cx);
        let (_spaces, _focused, cx) = show_spaces(cx);

        let sidebar = cx.debug_bounds("sidebar-spaces").unwrap();
        let repo = cx.debug_bounds("repo-0").unwrap();
        assert!(repo.origin.y >= sidebar.origin.y);
        let held = cx.debug_bounds("wt-0-0").unwrap();
        assert_eq!(held.size.height, px(WORKTREE_ROW_HEIGHT));
        assert_eq!(held.size.width, px(SIDEBAR_WIDTH));
        assert!(
            cx.debug_bounds("wt-0-0-hold").is_some(),
            "held: a hold line"
        );
        assert!(
            cx.debug_bounds("wt-0-1").is_some(),
            "unheld linked worktree: a row"
        );
        assert!(
            cx.debug_bounds("wt-0-1-hold").is_none(),
            "unheld: no hold line"
        );
        assert!(cx.debug_bounds("wt-0-0-selected").is_none());
        assert!(cx.debug_bounds("spaces-new").is_some());
        assert!(cx.debug_bounds("host-local").is_some());
        assert!(cx.debug_bounds("host-online").is_some());
    }

    #[gpui::test]
    fn a_click_selects_the_row_and_focuses_its_holder(cx: &mut TestAppContext) {
        let (_guard, wt1) = register_held_worktree(cx);
        let (_spaces, focused, cx) = show_spaces(cx);

        click_row(cx, "wt-0-0");
        assert!(cx.debug_bounds("wt-0-0-selected").is_some());
        assert_eq!(selected_in_view(cx), Some(Selected::Worktree(text(&wt1))));
        assert_eq!(*focused.borrow(), vec!["w1:p2".to_string()]);

        click_row(cx, "wt-0-0");
        assert_eq!(
            *focused.borrow(),
            vec!["w1:p2".to_string(), "w1:p2".to_string()],
            "a second click focuses again"
        );
    }

    #[gpui::test]
    fn a_click_on_an_unheld_row_moves_the_card_without_a_focus_call(cx: &mut TestAppContext) {
        let (_guard, _wt1) = register_held_worktree(cx);
        let (_spaces, focused, cx) = show_spaces(cx);

        click_row(cx, "wt-0-0");
        click_row(cx, "wt-0-1");
        assert!(cx.debug_bounds("wt-0-1-selected").is_some());
        assert!(cx.debug_bounds("wt-0-0-selected").is_none());
        assert_eq!(
            *focused.borrow(),
            vec!["w1:p2".to_string()],
            "only the held row focused"
        );
    }

    #[gpui::test]
    fn an_agent_selection_hides_the_worktree_card(cx: &mut TestAppContext) {
        let (_guard, _wt1) = register_held_worktree(cx);
        let (_spaces, _focused, cx) = show_spaces(cx);

        click_row(cx, "wt-0-0");
        assert!(cx.debug_bounds("wt-0-0-selected").is_some());
        select_in_view(cx, Selected::Agent("w1:p2".into()));
        assert!(
            cx.debug_bounds("wt-0-0-selected").is_none(),
            "an agent selection clears the worktree card"
        );
    }

    #[gpui::test]
    fn a_badge_in_the_model_shows_on_its_row(cx: &mut TestAppContext) {
        let (_guard, _wt1) = register_held_worktree(cx);
        let (spaces, _focused, cx) = show_spaces(cx);
        assert!(
            cx.debug_bounds("wt-0-0-badge").is_none(),
            "no badge until the bridge sends one"
        );

        spaces.update(cx, |spaces, cx| {
            spaces.groups[0].rows[0].badge = Some(Badge::Pr(640));
            spaces.groups[0].rows[1].badge = Some(Badge::You("T12".into()));
            cx.notify();
        });
        cx.run_until_parked();
        assert!(cx.debug_bounds("wt-0-0-badge").is_some());
        assert!(cx.debug_bounds("wt-0-1-badge").is_some());
    }
}
