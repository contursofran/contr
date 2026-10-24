//! The sidebar's part of the dev state, and its clicks. The sidebar is the spaces column (the repos
//! and their worktree rows) and the agents list under it. The state reads the models that the
//! views draw, and a click calls the path that the row's own handler calls. While the sidebar is
//! closed, no row is on screen: its state is `null` and its clicks are `disabled`.

use crate::dev_protocol::{DevError, code};
use crate::frame::FrameParts;
use crate::spaces::badge_text;
use crate::spaces_model::{Badge, WorktreeRow};
use crate::status_icon::Status;
use contr_herdr::AgentRow;
use gpui::{App, Window};
use serde_json::{Value, json};

/// The `sidebar` section of the state: the repos with their worktree rows, and the agents. `null`
/// while the sidebar is closed.
pub(crate) fn state(parts: &FrameParts, _window: &Window, cx: &App) -> Value {
    if !parts.frame.read(cx).sidebar_open() {
        return Value::Null;
    }
    let spaces = parts.spaces.read(cx);
    let selected_row = spaces.selected_worktree(cx);
    let repos: Vec<Value> = spaces
        .groups
        .iter()
        .map(|group| {
            let rows: Vec<Value> = group
                .rows
                .iter()
                .map(|row| worktree_state(row, selected_row.as_deref()))
                .collect();
            json!({"name": group.name, "branch": group.branch, "rows": rows})
        })
        .collect();
    let agents = parts.agents.read(cx);
    let selected_pane = agents.selected_pane(cx);
    let rows = agents.rows(cx);
    let agent_rows: Vec<Value> = rows
        .iter()
        .map(|row| agent_state(row, selected_pane.as_deref()))
        .collect();
    json!({
        "repos": repos,
        "agents": {"count": rows.len(), "rows": agent_rows},
    })
}

/// One worktree row, with the texts and marks that the row draws.
fn worktree_state(row: &WorktreeRow, selected: Option<&str>) -> Value {
    let id = row.id();
    json!({
        "id": format!("wt:{id}"),
        "name": row.name,
        "branch": row.branch,
        "main": row.is_main,
        "status": row.status.slug(),
        "hold": row.hold,
        "hold_you": row.hold_you,
        "badge": row.badge.as_ref().map(badge_text),
        "badge_you": matches!(row.badge, Some(Badge::You(_))),
        "selected": selected == Some(id.as_str()),
    })
}

/// One agent row, with the texts that the row draws.
fn agent_state(row: &AgentRow, selected: Option<&str>) -> Value {
    json!({
        "id": format!("agent:{}", row.pane_id),
        "name": row.name,
        "detail": row.detail,
        "link": row.link,
        "status": Status::from(row.status).slug(),
        "selected": selected == Some(row.pane_id.as_str()),
    })
}

/// The sidebar's fixed control: the toggle, live while the sidebar is open.
pub(crate) fn controls(parts: &FrameParts, cx: &App) -> Vec<(&'static str, bool)> {
    vec![("sidebar-toggle", parts.frame.read(cx).sidebar_open())]
}

/// A click on a sidebar control: the toggle, a worktree row (`wt:<root>`) or an agent row
/// (`agent:<pane>`). `None` when `id` is none of them.
pub(crate) fn click(
    id: &str,
    parts: &FrameParts,
    _window: &mut Window,
    cx: &mut App,
) -> Option<Result<(), DevError>> {
    if id == "sidebar-toggle" {
        return Some(toggle(parts, cx));
    }
    match id.split_once(':') {
        Some(("wt", root)) => Some(worktree(id, root, parts, cx)),
        Some(("agent", pane_id)) => Some(agent(id, pane_id, parts, cx)),
        _ => None,
    }
}

/// The header toggle. It calls `Frame::toggle_sidebar`, the method that the header's event reaches
/// too. The event itself is delivered only after the window update ends, so the reply would show
/// the old layout if the click emitted it.
fn toggle(parts: &FrameParts, cx: &mut App) -> Result<(), DevError> {
    require_open(parts, cx)?;
    parts.frame.update(cx, |frame, cx| frame.toggle_sidebar(cx));
    Ok(())
}

/// A worktree row's click: `Spaces::activate` with the row's id and its top holder's pane, as the
/// row's click event does.
fn worktree(id: &str, root: &str, parts: &FrameParts, cx: &mut App) -> Result<(), DevError> {
    require_open(parts, cx)?;
    let target = parts
        .spaces
        .read(cx)
        .groups
        .iter()
        .flat_map(|group| &group.rows)
        .find(|row| row.id() == root)
        .map(|row| (row.id(), row.focus_pane.clone()));
    let Some((row_id, focus_pane)) = target else {
        return Err(unknown_row(id));
    };
    parts
        .spaces
        .update(cx, |spaces, cx| spaces.activate(row_id, focus_pane, cx));
    Ok(())
}

