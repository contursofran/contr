//! The spaces model: the repo groups and worktree rows the sidebar shows, built from the herdr tree
//! and the git scan. Pure: no file reads and no GPUI, so the rules are tested with literal values.

use crate::git::{Checkout, RepoInfo, Scan, WorktreeEntry};
use crate::status_icon::Status;
use contr_herdr::{AgentState, HerdrTree, Pane, Tab};
use std::collections::{HashMap, HashSet};
use std::path::PathBuf;

/// A tab that holds a worktree. Its state is the best of its panes in that worktree.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Holder {
    pub tab_id: String,
    pub tab_label: String,
    pub pane_id: String,
    pub status: AgentState,
}

/// A mark on a worktree row: `Pr` is a pull request number, `You` a value that waits on the owner.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Badge {
    Pr(u32),
    You(String),
}

/// One worktree row: a linked worktree, or the main checkout when a tab holds it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorktreeRow {
    pub root: PathBuf,
    pub name: String,
    pub branch: Option<String>,
    pub is_main: bool,
    /// One per tab with a pane whose cwd is in this checkout, in tree order.
    pub holders: Vec<Holder>,
    /// The top holder's state as an icon. `Idle` without a holder.
    pub status: Status,
    /// The top holder's pane. `pane.focus` on it also moves the tab and the workspace.
    pub focus_pane: Option<String>,
    /// The second line: the top holder's tab and state. `None` without a holder.
    pub hold: Option<String>,
    /// `None` until the board bridge supplies one.
    pub badge: Option<Badge>,
}

impl WorktreeRow {
    /// The id that `Selected::Worktree` carries: the canonical root path.
    pub fn id(&self) -> String {
        self.root.to_string_lossy().into_owned()
    }
}

/// One repo in the sidebar, with its worktree rows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepoGroup {
    pub common_dir: PathBuf,
    pub name: String,
    pub branch: Option<String>,
    pub rows: Vec<WorktreeRow>,
}

/// Groups the panes that sit in a checkout of `scan` into repos and worktree rows. Repos come in
/// the order of their first pane in tree order. Panes outside git are left out.
pub fn build(tree: &HerdrTree, scan: &Scan) -> Vec<RepoGroup> {
    let mut holders: HashMap<PathBuf, Vec<Holder>> = HashMap::new();
    let mut repo_order: Vec<PathBuf> = Vec::new();
    for (tab, pane) in panes_in_tree_order(tree) {
        let Some(checkout) = checkout_of(pane, scan) else {
            continue;
        };
        if !repo_order.contains(&checkout.common_dir) {
            repo_order.push(checkout.common_dir.clone());
        }
        add_holder(holders.entry(checkout.root.clone()).or_default(), tab, pane);
    }
    repo_order
        .iter()
        .filter_map(|common_dir| repo_group(scan.repos.get(common_dir)?, &holders))
        .collect()
}

/// The cwd of every pane in the tree, in tree order, each once. Panes without a cwd are skipped.
/// These are the directories the git scan looks at.
pub fn pane_cwds(tree: &HerdrTree) -> Vec<String> {
    let mut seen = HashSet::new();
    panes_in_tree_order(tree)
        .filter_map(|(_, pane)| pane.cwd.clone())
        .filter(|cwd| seen.insert(cwd.clone()))
        .collect()
}

/// The order of agent states for choosing a row's top holder: blocked first, unknown last.
pub fn rank(state: AgentState) -> u8 {
    match state {
        AgentState::Blocked => 4,
        AgentState::Working => 3,
        AgentState::Done => 2,
        AgentState::Idle => 1,
        AgentState::Unknown => 0,
    }
}

/// Every pane with its tab, workspace by workspace, tab by tab.
fn panes_in_tree_order(tree: &HerdrTree) -> impl Iterator<Item = (&Tab, &Pane)> {
    tree.workspaces
        .iter()
        .flat_map(|workspace| workspace.tabs.iter())
        .flat_map(|tab| tab.panes.iter().map(move |pane| (tab, pane)))
}

