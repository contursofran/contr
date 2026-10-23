//! The herdr tree the app shows, built from one `session.snapshot`, and the
//! changes between two trees that the log reports.

use std::collections::{BTreeMap, HashMap};

use crate::wire::{WirePane, WireSnapshot, WireStatus, WireTab};

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

impl From<WireStatus> for AgentState {
    fn from(status: WireStatus) -> Self {
        match status {
            WireStatus::Idle => Self::Idle,
            WireStatus::Working => Self::Working,
            WireStatus::Blocked => Self::Blocked,
            WireStatus::Done => Self::Done,
            WireStatus::Unknown => Self::Unknown,
        }
    }
}

/// The herdr tree at one snapshot.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HerdrTree {
    pub version: String,
    pub focused_pane_id: Option<String>,
    pub workspaces: Vec<Workspace>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Workspace {
    pub id: String,
    pub label: String,
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
    /// Pane tokens as herdr sent them. Empty when the pane has none.
    pub tokens: BTreeMap<String, String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentInfo {
    /// `display_agent` when herdr sends it, else `agent`.
    pub name: String,
    pub status: AgentState,
}

/// An agent state that changed in one pane between two trees.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatusChange {
    pub pane_id: String,
    /// The agent name from `next`, else from `self`.
    pub agent: Option<String>,
    /// `None` when the pane had no agent before.
    pub from: Option<AgentState>,
    /// `None` when the pane has no agent now.
    pub to: Option<AgentState>,
}

impl HerdrTree {
    /// Groups the flat snapshot into the tree, in the snapshot's order. Tabs whose
    /// workspace is missing and panes whose tab is missing are dropped. A pane is
    /// placed by its `tab_id` alone: its `workspace_id` is not checked against the tab's.
    pub fn from_snapshot(s: &WireSnapshot) -> Self {
        let mut tabs_by_workspace: HashMap<&str, Vec<&WireTab>> = HashMap::new();
        for tab in &s.tabs {
            tabs_by_workspace
                .entry(tab.workspace_id.as_str())
                .or_default()
                .push(tab);
        }
        let mut panes_by_tab: HashMap<&str, Vec<&WirePane>> = HashMap::new();
        for pane in &s.panes {
            panes_by_tab
                .entry(pane.tab_id.as_str())
                .or_default()
                .push(pane);
        }

        let workspaces = s
            .workspaces
            .iter()
            .map(|ws| Workspace {
                id: ws.workspace_id.clone(),
                label: ws.label.clone(),
                status: ws.agent_status.into(),
                focused: ws.focused,
                tabs: tabs_by_workspace
                    .get(ws.workspace_id.as_str())
                    .into_iter()
                    .flatten()
                    .map(|tab| Tab {
                        id: tab.tab_id.clone(),
                        label: tab.label.clone(),
                        status: tab.agent_status.into(),
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
                                agent: agent_info(pane),
                                tokens: pane.tokens.clone(),
                            })
                            .collect(),
                    })
                    .collect(),
            })
            .collect();

        Self {
            version: s.version.clone(),
            focused_pane_id: s.focused_pane_id.clone(),
            workspaces,
        }
    }

    pub fn tab_count(&self) -> usize {
        self.workspaces.iter().map(|ws| ws.tabs.len()).sum()
    }

    pub fn pane_count(&self) -> usize {
        self.panes().count()
    }

    /// Pane ids in tree order: the ids to subscribe to for status events.
    pub fn pane_ids(&self) -> Vec<String> {
        self.panes().map(|pane| pane.id.clone()).collect()
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
                        .map(|agent| agent.name.clone()),
                    from,
                    to,
                })
            })
            .collect()
    }

    /// One indented line per workspace, tab and pane, for the log. The header
    /// line with the counts is the caller's.
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
                lines.extend(tab.panes.iter().map(pane_log_line));
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
    /// `herdr status pane=<id> agent=<name> <from> -> <to>`; `-` for no agent or no state.
    pub fn log_line(&self) -> String {
        let name = self.agent.as_deref().unwrap_or("-");
        let from = self.from.map_or("-", AgentState::label);
        let to = self.to.map_or("-", AgentState::label);
        format!(
            "herdr status pane={} agent={name} {from} -> {to}",
            self.pane_id
        )
    }
}

