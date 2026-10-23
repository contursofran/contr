use std::collections::HashMap;

use herdr_client::protocol::{
    AgentStatus, ClientShellAgent, ClientShellPane, ClientShellSnapshot, ClientShellTab,
};

/// The state of one agent: herdr's five values, one to one.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AgentState {
    Idle,
    Working,
    Blocked,
    Done,
    Unknown,
}

impl AgentState {
    /// The lower-case word herdr uses for this state.
    pub fn label(self) -> &'static str {
        match self {
            Self::Idle => "idle",
            Self::Working => "working",
            Self::Blocked => "blocked",
            Self::Done => "done",
            Self::Unknown => "unknown",
        }
    }
}

impl From<AgentStatus> for AgentState {
    fn from(status: AgentStatus) -> Self {
        match status {
            AgentStatus::Idle => Self::Idle,
            AgentStatus::Working => Self::Working,
            AgentStatus::Blocked => Self::Blocked,
            AgentStatus::Done => Self::Done,
            AgentStatus::Unknown => Self::Unknown,
        }
    }
}

/// The herdr tree at one snapshot revision.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HerdrTree {
    pub boot_id: String,
    pub revision: u64,
    pub focused_pane_id: Option<String>,
    pub workspaces: Vec<Workspace>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Workspace {
    pub id: String,
    pub label: String,
    pub branch: Option<String>,
    pub status: AgentState,
    pub focused: bool,
    pub tabs: Vec<Tab>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Tab {
    pub id: String,
    pub label: String,
    pub status: AgentState,
    pub focused: bool,
    pub panes: Vec<Pane>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Pane {
    pub id: String,
    pub label: Option<String>,
    pub cwd: Option<String>,
    pub focused: bool,
    /// `None` when no agent runs in this pane.
    pub agent: Option<AgentInfo>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentInfo {
    /// `display_agent` when herdr sends it, else `agent`.
    pub name: Option<String>,
    pub status: AgentState,
    pub state_change_seq: u64,
}

/// An agent state that changed in one pane between two trees.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatusChange {
    pub pane_id: String,
    pub agent: Option<String>,
    /// `None` when the pane had no agent before.
    pub from: Option<AgentState>,
    /// `None` when the pane has no agent now.
    pub to: Option<AgentState>,
}

impl HerdrTree {
    /// Groups the flat snapshot into the tree, in the snapshot's order. Tabs
    /// whose workspace is missing and panes whose tab is missing are dropped.
    /// Agents join their pane by `pane_id`.
    pub fn from_snapshot(snapshot: &ClientShellSnapshot) -> Self {
        let agents: HashMap<&str, &ClientShellAgent> = snapshot
            .agents
            .iter()
            .map(|agent| (agent.pane_id.as_str(), agent))
            .collect();
        let mut tabs_by_workspace: HashMap<&str, Vec<&ClientShellTab>> = HashMap::new();
        for tab in &snapshot.tabs {
            tabs_by_workspace
                .entry(tab.workspace_id.as_str())
                .or_default()
                .push(tab);
        }
        let mut panes_by_tab: HashMap<&str, Vec<&ClientShellPane>> = HashMap::new();
        for pane in &snapshot.panes {
            panes_by_tab
                .entry(pane.tab_id.as_str())
                .or_default()
                .push(pane);
        }

        let workspaces = snapshot
            .workspaces
            .iter()
            .map(|ws| Workspace {
                id: ws.workspace_id.clone(),
                label: ws.label.clone(),
                branch: ws.branch.clone(),
                status: AgentState::from(ws.agent_status),
                focused: ws.focused,
                tabs: tabs_by_workspace
                    .get(ws.workspace_id.as_str())
                    .into_iter()
                    .flatten()
                    .map(|tab| Tab {
                        id: tab.tab_id.clone(),
                        label: tab.label.clone(),
                        status: AgentState::from(tab.agent_status),
                        focused: tab.focused,
                        panes: panes_by_tab
                            .get(tab.tab_id.as_str())
                            .into_iter()
                            .flatten()
                            .map(|pane| Pane {
                                id: pane.pane_id.clone(),
                                label: pane.label.clone(),
                                cwd: pane.cwd.clone(),
                                focused: pane.focused,
                                agent: agents
                                    .get(pane.pane_id.as_str())
                                    .map(|agent| agent_info(agent)),
                            })
                            .collect(),
                    })
                    .collect(),
            })
            .collect();

        Self {
            boot_id: snapshot.boot_id.clone(),
            revision: snapshot.revision,
            focused_pane_id: snapshot.focused_pane_id.clone(),
            workspaces,
        }
    }

    pub fn tab_count(&self) -> usize {
        self.workspaces.iter().map(|ws| ws.tabs.len()).sum()
    }

    pub fn pane_count(&self) -> usize {
        self.panes().count()
    }

    /// True when both trees have the same workspaces, tabs and panes, with the
    /// same ids and labels in the same order. Status and focus are ignored.
    pub fn same_shape(&self, other: &Self) -> bool {
        self.shape() == other.shape()
    }

    /// Every pane present in both trees whose agent state differs, in `next`'s
    /// order. A pane that is new or gone is not reported; see `same_shape`.
    pub fn status_changes(&self, next: &Self) -> Vec<StatusChange> {
        let before: HashMap<&str, &Pane> =
            self.panes().map(|pane| (pane.id.as_str(), pane)).collect();
        next.panes()
            .filter_map(|pane| {
                let prev = before.get(pane.id.as_str())?;
                let from = prev.agent.as_ref().map(|agent| agent.status);
                let to = pane.agent.as_ref().map(|agent| agent.status);
                (from != to).then(|| StatusChange {
                    pane_id: pane.id.clone(),
                    agent: pane
                        .agent
                        .as_ref()
                        .or(prev.agent.as_ref())
                        .and_then(|agent| agent.name.clone()),
                    from,
                    to,
                })
            })
            .collect()
    }

    /// One indented line per workspace, tab and pane, for the log. The header
    /// line with the revision and counts is the caller's.
    pub fn log_lines(&self) -> Vec<String> {
        let mut lines = Vec::new();
        for ws in &self.workspaces {
            lines.push(format!(
                "  ws {} {:?} {}",
                ws.id,
                ws.label,
                ws.status.label()
            ));
            for tab in &ws.tabs {
                lines.push(format!(
                    "    tab {} {:?} {}",
                    tab.id,
                    tab.label,
                    tab.status.label()
                ));
                for pane in &tab.panes {
                    lines.push(pane_log_line(pane));
                }
            }
        }
        lines
    }

    fn panes(&self) -> impl Iterator<Item = &Pane> {
        self.workspaces
            .iter()
            .flat_map(|ws| &ws.tabs)
            .flat_map(|tab| &tab.panes)
    }

    /// Ids, labels and depth of every node in tree order. Depth keeps the
    /// nesting, so two trees with equal shapes have the same parents too.
    fn shape(&self) -> Vec<(u8, &str, Option<&str>)> {
        let mut shape = Vec::new();
        for ws in &self.workspaces {
            shape.push((0, ws.id.as_str(), Some(ws.label.as_str())));
            for tab in &ws.tabs {
                shape.push((1, tab.id.as_str(), Some(tab.label.as_str())));
                for pane in &tab.panes {
                    shape.push((2, pane.id.as_str(), pane.label.as_deref()));
                }
            }
        }
        shape
    }
}

impl StatusChange {
    /// `herdr status pane=<id> agent=<name> <from> -> <to>`, with `-` for a
    /// missing name or state.
    pub(crate) fn log_line(&self) -> String {
        format!(
            "herdr status pane={} agent={} {} -> {}",
            self.pane_id,
            self.agent.as_deref().unwrap_or("-"),
            self.from.map_or("-", AgentState::label),
            self.to.map_or("-", AgentState::label),
        )
    }
}

fn agent_info(agent: &ClientShellAgent) -> AgentInfo {
    AgentInfo {
        name: agent.display_agent.clone().or_else(|| agent.agent.clone()),
        status: AgentState::from(agent.agent_status),
        state_change_seq: agent.state_change_seq,
    }
}

fn pane_log_line(pane: &Pane) -> String {
    let (name, status) = match &pane.agent {
        Some(agent) => (agent.name.as_deref().unwrap_or("-"), agent.status.label()),
        None => ("-", "-"),
    };
    let focus = if pane.focused { " focused" } else { "" };
    format!("      pane {} {name} {status}{focus}", pane.id)
}

/// A snapshot with every field empty, for tests that fill in what they need.
#[cfg(test)]
pub(crate) fn empty_snapshot() -> ClientShellSnapshot {
    ClientShellSnapshot {
        boot_id: String::new(),
        revision: 0,
        config_diagnostic: None,
        product_announcement: None,
        update_available: None,
        update_install_command: String::new(),
        server_keybindings_toml: None,
        latest_release_notes_available: false,
        integration_updates_available: false,
        worktree_directory: String::new(),
        release_notes: None,
        focused_workspace_id: None,
        focused_tab_id: None,
        focused_pane_id: None,
        tab_bar_right: vec![],
        tab_bar_right_separator: String::new(),
        agent_view_label: None,
        agent_order: vec![],
        workspaces: vec![],
        tabs: vec![],
        panes: vec![],
        agents: vec![],
        commands: vec![],
    }
}

#[cfg(test)]
mod tests {
    use herdr_client::protocol::ClientShellWorkspace;

    use super::*;

    fn workspace(id: &str, label: &str, status: AgentStatus) -> ClientShellWorkspace {
        ClientShellWorkspace {
            workspace_id: id.into(),
            active_tab_id: String::new(),
            new_workspace_cwd: String::new(),
            number: 1,
            label: label.into(),
            custom_label: false,
            branch: None,
            git_ahead_behind: None,
            tokens: vec![],
            worktree: None,
            focused: false,
            agent_status: status,
        }
    }

    fn tab(id: &str, workspace_id: &str, label: &str, status: AgentStatus) -> ClientShellTab {
        ClientShellTab {
            tab_id: id.into(),
            workspace_id: workspace_id.into(),
            number: 1,
            label: label.into(),
            custom_label: false,
            zoomed: false,
            focused: false,
            agent_status: status,
        }
    }

    fn pane(id: &str, tab_id: &str, workspace_id: &str, focused: bool) -> ClientShellPane {
        ClientShellPane {
            pane_id: id.into(),
            workspace_id: workspace_id.into(),
            tab_id: tab_id.into(),
            label: None,
            cwd: None,
            foreground_cwd: None,
            focused,
            right_click_passthrough: false,
        }
    }

    fn agent(
        pane_id: &str,
        tab_id: &str,
        workspace_id: &str,
        name: Option<&str>,
        status: AgentStatus,
    ) -> ClientShellAgent {
        ClientShellAgent {
            pane_id: pane_id.into(),
            workspace_id: workspace_id.into(),
            tab_id: tab_id.into(),
            name: None,
            display_agent: None,
            agent: name.map(String::from),
            title: None,
            terminal_title: None,
            terminal_title_stripped: None,
            agent_status: status,
            state_change_seq: 7,
            state_labels: vec![],
            tokens: vec![],
            focused: false,
        }
    }

    /// One workspace `w1`, one tab `t1`, one pane `p1` running `claude`.
    fn one_agent(status: AgentStatus) -> ClientShellSnapshot {
        let mut s = empty_snapshot();
        s.workspaces.push(workspace("w1", "contr", status));
        s.tabs.push(tab("t1", "w1", "main", status));
        s.panes.push(pane("p1", "t1", "w1", false));
        s.agents
            .push(agent("p1", "t1", "w1", Some("claude"), status));
        s
    }

    #[test]
    fn groups_tabs_and_panes_under_their_parents_in_order() {
        let mut s = empty_snapshot();
        s.boot_id = "boot".into();
        s.revision = 5;
        s.focused_pane_id = Some("p3".into());
        s.workspaces.push(workspace("w1", "one", AgentStatus::Idle));
        s.workspaces
            .push(workspace("w2", "two", AgentStatus::Working));
        s.tabs.push(tab("t1", "w1", "a", AgentStatus::Idle));
        s.tabs.push(tab("t2", "w2", "b", AgentStatus::Working));
        s.tabs.push(tab("t3", "w1", "c", AgentStatus::Done));
        s.panes.push(pane("p1", "t1", "w1", false));
        s.panes.push(pane("p2", "t1", "w1", false));
        s.panes.push(pane("p3", "t2", "w2", true));
        s.panes.push(pane("p4", "t3", "w1", false));

        let tree = HerdrTree::from_snapshot(&s);

        assert_eq!(tree.boot_id, "boot");
        assert_eq!(tree.revision, 5);
        assert_eq!(tree.focused_pane_id.as_deref(), Some("p3"));
        let ws: Vec<&str> = tree.workspaces.iter().map(|w| w.id.as_str()).collect();
        assert_eq!(ws, ["w1", "w2"]);
        let w1_tabs: Vec<&str> = tree.workspaces[0]
            .tabs
            .iter()
            .map(|t| t.id.as_str())
            .collect();
        assert_eq!(w1_tabs, ["t1", "t3"]);
        let t1_panes: Vec<&str> = tree.workspaces[0].tabs[0]
            .panes
            .iter()
            .map(|p| p.id.as_str())
            .collect();
        assert_eq!(t1_panes, ["p1", "p2"]);
        assert_eq!(tree.workspaces[0].tabs[1].panes[0].id, "p4");
        assert_eq!(tree.workspaces[1].tabs[0].panes[0].id, "p3");
        assert!(tree.workspaces[1].tabs[0].panes[0].focused);
        assert_eq!(tree.tab_count(), 3);
        assert_eq!(tree.pane_count(), 4);
    }

    #[test]
    fn drops_tabs_and_panes_whose_parent_is_missing() {
        let mut s = one_agent(AgentStatus::Idle);
        s.tabs.push(tab("t9", "gone", "orphan", AgentStatus::Idle));
        s.panes.push(pane("p9", "gone-tab", "w1", false));
        s.panes.push(pane("p8", "t9", "gone", false));

        let tree = HerdrTree::from_snapshot(&s);

        assert_eq!(tree.tab_count(), 1);
        assert_eq!(tree.pane_count(), 1);
        assert_eq!(tree.workspaces[0].tabs[0].panes[0].id, "p1");
    }

    #[test]
    fn joins_agent_by_pane_id_and_prefers_display_agent() {
        let mut s = empty_snapshot();
        s.workspaces
            .push(workspace("w1", "contr", AgentStatus::Idle));
        s.tabs.push(tab("t1", "w1", "main", AgentStatus::Idle));
        s.panes.push(pane("p1", "t1", "w1", false));
        s.panes.push(pane("p2", "t1", "w1", false));
        s.panes.push(pane("p3", "t1", "w1", false));
        let mut named = agent("p1", "t1", "w1", Some("claude"), AgentStatus::Working);
        named.display_agent = Some("Claude Code".into());
        s.agents.push(named);
        s.agents
            .push(agent("p2", "t1", "w1", Some("codex"), AgentStatus::Blocked));
        s.agents
            .push(agent("p9", "t1", "w1", Some("ghost"), AgentStatus::Idle));

        let tree = HerdrTree::from_snapshot(&s);
        let panes = &tree.workspaces[0].tabs[0].panes;

        let first = panes[0].agent.as_ref();
        assert_eq!(first.and_then(|a| a.name.as_deref()), Some("Claude Code"));
        assert_eq!(first.map(|a| a.status), Some(AgentState::Working));
        assert_eq!(first.map(|a| a.state_change_seq), Some(7));
        let second = panes[1].agent.as_ref();
        assert_eq!(second.and_then(|a| a.name.as_deref()), Some("codex"));
        assert_eq!(second.map(|a| a.status), Some(AgentState::Blocked));
        assert_eq!(panes[2].agent, None);
        assert_eq!(tree.pane_count(), 3);
    }

    #[test]
    fn maps_each_herdr_status_one_to_one() {
        let pairs = [
            (AgentStatus::Idle, AgentState::Idle, "idle"),
            (AgentStatus::Working, AgentState::Working, "working"),
            (AgentStatus::Blocked, AgentState::Blocked, "blocked"),
            (AgentStatus::Done, AgentState::Done, "done"),
            (AgentStatus::Unknown, AgentState::Unknown, "unknown"),
        ];
        for (status, expected, word) in pairs {
            assert_eq!(AgentState::from(status), expected);
            assert_eq!(expected.label(), word);
            let tree = HerdrTree::from_snapshot(&one_agent(status));
            assert_eq!(tree.workspaces[0].status, expected);
        }
    }

    #[test]
    fn status_changes_reports_only_the_pane_that_changed() {
        let mut before = empty_snapshot();
        before
            .workspaces
            .push(workspace("w1", "contr", AgentStatus::Working));
        before
            .tabs
            .push(tab("t1", "w1", "main", AgentStatus::Working));
        before.panes.push(pane("p1", "t1", "w1", false));
        before.panes.push(pane("p2", "t1", "w1", false));
        before
            .agents
            .push(agent("p1", "t1", "w1", Some("claude"), AgentStatus::Idle));
        before
            .agents
            .push(agent("p2", "t1", "w1", Some("codex"), AgentStatus::Working));
        let mut after = before.clone();
        after.agents[1].agent_status = AgentStatus::Blocked;

        let changes =
            HerdrTree::from_snapshot(&before).status_changes(&HerdrTree::from_snapshot(&after));

        assert_eq!(
            changes,
            [StatusChange {
                pane_id: "p2".into(),
                agent: Some("codex".into()),
                from: Some(AgentState::Working),
                to: Some(AgentState::Blocked),
            }]
        );
    }

    #[test]
    fn an_agent_appearing_is_a_change_from_none() {
        let mut before = one_agent(AgentStatus::Idle);
        before.agents.clear();
        let after = one_agent(AgentStatus::Idle);

        let changes =
            HerdrTree::from_snapshot(&before).status_changes(&HerdrTree::from_snapshot(&after));

        assert_eq!(changes.len(), 1);
        assert_eq!(changes[0].pane_id, "p1");
        assert_eq!(changes[0].from, None);
        assert_eq!(changes[0].to, Some(AgentState::Idle));
    }

    #[test]
    fn a_new_pane_is_not_a_status_change() {
        let before = one_agent(AgentStatus::Working);
        let mut after = one_agent(AgentStatus::Working);
        after.panes.push(pane("p2", "t1", "w1", false));

        let changes =
            HerdrTree::from_snapshot(&before).status_changes(&HerdrTree::from_snapshot(&after));

        assert!(changes.is_empty());
    }

    #[test]
    fn same_shape_ignores_status_and_focus() {
        let before = one_agent(AgentStatus::Idle);
        let mut after = one_agent(AgentStatus::Working);
        after.panes[0].focused = true;

        assert!(HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn same_shape_is_false_when_a_tab_is_added() {
        let before = one_agent(AgentStatus::Idle);
        let mut after = one_agent(AgentStatus::Idle);
        after
            .tabs
            .push(tab("t2", "w1", "second", AgentStatus::Idle));

        assert!(!HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn same_shape_is_false_when_a_label_changes() {
        let before = one_agent(AgentStatus::Idle);
        let mut after = one_agent(AgentStatus::Idle);
        after.tabs[0].label = "renamed".into();

        assert!(!HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn log_lines_match_the_documented_format() {
        let mut s = empty_snapshot();
        s.workspaces
            .push(workspace("w2", "contr", AgentStatus::Working));
        s.tabs
            .push(tab("w2:t9Y", "w2", "planner", AgentStatus::Working));
        s.panes.push(pane("w2:p90", "w2:t9Y", "w2", true));
        s.panes.push(pane("w2:p91", "w2:t9Y", "w2", false));
        s.agents.push(agent(
            "w2:p90",
            "w2:t9Y",
            "w2",
            Some("claude"),
            AgentStatus::Working,
        ));

        let lines = HerdrTree::from_snapshot(&s).log_lines();

        assert_eq!(
            lines,
            [
                "  ws w2 \"contr\" working",
                "    tab w2:t9Y \"planner\" working",
                "      pane w2:p90 claude working focused",
                "      pane w2:p91 - -",
            ]
        );
    }

    #[test]
    fn status_change_log_line_uses_dashes_for_missing_values() {
        let change = StatusChange {
            pane_id: "w2:p9Z".into(),
            agent: Some("claude".into()),
            from: Some(AgentState::Working),
            to: Some(AgentState::Idle),
        };
        assert_eq!(
            change.log_line(),
            "herdr status pane=w2:p9Z agent=claude working -> idle"
        );

        let appeared = StatusChange {
            pane_id: "w2:p9Z".into(),
            agent: None,
            from: None,
            to: Some(AgentState::Idle),
        };
        assert_eq!(
            appeared.log_line(),
            "herdr status pane=w2:p9Z agent=- - -> idle"
        );
    }
}