/// The checkout the pane's cwd is in, if the scan has one.
fn checkout_of<'a>(pane: &Pane, scan: &'a Scan) -> Option<&'a Checkout> {
    scan.checkouts.get(pane.cwd.as_deref()?)
}

/// Records the pane for its tab in this checkout. A pane without an agent counts as `Unknown`.
/// A tab keeps its highest-ranked pane. On a tie the first pane in tree order stays: only a
/// strictly higher rank replaces it.
fn add_holder(holders: &mut Vec<Holder>, tab: &Tab, pane: &Pane) {
    let status = pane
        .agent
        .as_ref()
        .map_or(AgentState::Unknown, |agent| agent.status);
    match holders.iter_mut().find(|holder| holder.tab_id == tab.id) {
        Some(holder) if rank(status) > rank(holder.status) => {
            holder.pane_id = pane.id.clone();
            holder.status = status;
        }
        Some(_) => {}
        None => holders.push(Holder {
            tab_id: tab.id.clone(),
            tab_label: tab.label.clone(),
            pane_id: pane.id.clone(),
            status,
        }),
    }
}

/// The repo's group, or `None` when it has no rows (its checkouts are not in its worktree list).
fn repo_group(repo: &RepoInfo, holders: &HashMap<PathBuf, Vec<Holder>>) -> Option<RepoGroup> {
    let rows: Vec<WorktreeRow> = repo
        .worktrees
        .iter()
        .filter_map(|entry| {
            let held: &[Holder] = holders.get(&entry.root).map_or(&[], Vec::as_slice);
            shows_row(entry, held).then(|| worktree_row(entry, held))
        })
        .collect();
    (!rows.is_empty()).then(|| RepoGroup {
        common_dir: repo.common_dir.clone(),
        name: repo.name.clone(),
        branch: repo.main_branch.clone(),
        rows,
    })
}

/// A held worktree always has a row. An unheld one has a row when `show_unheld` says so.
fn shows_row(entry: &WorktreeEntry, held: &[Holder]) -> bool {
    !held.is_empty() || show_unheld(entry)
}

/// Settled (D48): every non-prunable linked worktree shows, dimmed and idle when no tab holds it.
/// The main checkout shows only when a tab holds it, because the repo row already names it.
fn show_unheld(entry: &WorktreeEntry) -> bool {
    !entry.is_main
}

fn worktree_row(entry: &WorktreeEntry, held: &[Holder]) -> WorktreeRow {
    let top = top_holder(held);
    WorktreeRow {
        root: entry.root.clone(),
        name: entry.name.clone(),
        branch: entry.branch.clone(),
        is_main: entry.is_main,
        holders: held.to_vec(),
        status: top.map_or(Status::Idle, |holder| Status::from(holder.status)),
        focus_pane: top.map(|holder| holder.pane_id.clone()),
        hold: top.map(|holder| hold_text(holder, held.len())),
        badge: None,
    }
}

/// The holder that speaks for the row: the highest rank. On a tie, `max_by_key` returns the last
/// one, which is the newest tab in tree order.
fn top_holder(holders: &[Holder]) -> Option<&Holder> {
    holders.iter().max_by_key(|holder| rank(holder.status))
}