/// An agent row's click: `AgentsList::activate` with the agent's pane, as the row's click event
/// does. The row must be listed.
fn agent(id: &str, pane_id: &str, parts: &FrameParts, cx: &mut App) -> Result<(), DevError> {
    require_open(parts, cx)?;
    let listed = parts
        .agents
        .read(cx)
        .rows(cx)
        .iter()
        .any(|row| row.pane_id == pane_id);
    if !listed {
        return Err(unknown_row(id));
    }
    parts
        .agents
        .update(cx, |list, cx| list.activate(pane_id.to_owned(), cx));
    Ok(())
}

/// The sidebar's controls and rows are on screen only while the sidebar is open.
fn require_open(parts: &FrameParts, cx: &App) -> Result<(), DevError> {
    if parts.frame.read(cx).sidebar_open() {
        Ok(())
    } else {
        Err(DevError::new(code::DISABLED, "the sidebar is closed"))
    }
}

/// No row of the open sidebar has the id.
fn unknown_row(id: &str) -> DevError {
    DevError::new(code::UNKNOWN_ID, format!("no sidebar row has the id {id}"))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::agents_list::FocusFn;
    use crate::board::GlobalBoard;
    use crate::board_panel::test_support::{bridge, fixture};
    use crate::dev_control::dispatch;
    use crate::dev_protocol::{Reply, Request};
    use crate::frame::Frame;
    use crate::git::fake::{linked, main_repo};
    use crate::selection::Selection;
    use crate::status_bar;
    use crate::title_bar::tests::herdr_in;
    use crate::visibility::Visibility;
    use contr_herdr::{AgentState, ConnState, HerdrTree, LiveHerdr, Pane, Tab, Workspace};
    use gpui::{Entity, TestAppContext, VisualTestContext, px, size};
    use std::cell::RefCell;
    use std::fs;
    use std::path::{Path, PathBuf};
    use std::rc::Rc;
    use std::sync::Arc;

    /// The pane ids that the focus call was given, in order.
    type Focused = Rc<RefCell<Vec<String>>>;

    /// The frame with two worktrees of one repo, both held by tab `t1`: `w1:p2` runs a working
    /// agent in `wt1`, and `w1:p4` a done agent in `wt2`. The board fixture gives `w1:p2` the open
    /// item `T12` and `w1:p4` the pull request `#640`. The temp dir lives as long as this value.
    struct Shown<'a> {
        focused: Focused,
        cx: &'a mut VisualTestContext,
        wt1: String,
        wt2: String,
        _dir: tempfile::TempDir,
    }

    fn connected(tree: Option<HerdrTree>) -> LiveHerdr {
        LiveHerdr {
            state: ConnState::Connected {
                server_version: "0.9.3".into(),
            },
            tree: tree.map(Arc::new),
        }
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
                    label: "b-1".into(),
                    status: AgentState::Idle,
                    focused: true,
                    panes,
                }],
            }],
        }
    }

    /// A repo at `base/repo` with two linked worktrees: `base/wt1` on `feature` and `base/wt2` on
    /// `other`. Returns their roots.
    fn two_worktrees(base: &Path) -> (PathBuf, PathBuf) {
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        let wt1 = base.join("wt1");
        linked(&git, "w1", &wt1, "ref: refs/heads/feature\n");
        let wt2 = base.join("wt2");
        linked(&git, "w2", &wt2, "ref: refs/heads/other\n");
        (wt1, wt2)
    }

    /// Opens the frame in a window of the reference size, with herdr holding `live` and the board
    /// empty. The focus call records the pane ids it is given.
    fn frame_with(
        cx: &mut TestAppContext,
        live: LiveHerdr,
    ) -> (Entity<Frame>, Focused, &mut VisualTestContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, live);
        cx.update(crate::board::init_detached);
        cx.update(|app| status_bar::init_detached(app, None));
        let focused: Focused = Rc::new(RefCell::new(Vec::new()));
        let log = focused.clone();
        let focus: FocusFn =
            Rc::new(move |pane_id: String, _: &mut App| log.borrow_mut().push(pane_id));
        let (frame, cx) =
            cx.add_window_view(move |window, cx| Frame::with_focus(window, cx, focus));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        (frame, focused, cx)
    }

    /// The frame with the two held worktrees, and the board fixture on the board.
    fn shown(cx: &mut TestAppContext) -> Shown<'_> {
        let dir = tempfile::tempdir().unwrap();
        let base = fs::canonicalize(dir.path()).unwrap();
        let (wt1, wt2) = two_worktrees(&base);
        let panes = vec![
            pane("w1:p2", &wt1.to_string_lossy(), Some(AgentState::Working)),
            pane("w1:p4", &wt2.to_string_lossy(), Some(AgentState::Done)),
        ];
        let (_frame, focused, cx) = frame_with(cx, connected(Some(tree(panes))));
        cx.update(|_, app| {
            let board = app.global::<GlobalBoard>().0.clone();
            board.update(app, |board, cx| {
                board.set(Some(fixture()), None, Some(bridge()), cx)
            });
        });
        cx.run_until_parked();
        Shown {
            focused,
            cx,
            wt1: wt1.to_string_lossy().into_owned(),
            wt2: wt2.to_string_lossy().into_owned(),
            _dir: dir,
        }
    }

    fn send(cx: &mut VisualTestContext, request: Request) -> Reply {
        cx.update(|window, app| dispatch::handle(request, window, app))
    }

    fn click_id(cx: &mut VisualTestContext, id: &str) -> Reply {
        send(cx, Request::Click { id: id.into() })
    }

    fn ok(reply: Reply) -> Value {
        match reply {
            Reply::Ok(result) => result,
            Reply::Err(error) => panic!("expected a result, got {}: {}", error.code, error.message),
        }
    }

    fn err(reply: Reply) -> DevError {
        match reply {
            Reply::Err(error) => error,
            Reply::Ok(result) => panic!("expected an error, got {result}"),
        }
    }

    fn state_of(cx: &mut VisualTestContext) -> Value {
        ok(send(cx, Request::State))
    }

    /// The worktree row with `id`, in the repo groups of a sidebar state.
    fn repo_row<'a>(sidebar: &'a Value, id: &str) -> &'a Value {
        sidebar["repos"]
            .as_array()
            .unwrap()
            .iter()
            .flat_map(|repo| repo["rows"].as_array().unwrap())
            .find(|row| row["id"] == id)
            .unwrap_or_else(|| panic!("no repo row {id}"))
    }

    /// The agent row with `id`, in a sidebar state.
    fn agent_row<'a>(sidebar: &'a Value, id: &str) -> &'a Value {
        sidebar["agents"]["rows"]
            .as_array()
            .unwrap()
            .iter()
            .find(|row| row["id"] == id)
            .unwrap_or_else(|| panic!("no agent row {id}"))
    }

    /// The live flag of the control `id` in a state.
    fn live_of(state: &Value, id: &str) -> Value {
        state["controls"]
            .as_array()
            .unwrap()
            .iter()
            .find(|control| control["id"] == id)
            .unwrap()["live"]
            .clone()
    }

    #[gpui::test]
    fn the_state_lists_each_worktree_row_with_its_id_and_marks(cx: &mut TestAppContext) {
        let shown = shown(cx);
        let sidebar = state_of(shown.cx)["sidebar"].clone();
        let repos = sidebar["repos"].as_array().unwrap();
        assert_eq!(repos.len(), 1);
        assert_eq!(repos[0]["name"], "repo");
        assert_eq!(repos[0]["branch"], "main");

        let first = repo_row(&sidebar, &format!("wt:{}", shown.wt1));
        assert_eq!(first["name"], "wt1");
        assert_eq!(first["branch"], "feature");
        assert_eq!(first["main"], false);
        assert_eq!(first["status"], "working");
        assert!(first["hold"].is_string(), "a held row shows its hold line");
        // The open item T12 of the board is the badge, and the owner's turn.
        assert_eq!(first["badge"], "T12");
        assert_eq!(first["badge_you"], true);
        assert_eq!(first["selected"], false);

        let second = repo_row(&sidebar, &format!("wt:{}", shown.wt2));
        assert_eq!(second["name"], "wt2");
        // The pull request #640 is the badge, and it is not the owner's turn.
        assert_eq!(second["badge"], "#640");
        assert_eq!(second["badge_you"], false);
    }

    #[gpui::test]
    fn the_state_lists_each_agent_row_with_its_id(cx: &mut TestAppContext) {
        let shown = shown(cx);
        let sidebar = state_of(shown.cx)["sidebar"].clone();
        assert_eq!(sidebar["agents"]["count"], 2);
        let working = agent_row(&sidebar, "agent:w1:p2");
        assert_eq!(working["status"], "working");
        assert_eq!(working["selected"], false);
        assert_eq!(agent_row(&sidebar, "agent:w1:p4")["status"], "done");
    }

    #[gpui::test]
    fn a_click_on_a_worktree_row_selects_it_and_focuses_its_pane(cx: &mut TestAppContext) {
        let shown = shown(cx);
        let id = format!("wt:{}", shown.wt1);
        let state = ok(click_id(shown.cx, &id));
        assert_eq!(
            state["selection"],
            json!({"kind": "worktree", "id": shown.wt1})
        );
        assert_eq!(*shown.focused.borrow(), vec!["w1:p2".to_string()]);
        assert_eq!(repo_row(&state["sidebar"], &id)["selected"], true);
    }

    #[gpui::test]
    fn select_takes_a_worktree_row_as_a_click_does(cx: &mut TestAppContext) {
        let shown = shown(cx);
        let state = ok(send(
            shown.cx,
            Request::Select {
                row: format!("wt:{}", shown.wt2),
            },
        ));
        assert_eq!(
            state["selection"],
            json!({"kind": "worktree", "id": shown.wt2})
        );
        assert_eq!(*shown.focused.borrow(), vec!["w1:p4".to_string()]);
    }

    #[gpui::test]
    fn a_click_on_an_agent_row_selects_it_and_focuses_its_pane(cx: &mut TestAppContext) {
        let shown = shown(cx);
        let state = ok(click_id(shown.cx, "agent:w1:p4"));
        assert_eq!(state["selection"], json!({"kind": "agent", "id": "w1:p4"}));
        assert_eq!(*shown.focused.borrow(), vec!["w1:p4".to_string()]);
        assert_eq!(
            agent_row(&state["sidebar"], "agent:w1:p4")["selected"],
            true
        );
    }

    #[gpui::test]
    fn an_id_that_no_row_has_is_unknown_and_selects_nothing(cx: &mut TestAppContext) {
        let shown = shown(cx);
        assert_eq!(err(click_id(shown.cx, "wt:/nope")).code, code::UNKNOWN_ID);
        assert_eq!(
            err(click_id(shown.cx, "agent:w1:p9")).code,
            code::UNKNOWN_ID
        );
        assert_eq!(state_of(shown.cx)["selection"], Value::Null);
        assert!(shown.focused.borrow().is_empty());
    }

    #[gpui::test]
    fn the_sidebar_toggle_hides_the_sidebar_and_the_reopen_shows_it(cx: &mut TestAppContext) {
        let shown = shown(cx);
        let state = ok(click_id(shown.cx, "sidebar-toggle"));
        assert_eq!(state["layout"]["sidebar_open"], false);
        assert_eq!(state["sidebar"], Value::Null);
        assert_eq!(live_of(&state, "sidebar-toggle"), json!(false));

        let state = ok(click_id(shown.cx, "sidebar-reopen"));
        assert_eq!(state["layout"]["sidebar_open"], true);
        assert!(state["sidebar"].is_object());
        assert_eq!(live_of(&state, "sidebar-toggle"), json!(true));
    }

    #[gpui::test]
    fn the_sidebar_clicks_are_disabled_while_the_sidebar_is_closed(cx: &mut TestAppContext) {
        let shown = shown(cx);
        ok(click_id(shown.cx, "sidebar-toggle"));
        let ids = [
            "sidebar-toggle".to_string(),
            format!("wt:{}", shown.wt1),
            "agent:w1:p2".to_string(),
        ];
        for id in ids {
            assert_eq!(err(click_id(shown.cx, &id)).code, code::DISABLED, "{id}");
        }
        // The refused rows were not selected or focused.
        assert_eq!(state_of(shown.cx)["selection"], Value::Null);
        assert!(shown.focused.borrow().is_empty());
    }

    #[gpui::test]
    fn without_a_tree_the_sidebar_has_no_rows_and_an_agent_click_is_unknown(
        cx: &mut TestAppContext,
    ) {
        let (_frame, _focused, cx) = frame_with(cx, connected(None));
        let sidebar = state_of(cx)["sidebar"].clone();
        assert_eq!(sidebar["repos"], json!([]));
        assert_eq!(sidebar["agents"], json!({"count": 0, "rows": []}));
        assert_eq!(err(click_id(cx, "agent:w1:p2")).code, code::UNKNOWN_ID);
    }
}
