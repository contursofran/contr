//! The agents list the sidebar shows: one row per herdr pane that runs an
//! agent, in tree order. Pure: no socket, no GPUI. Name, model and role come
//! from the board bridge (`AgentMeta`) when it has them. Without it, a row shows
//! the herdr tab label and the agent kind.

use crate::model::{AgentInfo, AgentState, HerdrTree, Pane, Tab};

/// What the board bridge knows about one agent. Every field is optional.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AgentMeta {
    pub name: Option<String>,
    pub model: Option<String>,
    pub role: Option<String>,
    pub link: Option<String>,
}

/// One row of the agents list.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentRow {
    pub pane_id: String,
    pub tab_id: String,
    pub workspace_id: String,
    /// The bridge's name, else the tab label. With more than one agent in the
    /// tab: the tab label, then the pane's label or its number.
    pub name: String,
    /// `"model · role"` from the bridge, with the parts that are present. Else the
    /// agent kind, as herdr reports it (`claude`).
    pub detail: String,
    /// The bridge's link, as given. `None` when there is none.
    pub link: Option<String>,
    /// herdr's state, one to one.
    pub status: AgentState,
    /// True for the pane that herdr focuses.
    pub focused: bool,
}

/// One row per pane that runs an agent, in tree order: workspace, tab, then
/// pane. `meta` gives the bridge's data for a pane id, or `None` when there is none.
pub fn agent_rows(tree: &HerdrTree, meta: impl Fn(&str) -> Option<AgentMeta>) -> Vec<AgentRow> {
    let focused = tree.focused_pane_id.as_deref();
    let mut rows = Vec::new();
    for ws in &tree.workspaces {
        for tab in &ws.tabs {
            let agents: Vec<(&Pane, &AgentInfo)> = tab
                .panes
                .iter()
                .filter_map(|pane| pane.agent.as_ref().map(|agent| (pane, agent)))
                .collect();
            for (index, (pane, agent)) in agents.iter().copied().enumerate() {
                let bridge = meta(&pane.id).unwrap_or_default();
                let detail = detail_of(&bridge, &agent.name);
                let name = bridge.name.as_deref().and_then(present).map_or_else(
                    || fallback_name(tab, agent, pane, index, agents.len()),
                    str::to_owned,
                );
                rows.push(AgentRow {
                    pane_id: pane.id.clone(),
                    tab_id: tab.id.clone(),
                    workspace_id: ws.id.clone(),
                    name,
                    detail,
                    link: bridge.link.as_deref().and_then(present).map(str::to_owned),
                    status: agent.status,
                    focused: focused == Some(pane.id.as_str()),
                });
            }
        }
    }
    rows
}

/// The header count: `"N running"`. Every listed agent counts, idle and done too.
pub fn running_line(n: usize) -> String {
    format!("{n} running")
}

/// The tab label, or the agent kind when the label is blank. When the tab has more than one
/// agent, the name is followed by the pane's label, or by the pane's 1-based number among the
/// tab's agents.
fn fallback_name(
    tab: &Tab,
    agent: &AgentInfo,
    pane: &Pane,
    index: usize,
    agents_in_tab: usize,
) -> String {
    let base = present(&tab.label).unwrap_or(agent.name.as_str());
    if agents_in_tab < 2 {
        return base.to_owned();
    }
    let which = pane
        .label
        .as_deref()
        .and_then(present)
        .map_or_else(|| (index + 1).to_string(), str::to_owned);
    format!("{base} · {which}")
}

/// `"model · role"` from the parts of `bridge` that are present. The agent kind
/// when neither is.
fn detail_of(bridge: &AgentMeta, agent: &str) -> String {
    let parts: Vec<&str> = [bridge.model.as_deref(), bridge.role.as_deref()]
        .into_iter()
        .flatten()
        .filter_map(present)
        .collect();
    if parts.is_empty() {
        agent.to_owned()
    } else {
        parts.join(" · ")
    }
}