/// Settled (D49): the second line is the herdr fallback, `{tab label} {state}`, plus ` +N` for
/// the N other tabs that hold the worktree (for example "b-3s2 working +1").
fn hold_text(top: &Holder, holder_count: usize) -> String {
    let line = format!("{} {}", top.tab_label, top.status.label());
    match holder_count.saturating_sub(1) {
        0 => line,
        more => format!("{line} +{more}"),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::{RepoGroup, WorktreeRow, build, pane_cwds, rank};
    use crate::git::{Checkout, RepoInfo, Scan, WorktreeEntry};
    use crate::status_icon::Status;
    use contr_herdr::AgentState::{Blocked, Done, Idle, Unknown, Working};
    use contr_herdr::{AgentInfo, AgentState, HerdrTree, Pane, Tab, Workspace};
    use std::collections::BTreeMap;
    use std::path::PathBuf;

    const COMMON: &str = "/work/app/.git";
    const MAIN: &str = "/work/app";
    const W1: &str = "/work/app-wt/w1";
    const W2: &str = "/work/app-wt/w2";

    /// A pane in `cwd`; `state` is its agent's state, `None` for a plain shell.
    fn pane(id: &str, cwd: &str, state: Option<AgentState>) -> Pane {
        Pane {
            id: id.into(),
            label: None,
            cwd: Some(cwd.into()),
            focused: false,
            agent: state.map(|status| AgentInfo {
                name: "claude".into(),
                status,
            }),
            tokens: BTreeMap::new(),
        }
    }

    fn tab(id: &str, label: &str, panes: Vec<Pane>) -> Tab {
        Tab {
            id: id.into(),
            label: label.into(),
            status: Idle,
            focused: false,
            panes,
        }
    }

    fn tree(tabs: Vec<Tab>) -> HerdrTree {
        HerdrTree {
            version: "test".into(),
            focused_pane_id: None,
            workspaces: vec![Workspace {
                id: "w1".into(),
                label: "contr".into(),
                status: Idle,
                focused: true,
                tabs,
            }],
        }
    }

    fn checkout(root: &str, common: &str, is_main: bool) -> Checkout {
        Checkout {
            root: root.into(),
            git_dir: common.into(),
            common_dir: common.into(),
            is_main,
        }
    }

    fn entry(root: &str, name: &str, branch: &str, is_main: bool) -> WorktreeEntry {
        WorktreeEntry {
            root: root.into(),
            name: name.into(),
            branch: Some(branch.into()),
            is_main,
        }
    }

    fn repo(common: &str, name: &str, worktrees: Vec<WorktreeEntry>) -> RepoInfo {
        RepoInfo {
            common_dir: common.into(),
            name: name.into(),
            main_root: worktrees
                .first()
                .map_or_else(PathBuf::new, |w| w.root.clone()),
            main_branch: Some("main".into()),
            worktrees,
        }
    }

    /// Repo `app`: main checkout `MAIN` on `main`, linked `W1` and `W2`. Each root is a cwd.
    fn app_scan() -> Scan {
        let mut scan = Scan::default();
        for (root, is_main) in [(MAIN, true), (W1, false), (W2, false)] {
            scan.checkouts
                .insert(root.into(), checkout(root, COMMON, is_main));
        }
        let worktrees = vec![
            entry(MAIN, "app", "main", true),
            entry(W1, "w1", "agent/6-x", false),
            entry(W2, "w2", "agent/6-y", false),
        ];
        scan.repos
            .insert(COMMON.into(), repo(COMMON, "app", worktrees));
        scan
    }

    /// `app_scan` plus repo `tool`, which has only its main checkout.
    fn with_tool() -> Scan {
        const TOOL: &str = "/work/tool/.git";
        let mut scan = app_scan();
        scan.checkouts
            .insert("/work/tool".into(), checkout("/work/tool", TOOL, true));
        let worktrees = vec![entry("/work/tool", "tool", "main", true)];
        scan.repos
            .insert(TOOL.into(), repo(TOOL, "tool", worktrees));
        scan
    }

    /// Builds the spaces for one tab list over `app_scan`.
    fn built(tabs: Vec<Tab>) -> Vec<RepoGroup> {
        build(&tree(tabs), &app_scan())
    }

    fn row<'a>(groups: &'a [RepoGroup], name: &str) -> &'a WorktreeRow {
        groups
            .iter()
            .flat_map(|group| group.rows.iter())
            .find(|row| row.name == name)
            .unwrap()
    }

    fn names(group: &RepoGroup) -> Vec<&str> {
        group.rows.iter().map(|row| row.name.as_str()).collect()
    }

    #[test]
    fn repos_follow_their_first_pane_in_tree_order() {
        let groups = build(
            &tree(vec![
                tab("t1", "tool", vec![pane("w1:p1", "/work/tool", Some(Idle))]),
                tab("t2", "app", vec![pane("w1:p2", W1, Some(Working))]),
                tab("t3", "tool-2", vec![pane("w1:p3", "/work/tool", None)]),
            ]),
            &with_tool(),
        );
        let order: Vec<&str> = groups.iter().map(|group| group.name.as_str()).collect();
        assert_eq!(order, ["tool", "app"]);
        assert_eq!(groups[0].branch.as_deref(), Some("main"));
        assert_eq!(groups[1].common_dir, PathBuf::from(COMMON));
    }

    #[test]
    fn working_builder_tab_beats_done_planner_tab() {
        let groups = built(vec![
            tab("t1", "p-6", vec![pane("w1:p1", W1, Some(Done))]),
            tab("t2", "b-3s2", vec![pane("w1:p2", W1, Some(Working))]),
        ]);
        let w1 = row(&groups, "w1");
        assert_eq!(w1.status, Status::Working);
        assert_eq!(w1.focus_pane.as_deref(), Some("w1:p2"));
        assert_eq!(w1.hold.as_deref(), Some("b-3s2 working +1"));
        assert_eq!(w1.holders.len(), 2);
    }

    #[test]
    fn equal_states_take_the_newest_tab() {
        let groups = built(vec![
            tab("t1", "old", vec![pane("w1:p1", W1, Some(Working))]),
            tab("t2", "new", vec![pane("w1:p2", W1, Some(Working))]),
        ]);
        let w1 = row(&groups, "w1");
        assert_eq!(w1.focus_pane.as_deref(), Some("w1:p2"));
        assert_eq!(w1.hold.as_deref(), Some("new working +1"));
    }

    #[test]
    fn blocked_beats_working() {
        let groups = built(vec![
            tab("t1", "a", vec![pane("w1:p1", W1, Some(Working))]),
            tab("t2", "b", vec![pane("w1:p2", W1, Some(Blocked))]),
        ]);
        let w1 = row(&groups, "w1");
        assert_eq!(w1.status, Status::Blocked);
        assert_eq!(w1.focus_pane.as_deref(), Some("w1:p2"));
        assert_eq!(w1.hold.as_deref(), Some("b blocked +1"));
    }

    #[test]
    fn pane_without_agent_holds_as_unknown_and_shows_idle() {
        let groups = built(vec![
            tab("t1", "shell", vec![pane("w1:p1", W1, None)]),
            tab("t2", "agent", vec![pane("w1:p2", W2, Some(Unknown))]),
        ]);
        let w1 = row(&groups, "w1");
        assert_eq!(w1.holders[0].status, Unknown);
        assert_eq!(w1.status, Status::Idle);
        assert_eq!(w1.hold.as_deref(), Some("shell unknown"));
        let w2 = row(&groups, "w2");
        assert_eq!(w2.status, Status::Idle);
        assert_eq!(w2.focus_pane.as_deref(), Some("w1:p2"));
    }

    #[test]
    fn a_tab_keeps_its_best_pane_in_each_worktree() {
        let groups = built(vec![tab(
            "t1",
            "multi",
            vec![
                pane("w1:p1", W1, Some(Idle)),
                pane("w1:p2", W1, Some(Working)),
                pane("w1:p3", W1, Some(Idle)),
            ],
        )]);
        let w1 = row(&groups, "w1");
        assert_eq!(w1.holders.len(), 1);
        assert_eq!(w1.holders[0].pane_id, "w1:p2");
    }

    #[test]
    fn one_tab_holds_every_worktree_it_has_panes_in() {
        let groups = built(vec![tab(
            "t1",
            "multi",
            vec![
                pane("w1:p1", W1, Some(Working)),
                pane("w1:p2", W2, Some(Idle)),
            ],
        )]);
        let w1 = row(&groups, "w1");
        assert_eq!(w1.holders[0].tab_id, "t1");
        assert_eq!(w1.focus_pane.as_deref(), Some("w1:p1"));
        let w2 = row(&groups, "w2");
        assert_eq!(w2.holders[0].tab_id, "t1");
        assert_eq!(w2.focus_pane.as_deref(), Some("w1:p2"));
    }

    #[test]
    fn main_checkout_row_only_when_a_tab_holds_it() {
        let unheld = built(vec![tab("t1", "a", vec![pane("w1:p1", W1, Some(Idle))])]);
        assert_eq!(names(&unheld[0]), ["w1", "w2"]);
        let held = built(vec![tab("t1", "a", vec![pane("w1:p1", MAIN, Some(Idle))])]);
        assert_eq!(names(&held[0]), ["app", "w1", "w2"]);
        assert!(held[0].rows[0].is_main);
    }

    #[test]
    fn unheld_linked_worktree_is_idle_with_no_focus_or_hold() {
        let groups = built(vec![tab("t1", "a", vec![pane("w1:p1", W1, Some(Working))])]);
        let w2 = row(&groups, "w2");
        assert_eq!(w2.status, Status::Idle);
        assert_eq!(w2.focus_pane, None);
        assert_eq!(w2.hold, None);
        assert!(w2.holders.is_empty());
        assert_eq!(w2.branch.as_deref(), Some("agent/6-y"));
    }

    #[test]
    fn panes_outside_git_and_without_a_cwd_are_left_out() {
        let mut no_cwd = pane("w1:p2", W1, Some(Working));
        no_cwd.cwd = None;
        let groups = built(vec![tab(
            "t1",
            "scratch",
            vec![pane("w1:p1", "/elsewhere", Some(Working)), no_cwd],
        )]);
        assert!(groups.is_empty());
    }

    #[test]
    fn a_checkout_the_repo_does_not_list_gives_no_group() {
        let mut scan = app_scan();
        let ghost = "/work/ghost/.git";
        scan.checkouts
            .insert("/work/ghost".into(), checkout("/work/ghost", ghost, true));
        scan.repos
            .insert(ghost.into(), repo(ghost, "ghost", Vec::new()));
        let groups = build(
            &tree(vec![tab(
                "t1",
                "a",
                vec![pane("w1:p1", "/work/ghost", Some(Working))],
            )]),
            &scan,
        );
        assert!(groups.is_empty());
    }

    #[test]
    fn a_checkout_whose_repo_is_missing_gives_no_group() {
        let mut scan = app_scan();
        scan.repos.clear();
        let groups = build(
            &tree(vec![tab("t1", "a", vec![pane("w1:p1", W1, Some(Working))])]),
            &scan,
        );
        assert!(groups.is_empty());
    }

    #[test]
    fn rank_puts_blocked_first_and_unknown_last() {
        let ranks = [Blocked, Working, Done, Idle, Unknown].map(rank);
        assert!(ranks.windows(2).all(|pair| pair[0] > pair[1]));
    }

    #[test]
    fn row_id_is_its_root_path() {
        let groups = built(vec![tab("t1", "a", vec![pane("w1:p1", W1, Some(Idle))])]);
        assert_eq!(row(&groups, "w1").id(), W1);
    }

    #[test]
    fn pane_cwds_are_listed_once_in_tree_order_and_skip_missing_cwds() {
        let mut no_cwd = pane("w1:p4", W2, Some(Idle));
        no_cwd.cwd = None;
        let cwds = pane_cwds(&tree(vec![
            tab(
                "t1",
                "a",
                vec![pane("w1:p1", W1, None), pane("w1:p2", MAIN, None)],
            ),
            tab("t2", "b", vec![pane("w1:p3", W1, None), no_cwd]),
        ]));
        assert_eq!(cwds, [W1, MAIN]);
        assert!(pane_cwds(&tree(Vec::new())).is_empty());
    }
}
