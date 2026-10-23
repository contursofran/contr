//! The tab strip the title bar shows: the tabs of herdr's current workspace,
//! the target of "+", and the target of split. Pure: no socket, no GPUI.

use crate::model::{AgentState, HerdrTree, Pane, Tab, Workspace};

/// One tab in the strip.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TabItem {
    pub id: String,
    /// herdr's tab label, shown as it is.
    pub label: String,
    /// herdr's aggregate status for the tab.
    pub status: AgentState,
    /// True for the focused tab. At most one tab in a strip is active.
    pub active: bool,
}

/// What the title bar shows for one herdr tree.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct TabStrip {
    /// The target of "+". `None` when there is no tree or no workspace.
    pub workspace_id: Option<String>,
    /// Every tab of the current workspace, in herdr's order.
    pub tabs: Vec<TabItem>,
    /// The target of split. `None` when there is no active tab or it has no panes.
    pub split_pane_id: Option<String>,
}

impl TabStrip {
    /// The strip for `tree`. No tree (herdr not running) and no workspace both
    /// give the empty strip.
    pub fn from_tree(tree: Option<&HerdrTree>) -> Self {
        let Some(tree) = tree else {
            return Self::default();
        };
        let Some(ws) = tree.current_workspace() else {
            return Self::default();
        };
        let focused_pane = tree.focused_pane_id.as_deref();
        let active = active_index(ws, focused_pane);
        let tabs = ws
            .tabs
            .iter()
            .enumerate()
            .map(|(i, tab)| TabItem {
                id: tab.id.clone(),
                label: tab.label.clone(),
                status: tab.status,
                active: Some(i) == active,
            })
            .collect();
        let split_pane_id = active
            .and_then(|i| ws.tabs.get(i))
            .and_then(|tab| split_pane(tab, focused_pane))
            .map(|pane| pane.id.clone());
        Self {
            workspace_id: Some(ws.id.clone()),
            tabs,
            split_pane_id,
        }
    }
}

impl HerdrTree {
    /// The workspace the title bar works in: the focused one, else the one that
    /// holds the focused pane, else the first. `None` without workspaces.
    pub fn current_workspace(&self) -> Option<&Workspace> {
        self.workspaces
            .iter()
            .find(|ws| ws.focused)
            .or_else(|| {
                let pane = self.focused_pane_id.as_deref()?;
                self.workspaces
                    .iter()
                    .find(|ws| ws.tabs.iter().any(|tab| tab_holds(tab, pane)))
            })
            .or_else(|| self.workspaces.first())
    }
}

/// The active tab: the first tab with `focused`, else the tab holding the
/// focused pane, else none. Returns the tab's index in `ws.tabs`.
fn active_index(ws: &Workspace, focused_pane: Option<&str>) -> Option<usize> {
    ws.tabs.iter().position(|tab| tab.focused).or_else(|| {
        let pane = focused_pane?;
        ws.tabs.iter().position(|tab| tab_holds(tab, pane))
    })
}

/// The pane a split targets in `tab`: the focused pane if it is in `tab`, else
/// the first focused pane, else the first pane.
fn split_pane<'a>(tab: &'a Tab, focused_pane: Option<&str>) -> Option<&'a Pane> {
    tab.panes
        .iter()
        .find(|pane| Some(pane.id.as_str()) == focused_pane)
        .or_else(|| tab.panes.iter().find(|pane| pane.focused))
        .or_else(|| tab.panes.first())
}

