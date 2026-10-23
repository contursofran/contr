//! The sidebar's spaces column: the header (window-button inset, toggle, wordmark) and the body
//! that scrolls. The state keeps the repo groups in step with herdr and git; rows fill the body in
//! a later slice.

use crate::git::{self, Scan};
use crate::herdr::{GlobalHerdr, Herdr};
use crate::selection::{GlobalSelection, Selected, Selection};
use crate::spaces_model::{self, RepoGroup};
use crate::theme::{
    SIDEBAR_TOGGLE_BOX, SIDEBAR_TOGGLE_FILL, SIDEBAR_TOGGLE_HEIGHT, SIDEBAR_TOGGLE_RADIUS,
    SIDEBAR_TOGGLE_WIDTH, TEXT_2, TEXT_DIM, TITLE_BAR_HEIGHT, TRAFFIC_LIGHT_INSET,
};
use crate::visibility::GlobalVisibility;
use async_channel::Receiver;
use gpui::{
    App, AsyncApp, Context, Div, Entity, EventEmitter, FontWeight, IntoElement, MouseButton,
    ParentElement, Render, Stateful, Styled, Subscription, Task, WeakEntity, Window, div,
    prelude::*, px, rgb,
};
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
    /// The repo groups the body shows. Empty while herdr has no tree.
    pub groups: Vec<RepoGroup>,
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
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let selection = cx.global::<GlobalSelection>().0.clone();
        let visibility = cx.global::<GlobalVisibility>().0.clone();
        let herdr_sub = cx.observe(&herdr, |this, _, cx| this.on_herdr_changed(cx));
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
            groups: Vec::new(),
            scan: Scan::default(),
            scanned_cwds: Vec::new(),
            last_scan: None,
            scanning: None,
            #[cfg(test)]
            scans: 0,
            _subs: vec![herdr_sub, visibility_sub],
            _tick: tick,
        };
        spaces.on_herdr_changed(cx);
        spaces
    }

    /// herdr's state changed. Rescans when a pane sits in another directory or the last scan is
    /// older than `RESCAN_EVERY`, then rebuilds the groups from the tree and the scan we have.
    fn on_herdr_changed(&mut self, cx: &mut Context<Self>) {
        let cwds = self.tree_cwds(cx);
        let stale = self.last_scan.is_none_or(|at| at.elapsed() >= RESCAN_EVERY);
        if cwds != self.scanned_cwds || stale {
            self.start_scan(cwds, cx);
        }
        self.rebuild(cx);
    }

    /// The timer's rescan: the same cwds again, because a branch may have changed since.
    fn rescan(&mut self, cx: &mut Context<Self>) {
        let cwds = self.scanned_cwds.clone();
        self.start_scan(cwds, cx);
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
        self.last_scan = Some(Instant::now());
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

    /// Rebuilds `groups` from the herdr tree and the scan. Notifies only when they changed. Then
    /// clears a selected worktree that no row has any more.
    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let groups = self
            .herdr
            .read(cx)
            .live
            .tree
            .as_deref()
            .map(|tree| spaces_model::build(tree, &self.scan))
            .unwrap_or_default();
        if groups != self.groups {
            self.groups = groups;
            cx.notify();
        }
        self.clear_gone_selection(cx);
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
        self.selection.update(cx, |selection, cx| {
            selection.current = None;
            cx.notify();
        });
    }
}

/// The rescan timer. Shown: one rescan every `RESCAN_EVERY`. Hidden: it waits for the next
/// visibility change and starts no timer, as the host sampler does.
async fn tick(
    this: WeakEntity<Spaces>,
    cx: &mut AsyncApp,
    changes: Receiver<bool>,
    mut visible: bool,
) {
    loop {
        while !visible {
            match changes.recv().await {
                Ok(now_visible) => visible = now_visible,
                Err(_) => return, // the sidebar was dropped
            }
        }
        cx.background_executor().timer(RESCAN_EVERY).await;
        // Visibility changes that arrived during the wait; the last one wins.
        while let Ok(now_visible) = changes.try_recv() {
            visible = now_visible;
        }
        if visible && this.update(cx, |spaces, cx| spaces.rescan(cx)).is_err() {
            return; // the sidebar was dropped
        }
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
}