/// `text` when it has something besides white space, else `None`. The text is kept as given.
fn present(text: &str) -> Option<&str> {
    (!text.trim().is_empty()).then_some(text)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::model::Workspace;

    /// A pane: `(id, label, agent)`, where the agent is `(name, state)`.
    type PaneSpec<'a> = (&'a str, Option<&'a str>, Option<(&'a str, AgentState)>);
    /// A tab: `(id, label, panes)`.
    type TabSpec<'a> = (&'a str, &'a str, &'a [PaneSpec<'a>]);
    /// A workspace: `(id, tabs)`.
    type WsSpec<'a> = (&'a str, &'a [TabSpec<'a>]);

    /// Builds the tree from the specs. Workspace and tab states are idle, and
    /// `focused` names the focused pane.
    fn tree(focused: Option<&str>, workspaces: &[WsSpec]) -> HerdrTree {
        HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: focused.map(Into::into),
            workspaces: workspaces
                .iter()
                .map(|(ws_id, tabs)| Workspace {
                    id: (*ws_id).into(),
                    label: (*ws_id).into(),
                    status: AgentState::Idle,
                    focused: false,
                    tabs: tabs
                        .iter()
                        .map(|(tab_id, label, panes)| Tab {
                            id: (*tab_id).into(),
                            label: (*label).into(),
                            status: AgentState::Idle,
                            focused: false,
                            panes: panes
                                .iter()
                                .map(|(pane_id, label, agent)| Pane {
                                    id: (*pane_id).into(),
                                    label: label.map(Into::into),
                                    cwd: None,
                                    focused: focused == Some(*pane_id),
                                    agent: agent.map(|(name, status)| AgentInfo {
                                        name: name.into(),
                                        status,
                                    }),
                                })
                                .collect(),
                        })
                        .collect(),
                })
                .collect(),
        }
    }

    fn no_meta(_: &str) -> Option<AgentMeta> {
        None
    }

    fn meta(name: Option<&str>, model: Option<&str>, role: Option<&str>) -> AgentMeta {
        AgentMeta {
            name: name.map(Into::into),
            model: model.map(Into::into),
            role: role.map(Into::into),
            link: None,
        }
    }

    #[test]
    fn panes_without_an_agent_give_no_row() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, None),
                        ("p2", None, Some(("claude", AgentState::Idle))),
                    ],
                )],
            )],
        );

        let rows = agent_rows(&t, no_meta);

        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].pane_id, "p2");
    }

    #[test]
    fn a_tree_without_agents_gives_no_rows() {
        let t = tree(None, &[("w1", &[("t1", "a", &[("p1", None, None)])])]);

        assert!(agent_rows(&t, no_meta).is_empty());
        assert!(agent_rows(&tree(None, &[]), no_meta).is_empty());
    }

    #[test]
    fn rows_follow_workspace_tab_then_pane_across_workspaces() {
        let t = tree(
            None,
            &[
                (
                    "w1",
                    &[
                        (
                            "t1",
                            "a",
                            &[
                                ("p1", None, None),
                                ("p2", None, Some(("claude", AgentState::Idle))),
                            ],
                        ),
                        (
                            "t2",
                            "b",
                            &[("p3", None, Some(("codex", AgentState::Working)))],
                        ),
                    ],
                ),
                (
                    "w2",
                    &[(
                        "t3",
                        "c",
                        &[("p4", None, Some(("claude", AgentState::Done)))],
                    )],
                ),
            ],
        );

        let rows = agent_rows(&t, no_meta);

        let ids: Vec<(&str, &str, &str)> = rows
            .iter()
            .map(|r| {
                (
                    r.workspace_id.as_str(),
                    r.tab_id.as_str(),
                    r.pane_id.as_str(),
                )
            })
            .collect();
        assert_eq!(
            ids,
            [("w1", "t1", "p2"), ("w1", "t2", "p3"), ("w2", "t3", "p4")]
        );
    }

    #[test]
    fn the_name_is_the_tab_label_for_a_single_agent() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "b-1151",
                    &[("p1", Some("ignored"), Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        assert_eq!(agent_rows(&t, no_meta)[0].name, "b-1151");
    }

    #[test]
    fn two_agents_in_a_tab_are_named_by_pane_label_or_number() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", Some("left"), Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );

        let names: Vec<String> = agent_rows(&t, no_meta)
            .into_iter()
            .map(|r| r.name)
            .collect();

        assert_eq!(names, ["planner · left", "planner · 2"]);
    }

    #[test]
    fn the_number_counts_only_the_agent_panes_of_the_tab() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p0", None, None),
                        ("p1", None, Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );

        let names: Vec<String> = agent_rows(&t, no_meta)
            .into_iter()
            .map(|r| r.name)
            .collect();

        assert_eq!(names, ["planner · 1", "planner · 2"]);
    }

    #[test]
    fn a_blank_pane_label_is_no_label() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", Some("   "), Some(("claude", AgentState::Idle))),
                        ("p2", Some(""), Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );

        let names: Vec<String> = agent_rows(&t, no_meta)
            .into_iter()
            .map(|r| r.name)
            .collect();

        assert_eq!(names, ["planner · 1", "planner · 2"]);
    }

    #[test]
    fn a_blank_tab_label_falls_back_to_the_agent_kind() {
        for label in ["", "   "] {
            let t = tree(
                None,
                &[(
                    "w1",
                    &[(
                        "t1",
                        label,
                        &[("p1", None, Some(("claude", AgentState::Idle)))],
                    )],
                )],
            );

            assert_eq!(agent_rows(&t, no_meta)[0].name, "claude", "label {label:?}");
        }
    }

    #[test]
    fn a_blank_tab_label_keeps_the_split_suffix_with_each_agent_kind() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    " ",
                    &[
                        ("p1", Some("left"), Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );

        let names: Vec<String> = agent_rows(&t, no_meta)
            .into_iter()
            .map(|r| r.name)
            .collect();

        assert_eq!(names, ["claude · left", "codex · 2"]);
    }

    #[test]
    fn the_bridge_name_overrides_the_tab_label_and_the_split_name() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );
        let bridge = |pane: &str| (pane == "p1").then(|| meta(Some("b-1151"), None, None));

        let rows = agent_rows(&t, bridge);

        assert_eq!(rows[0].name, "b-1151");
        assert_eq!(rows[1].name, "planner · 2");
    }

    #[test]
    fn a_blank_bridge_name_is_no_name() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[("p1", None, Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        let rows = agent_rows(&t, |_| Some(meta(Some("  "), None, None)));

        assert_eq!(rows[0].name, "planner");
    }

    #[test]
    fn the_detail_is_the_agent_kind_without_bridge_data() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[("p1", None, Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        assert_eq!(agent_rows(&t, no_meta)[0].detail, "claude");
        // A bridge that names the agent but gives no model or role keeps the agent kind.
        let named = agent_rows(&t, |_| Some(meta(Some("b-1151"), None, None)));
        assert_eq!(named[0].detail, "claude");
    }

    #[test]
    fn the_detail_joins_model_and_role() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[("p1", None, Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        let rows = agent_rows(&t, |_| Some(meta(None, Some("haiku 5.5"), Some("builder"))));

        assert_eq!(rows[0].detail, "haiku 5.5 · builder");
    }

    #[test]
    fn the_detail_has_only_the_part_that_is_present() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[("p1", None, Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        let role_only = agent_rows(&t, |_| Some(meta(None, None, Some("builder"))));
        let model_only = agent_rows(&t, |_| Some(meta(None, Some("haiku 5.5"), None)));

        assert_eq!(role_only[0].detail, "builder");
        assert_eq!(model_only[0].detail, "haiku 5.5");
    }

    #[test]
    fn blank_model_and_role_fall_back_to_the_agent_kind() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[("p1", None, Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        let rows = agent_rows(&t, |_| Some(meta(None, Some(""), Some("  "))));

        assert_eq!(rows[0].detail, "claude");
    }

    #[test]
    fn the_link_is_passed_through_as_given() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );
        let bridge = |pane: &str| {
            Some(AgentMeta {
                link: Some(if pane == "p1" { "#640" } else { "PR 12" }.into()),
                ..AgentMeta::default()
            })
        };

        let rows = agent_rows(&t, bridge);

        assert_eq!(rows[0].link.as_deref(), Some("#640"));
        assert_eq!(rows[1].link.as_deref(), Some("PR 12"));
    }

    #[test]
    fn a_missing_or_blank_link_is_none() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[("p1", None, Some(("claude", AgentState::Idle)))],
                )],
            )],
        );

        let none = agent_rows(&t, no_meta);
        let blank = agent_rows(&t, |_| {
            Some(AgentMeta {
                link: Some(" ".into()),
                ..AgentMeta::default()
            })
        });

        assert_eq!(none[0].link, None);
        assert_eq!(blank[0].link, None);
    }

    #[test]
    fn all_five_states_pass_through_one_to_one() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("claude", AgentState::Working))),
                        ("p3", None, Some(("claude", AgentState::Blocked))),
                        ("p4", None, Some(("claude", AgentState::Done))),
                        ("p5", None, Some(("claude", AgentState::Unknown))),
                    ],
                )],
            )],
        );

        let states: Vec<AgentState> = agent_rows(&t, no_meta).iter().map(|r| r.status).collect();

        assert_eq!(
            states,
            [
                AgentState::Idle,
                AgentState::Working,
                AgentState::Blocked,
                AgentState::Done,
                AgentState::Unknown,
            ]
        );
    }

    #[test]
    fn only_the_focused_pane_is_focused() {
        let t = tree(
            Some("p2"),
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, Some(("claude", AgentState::Idle))),
                        ("p2", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );

        let focused: Vec<bool> = agent_rows(&t, no_meta).iter().map(|r| r.focused).collect();

        assert_eq!(focused, [false, true]);
    }

    #[test]
    fn a_focus_on_a_pane_without_an_agent_or_an_unknown_pane_marks_no_row() {
        let rows_for = |focus: &str| {
            let t = tree(
                Some(focus),
                &[(
                    "w1",
                    &[(
                        "t1",
                        "planner",
                        &[
                            ("p1", None, None),
                            ("p2", None, Some(("claude", AgentState::Idle))),
                        ],
                    )],
                )],
            );
            agent_rows(&t, no_meta)
        };

        assert_eq!(rows_for("p1").iter().filter(|r| r.focused).count(), 0);
        assert_eq!(rows_for("gone").iter().filter(|r| r.focused).count(), 0);
    }

    #[test]
    fn the_bridge_is_asked_for_each_agent_pane_by_its_id() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, Some(("claude", AgentState::Idle))),
                        ("p2", None, None),
                        ("p3", None, Some(("codex", AgentState::Idle))),
                    ],
                )],
            )],
        );
        let asked = std::cell::RefCell::new(Vec::new());

        let rows = agent_rows(&t, |pane| {
            asked.borrow_mut().push(pane.to_owned());
            None
        });

        assert_eq!(*asked.borrow(), ["p1", "p3"]);
        assert_eq!(rows.len(), 2);
    }

    #[test]
    fn running_line_reads_zero_one_and_many() {
        assert_eq!(running_line(0), "0 running");
        assert_eq!(running_line(1), "1 running");
        assert_eq!(running_line(3), "3 running");
    }

    #[test]
    fn running_counts_every_listed_agent_including_idle_and_done() {
        let t = tree(
            None,
            &[(
                "w1",
                &[(
                    "t1",
                    "planner",
                    &[
                        ("p1", None, Some(("claude", AgentState::Working))),
                        ("p2", None, Some(("claude", AgentState::Idle))),
                        ("p3", None, Some(("claude", AgentState::Done))),
                    ],
                )],
            )],
        );

        assert_eq!(running_line(agent_rows(&t, no_meta).len()), "3 running");
    }
}