fn tab_holds(tab: &Tab, pane_id: &str) -> bool {
    tab.panes.iter().any(|pane| pane.id == pane_id)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    fn pane(id: &str, focused: bool) -> Pane {
        Pane {
            id: id.into(),
            label: None,
            cwd: None,
            focused,
            agent: None,
        }
    }

    /// An idle tab whose label is its id.
    fn tab(id: &str, focused: bool, panes: Vec<Pane>) -> Tab {
        Tab {
            id: id.into(),
            label: id.into(),
            status: AgentState::Idle,
            focused,
            panes,
        }
    }

    fn workspace(id: &str, focused: bool, tabs: Vec<Tab>) -> Workspace {
        Workspace {
            id: id.into(),
            label: id.into(),
            status: AgentState::Idle,
            focused,
            tabs,
        }
    }

    fn tree(focused_pane_id: Option<&str>, workspaces: Vec<Workspace>) -> HerdrTree {
        HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: focused_pane_id.map(str::to_owned),
            workspaces,
        }
    }

    fn strip_of(tree: &HerdrTree) -> TabStrip {
        TabStrip::from_tree(Some(tree))
    }

    fn current_id(tree: &HerdrTree) -> Option<&str> {
        tree.current_workspace().map(|ws| ws.id.as_str())
    }

    /// The ids of the active tabs, so a test can see that there is at most one.
    fn active_ids(strip: &TabStrip) -> Vec<&str> {
        strip
            .tabs
            .iter()
            .filter(|item| item.active)
            .map(|item| item.id.as_str())
            .collect()
    }

    #[test]
    fn no_tree_gives_the_empty_strip() {
        assert_eq!(TabStrip::from_tree(None), TabStrip::default());
    }

    #[test]
    fn no_workspace_gives_the_empty_strip() {
        let t = tree(Some("w1:p1"), vec![]);

        assert_eq!(strip_of(&t), TabStrip::default());
    }

    #[test]
    fn shows_only_the_focused_workspace_tabs_in_herdr_order() {
        let t = tree(
            None,
            vec![
                workspace("w1", false, vec![tab("w1:t1", false, vec![])]),
                workspace(
                    "w2",
                    true,
                    vec![tab("w2:t2", false, vec![]), tab("w2:t1", false, vec![])],
                ),
            ],
        );

        let strip = strip_of(&t);

        assert_eq!(strip.workspace_id.as_deref(), Some("w2"));
        let ids: Vec<&str> = strip.tabs.iter().map(|item| item.id.as_str()).collect();
        assert_eq!(ids, ["w2:t2", "w2:t1"]);
    }

    #[test]
    fn current_workspace_prefers_the_focused_flag_over_the_focused_pane() {
        let t = tree(
            Some("w1:p1"),
            vec![
                workspace(
                    "w1",
                    false,
                    vec![tab("w1:t1", false, vec![pane("w1:p1", true)])],
                ),
                workspace("w2", true, vec![]),
            ],
        );

        assert_eq!(current_id(&t), Some("w2"));
    }

    #[test]
    fn current_workspace_is_the_one_holding_the_focused_pane_when_none_is_focused() {
        let t = tree(
            Some("w2:p3"),
            vec![
                workspace(
                    "w1",
                    false,
                    vec![tab("w1:t1", false, vec![pane("w1:p1", false)])],
                ),
                workspace(
                    "w2",
                    false,
                    vec![tab("w2:t1", false, vec![pane("w2:p3", false)])],
                ),
            ],
        );

        assert_eq!(current_id(&t), Some("w2"));
    }

    #[test]
    fn current_workspace_is_the_first_when_nothing_points_at_one() {
        let no_pane = tree(
            None,
            vec![
                workspace("w1", false, vec![]),
                workspace("w2", false, vec![]),
            ],
        );
        let unknown_pane = tree(
            Some("gone:p1"),
            vec![
                workspace("w1", false, vec![]),
                workspace("w2", false, vec![]),
            ],
        );

        assert_eq!(current_id(&no_pane), Some("w1"));
        assert_eq!(current_id(&unknown_pane), Some("w1"));
    }

    #[test]
    fn current_workspace_is_none_without_workspaces() {
        assert!(tree(Some("w1:p1"), vec![]).current_workspace().is_none());
    }

    #[test]
    fn the_focused_tab_is_the_active_one() {
        let t = tree(
            None,
            vec![workspace(
                "w1",
                true,
                vec![
                    tab("w1:t1", false, vec![]),
                    tab("w1:t2", true, vec![]),
                    tab("w1:t3", false, vec![]),
                ],
            )],
        );

        assert_eq!(active_ids(&strip_of(&t)), ["w1:t2"]);
    }

    #[test]
    fn the_tab_holding_the_focused_pane_is_active_when_no_tab_is_focused() {
        let t = tree(
            Some("w1:p3"),
            vec![workspace(
                "w1",
                true,
                vec![
                    tab("w1:t1", false, vec![pane("w1:p1", false)]),
                    tab("w1:t2", false, vec![pane("w1:p3", false)]),
                ],
            )],
        );

        let strip = strip_of(&t);

        assert_eq!(active_ids(&strip), ["w1:t2"]);
        assert_eq!(strip.split_pane_id.as_deref(), Some("w1:p3"));
    }

    #[test]
    fn no_tab_is_active_when_the_focused_pane_is_in_another_workspace() {
        let t = tree(
            Some("w2:p1"),
            vec![
                workspace(
                    "w1",
                    true,
                    vec![tab("w1:t1", false, vec![pane("w1:p1", false)])],
                ),
                workspace(
                    "w2",
                    false,
                    vec![tab("w2:t1", false, vec![pane("w2:p1", true)])],
                ),
            ],
        );

        let strip = strip_of(&t);

        assert_eq!(strip.workspace_id.as_deref(), Some("w1"));
        assert!(active_ids(&strip).is_empty());
        assert_eq!(strip.split_pane_id, None);
    }

    #[test]
    fn at_most_one_tab_is_active_even_if_two_say_focused() {
        let t = tree(
            None,
            vec![workspace(
                "w1",
                true,
                vec![tab("w1:t1", true, vec![]), tab("w1:t2", true, vec![])],
            )],
        );

        assert_eq!(active_ids(&strip_of(&t)), ["w1:t1"]);
    }

    #[test]
    fn a_workspace_without_tabs_keeps_its_id_for_the_plus_button() {
        let t = tree(None, vec![workspace("w1", true, vec![])]);

        let strip = strip_of(&t);

        assert_eq!(strip.workspace_id.as_deref(), Some("w1"));
        assert!(strip.tabs.is_empty());
        assert_eq!(strip.split_pane_id, None);
    }

    #[test]
    fn status_and_label_are_copied_from_each_tab() {
        let t = tree(
            None,
            vec![workspace(
                "w1",
                true,
                vec![
                    Tab {
                        id: "w1:t1".into(),
                        label: "api".into(),
                        status: AgentState::Blocked,
                        focused: true,
                        panes: vec![],
                    },
                    Tab {
                        id: "w1:t2".into(),
                        label: "docs".into(),
                        status: AgentState::Working,
                        focused: false,
                        panes: vec![],
                    },
                    Tab {
                        id: "w1:t3".into(),
                        label: "3".into(),
                        status: AgentState::Unknown,
                        focused: false,
                        panes: vec![],
                    },
                ],
            )],
        );

        let strip = strip_of(&t);
        let got: Vec<(&str, &str, AgentState, bool)> = strip
            .tabs
            .iter()
            .map(|item| {
                (
                    item.id.as_str(),
                    item.label.as_str(),
                    item.status,
                    item.active,
                )
            })
            .collect();

        assert_eq!(
            got,
            [
                ("w1:t1", "api", AgentState::Blocked, true),
                ("w1:t2", "docs", AgentState::Working, false),
                ("w1:t3", "3", AgentState::Unknown, false),
            ]
        );
    }

    #[test]
    fn split_targets_the_focused_pane_of_the_active_tab() {
        let t = tree(
            Some("w1:p2"),
            vec![workspace(
                "w1",
                true,
                vec![tab(
                    "w1:t1",
                    true,
                    vec![pane("w1:p1", true), pane("w1:p2", false)],
                )],
            )],
        );

        assert_eq!(strip_of(&t).split_pane_id.as_deref(), Some("w1:p2"));
    }

    #[test]
    fn split_falls_back_to_the_first_focused_pane_of_the_active_tab() {
        let t = tree(
            Some("w1:p9"),
            vec![workspace(
                "w1",
                true,
                vec![
                    tab(
                        "w1:t1",
                        true,
                        vec![
                            pane("w1:p1", false),
                            pane("w1:p2", true),
                            pane("w1:p3", true),
                        ],
                    ),
                    tab("w1:t2", false, vec![pane("w1:p9", true)]),
                ],
            )],
        );

        assert_eq!(strip_of(&t).split_pane_id.as_deref(), Some("w1:p2"));
    }

    #[test]
    fn split_falls_back_to_the_first_pane_when_none_is_focused() {
        let t = tree(
            None,
            vec![workspace(
                "w1",
                true,
                vec![tab(
                    "w1:t1",
                    true,
                    vec![pane("w1:p1", false), pane("w1:p2", false)],
                )],
            )],
        );

        assert_eq!(strip_of(&t).split_pane_id.as_deref(), Some("w1:p1"));
    }

    #[test]
    fn split_is_none_for_an_active_tab_without_panes() {
        let t = tree(
            None,
            vec![workspace("w1", true, vec![tab("w1:t1", true, vec![])])],
        );

        let strip = strip_of(&t);

        assert_eq!(active_ids(&strip), ["w1:t1"]);
        assert_eq!(strip.split_pane_id, None);
    }

    #[test]
    fn split_is_none_without_an_active_tab() {
        let t = tree(
            None,
            vec![workspace(
                "w1",
                true,
                vec![tab("w1:t1", false, vec![pane("w1:p1", true)])],
            )],
        );

        let strip = strip_of(&t);

        assert!(active_ids(&strip).is_empty());
        assert_eq!(strip.split_pane_id, None);
    }
}