/// The agent in a pane: `display_agent` if herdr sends it, else `agent`. A pane
/// with neither has no agent, whatever its `agent_status` says.
fn agent_info(pane: &WirePane) -> Option<AgentInfo> {
    let name = pane.display_agent.as_ref().or(pane.agent.as_ref())?;
    Some(AgentInfo {
        name: name.clone(),
        status: pane.agent_status.into(),
    })
}

fn pane_log_line(pane: &Pane) -> String {
    let (name, status) = match &pane.agent {
        Some(agent) => (agent.name.as_str(), agent.status.label()),
        None => ("-", "-"),
    };
    let focus = if pane.focused { " focused" } else { "" };
    format!("      pane {} {name} {status}{focus}", pane.id)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    /// A snapshot from JSON object lists, one per array: `workspaces`, `tabs`
    /// and `panes` are the array bodies without brackets.
    fn parse(workspaces: &str, tabs: &str, panes: &str) -> WireSnapshot {
        let json = format!(
            r#"{{"version":"0.9.3","protocol":22,"workspaces":[{workspaces}],"tabs":[{tabs}],"panes":[{panes}]}}"#
        );
        serde_json::from_str(&json).expect("test snapshot is valid JSON")
    }

    /// Workspace `w1`, tab `t1`, and panes `p1` (claude) and `p2` (codex) in the given states.
    fn two_agents(p1: &str, p2: &str) -> WireSnapshot {
        parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"working"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}"#,
            &format!(
                r#"{{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"{p1}"}},
                   {{"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent":"codex","agent_status":"{p2}"}}"#
            ),
        )
    }

    #[test]
    fn groups_tabs_and_panes_under_their_parents_in_order() {
        let mut s = parse(
            r#"{"workspace_id":"w1","label":"one","agent_status":"idle"},
               {"workspace_id":"w2","label":"two","agent_status":"working","focused":true}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"a","agent_status":"idle"},
               {"tab_id":"t2","workspace_id":"w2","label":"b","agent_status":"working"},
               {"tab_id":"t3","workspace_id":"w1","label":"c","agent_status":"done"}"#,
            r#"{"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
               {"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
               {"pane_id":"p3","tab_id":"t2","workspace_id":"w2","agent_status":"unknown","focused":true},
               {"pane_id":"p4","tab_id":"t3","workspace_id":"w1","agent_status":"unknown"}"#,
        );
        s.focused_pane_id = Some("p3".into());

        let tree = HerdrTree::from_snapshot(&s);

        assert_eq!(tree.version, "0.9.3");
        assert_eq!(tree.focused_pane_id.as_deref(), Some("p3"));
        let ws: Vec<&str> = tree.workspaces.iter().map(|w| w.id.as_str()).collect();
        assert_eq!(ws, ["w1", "w2"]);
        assert!(tree.workspaces[1].focused);
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
        assert_eq!(t1_panes, ["p2", "p1"]);
        assert_eq!(tree.workspaces[0].tabs[1].panes[0].id, "p4");
        assert_eq!(tree.workspaces[1].tabs[0].panes[0].id, "p3");
        assert!(tree.workspaces[1].tabs[0].panes[0].focused);
        assert_eq!(tree.tab_count(), 3);
        assert_eq!(tree.pane_count(), 4);
    }

    #[test]
    fn drops_tabs_and_panes_whose_parent_is_missing() {
        let s = parse(
            r#"{"workspace_id":"w1","label":"one","agent_status":"idle"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"idle"},
               {"tab_id":"t9","workspace_id":"gone","label":"orphan","agent_status":"idle"}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
               {"pane_id":"p9","tab_id":"gone-tab","workspace_id":"w1","agent_status":"unknown"},
               {"pane_id":"p8","tab_id":"t9","workspace_id":"gone","agent_status":"unknown"}"#,
        );

        let tree = HerdrTree::from_snapshot(&s);

        assert_eq!(tree.tab_count(), 1);
        assert_eq!(tree.pane_count(), 1);
        assert_eq!(tree.pane_ids(), ["p1"]);
    }

    #[test]
    fn joins_agent_by_pane_and_prefers_display_agent() {
        let s = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"working"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","display_agent":"Claude Code","agent_status":"working"},
               {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent":"codex","agent_status":"blocked"},
               {"pane_id":"p3","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}"#,
        );

        let tree = HerdrTree::from_snapshot(&s);
        let panes = &tree.workspaces[0].tabs[0].panes;

        assert_eq!(
            panes[0].agent,
            Some(AgentInfo {
                name: "Claude Code".into(),
                status: AgentState::Working,
            })
        );
        assert_eq!(
            panes[1].agent,
            Some(AgentInfo {
                name: "codex".into(),
                status: AgentState::Blocked,
            })
        );
        assert_eq!(panes[2].agent, None);
        assert_eq!(tree.pane_count(), 3);
    }

    #[test]
    fn a_pane_without_an_agent_name_has_no_agent_whatever_its_status() {
        let s = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"working"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"working"}"#,
        );

        let tree = HerdrTree::from_snapshot(&s);

        assert_eq!(tree.workspaces[0].tabs[0].panes[0].agent, None);
    }

    #[test]
    fn maps_each_herdr_status_one_to_one() {
        let words = ["idle", "working", "blocked", "done", "unknown"];
        let states = [
            AgentState::Idle,
            AgentState::Working,
            AgentState::Blocked,
            AgentState::Done,
            AgentState::Unknown,
        ];
        for (word, state) in words.iter().zip(states) {
            let s = parse(
                &format!(r#"{{"workspace_id":"w1","label":"contr","agent_status":"{word}"}}"#),
                &format!(
                    r#"{{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"{word}"}}"#
                ),
                &format!(
                    r#"{{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"{word}"}}"#
                ),
            );

            let tree = HerdrTree::from_snapshot(&s);

            assert_eq!(state.label(), *word);
            assert_eq!(tree.workspaces[0].status, state);
            assert_eq!(tree.workspaces[0].tabs[0].status, state);
            let agent = tree.workspaces[0].tabs[0].panes[0].agent.as_ref();
            assert_eq!(agent.map(|a| a.status), Some(state));
        }
    }

    #[test]
    fn status_changes_reports_only_the_pane_that_changed() {
        let before = two_agents("idle", "working");
        let after = two_agents("idle", "blocked");

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
        let before = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"unknown"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"unknown"}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}"#,
        );
        let after = two_agents("idle", "idle");

        let changes =
            HerdrTree::from_snapshot(&before).status_changes(&HerdrTree::from_snapshot(&after));

        assert_eq!(
            changes,
            [StatusChange {
                pane_id: "p1".into(),
                agent: Some("claude".into()),
                from: None,
                to: Some(AgentState::Idle),
            }]
        );
    }

    #[test]
    fn an_agent_leaving_a_pane_is_a_change_to_none_named_as_before() {
        let before = two_agents("idle", "working");
        let after = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"idle"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"idle"}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"idle"},
               {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}"#,
        );

        let changes =
            HerdrTree::from_snapshot(&before).status_changes(&HerdrTree::from_snapshot(&after));

        assert_eq!(
            changes,
            [StatusChange {
                pane_id: "p2".into(),
                agent: Some("codex".into()),
                from: Some(AgentState::Working),
                to: None,
            }]
        );
    }

    #[test]
    fn a_new_or_gone_pane_is_not_a_status_change() {
        let one_pane = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"working"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"working"}"#,
        );
        let two_panes = two_agents("working", "idle");
        let before = HerdrTree::from_snapshot(&one_pane);
        let after = HerdrTree::from_snapshot(&two_panes);

        assert!(before.status_changes(&after).is_empty());
        assert!(after.status_changes(&before).is_empty());
    }

    #[test]
    fn same_shape_ignores_status_and_focus() {
        let before = two_agents("idle", "idle");
        let after = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"working"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"blocked","focused":true}"#,
            r#"{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"blocked","focused":true},
               {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent":"codex","agent_status":"working"}"#,
        );

        assert!(HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn same_shape_is_false_when_a_tab_is_added() {
        let before = two_agents("idle", "idle");
        let mut after = before.clone();
        after.tabs.push(
            serde_json::from_str(
                r#"{"tab_id":"t2","workspace_id":"w1","label":"second","agent_status":"idle"}"#,
            )
            .expect("test tab is valid JSON"),
        );

        assert!(!HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn same_shape_is_false_when_a_label_changes() {
        let before = two_agents("idle", "idle");
        let mut after = before.clone();
        after.tabs[0].label = "renamed".into();

        assert!(!HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn same_shape_is_false_when_a_pane_label_changes() {
        let before = two_agents("idle", "idle");
        let mut after = before.clone();
        after.panes[1].label = Some("renamed".into());

        assert!(!HerdrTree::from_snapshot(&before).same_shape(&HerdrTree::from_snapshot(&after)));
    }

    #[test]
    fn pane_ids_follow_tree_order() {
        let mut s = two_agents("idle", "idle");
        s.panes.reverse();

        let tree = HerdrTree::from_snapshot(&s);

        assert_eq!(tree.pane_ids(), ["p2", "p1"]);
    }

    #[test]
    fn pane_ids_follow_tree_order_across_tabs() {
        // The snapshot lists t2's pane first; the tree puts each pane under its tab.
        let s = parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"idle"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"a","agent_status":"idle"},
               {"tab_id":"t2","workspace_id":"w1","label":"b","agent_status":"idle"}"#,
            r#"{"pane_id":"p3","tab_id":"t2","workspace_id":"w1","agent_status":"unknown"},
               {"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
               {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}"#,
        );

        assert_eq!(HerdrTree::from_snapshot(&s).pane_ids(), ["p1", "p2", "p3"]);
    }

    #[test]
    fn status_change_log_lines_name_the_pane_and_both_states() {
        let change = |from, to| StatusChange {
            pane_id: "w2:p9Z".into(),
            agent: Some("claude".into()),
            from,
            to,
        };

        assert_eq!(
            change(Some(AgentState::Working), Some(AgentState::Idle)).log_line(),
            "herdr status pane=w2:p9Z agent=claude working -> idle"
        );
        assert_eq!(
            change(None, Some(AgentState::Blocked)).log_line(),
            "herdr status pane=w2:p9Z agent=claude - -> blocked"
        );
        assert_eq!(
            change(Some(AgentState::Done), None).log_line(),
            "herdr status pane=w2:p9Z agent=claude done -> -"
        );
    }

    #[test]
    fn log_lines_match_the_documented_format() {
        let s = parse(
            r#"{"workspace_id":"w2","label":"contr","agent_status":"working"}"#,
            r#"{"tab_id":"w2:t9Y","workspace_id":"w2","label":"planner","agent_status":"working"}"#,
            r#"{"pane_id":"w2:p90","tab_id":"w2:t9Y","workspace_id":"w2","agent":"claude","agent_status":"working","focused":true},
               {"pane_id":"w2:p91","tab_id":"w2:t9Y","workspace_id":"w2","agent_status":"unknown"}"#,
        );

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

    /// Workspace `w1`, tab `t1` and one idle pane `p1` whose `tokens` is the given JSON object.
    fn pane_with_tokens(tokens: &str) -> WireSnapshot {
        parse(
            r#"{"workspace_id":"w1","label":"contr","agent_status":"idle"}"#,
            r#"{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"idle"}"#,
            &format!(
                r#"{{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"idle","tokens":{tokens}}}"#
            ),
        )
    }

    #[test]
    fn from_snapshot_copies_pane_tokens_as_herdr_sent_them() {
        let tree =
            HerdrTree::from_snapshot(&pane_with_tokens(r#"{"contr_board":"1 s1 3","x":"y"}"#));

        let tokens = &tree.workspaces[0].tabs[0].panes[0].tokens;
        assert_eq!(tokens.len(), 2);
        assert_eq!(tokens["contr_board"], "1 s1 3");
        assert_eq!(tokens["x"], "y");
    }

    #[test]
    fn a_pane_without_tokens_has_an_empty_map() {
        let tree = HerdrTree::from_snapshot(&two_agents("idle", "working"));

        assert!(tree.panes().all(|pane| pane.tokens.is_empty()));
    }

    #[test]
    fn a_changed_or_removed_token_makes_the_trees_unequal_but_keeps_the_shape() {
        let before = HerdrTree::from_snapshot(&pane_with_tokens(r#"{"contr_board":"1 s1 3"}"#));
        let changed = HerdrTree::from_snapshot(&pane_with_tokens(r#"{"contr_board":"1 s1 4"}"#));
        let removed = HerdrTree::from_snapshot(&pane_with_tokens("{}"));

        for next in [&changed, &removed] {
            assert_ne!(&before, next);
            assert!(before.same_shape(next));
            assert!(before.status_changes(next).is_empty());
        }
    }
}
