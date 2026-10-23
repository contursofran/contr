//! The agents section at the bottom of the sidebar: an `Agents` header with the running count, then
//! one row per agent in herdr's order. Rendering only: a click does nothing yet. Each row is its own
//! cached view, so a spinning `working` icon redraws only its row.

use crate::herdr::{GlobalHerdr, Herdr};
use crate::status_icon::{Status, StatusIcon};
use crate::theme::{
    AGENT_GAP, AGENT_ROW_HEIGHT, AGENT_ROW_PAD_X, AGENTS_HEADER_HEIGHT, AGENTS_MAX_HEIGHT,
    CARD_INSET_X, LINE, SIDEBAR_PAD_X, TEXT, TEXT_2, TEXT_DIM, TEXT_DIM_2,
};
use contr_herdr::{AgentRow, agent_rows, running_line};
use gpui::{
    Context, Div, Entity, IntoElement, ParentElement, Render, Role, StatefulInteractiveElement,
    StyleRefinement, Styled, Subscription, Window, div, prelude::*, px, relative, rgb,
};

/// The agents section. It is empty, with no height, until herdr is connected.
pub struct AgentsList {
    herdr: Entity<Herdr>,
    rows: Vec<Entity<AgentRowView>>,
    _observe: Subscription,
}

impl AgentsList {
    /// Needs `Herdr::init` first. Shows the agents of the current tree, and follows the connection.
    pub fn new(cx: &mut Context<Self>) -> Self {
        let herdr = cx.global::<GlobalHerdr>().0.clone();
        let observe = cx.observe(&herdr, |list, _, cx| list.sync(cx));
        let mut list = Self {
            herdr,
            rows: Vec::new(),
            _observe: observe,
        };
        list.sync(cx);
        list
    }

    /// Brings the rows up to the tree. When the panes changed, the row views are rebuilt and the
    /// list redraws. Otherwise each row view takes its new row and redraws only if it changed.
    fn sync(&mut self, cx: &mut Context<Self>) {
        let rows = match self.herdr.read(cx).live.tree.as_deref() {
            Some(tree) => agent_rows(tree, |_| None), // the board bridge is not wired yet
            None => Vec::new(),
        };
        let same_panes = rows.len() == self.rows.len()
            && rows
                .iter()
                .zip(&self.rows)
                .all(|(row, view)| view.read(cx).row.pane_id == row.pane_id);
        if same_panes {
            for (view, row) in self.rows.iter().zip(rows) {
                view.update(cx, |view, cx| view.set_row(row, cx));
            }
        } else {
            self.rows = rows
                .into_iter()
                .enumerate()
                .map(|(index, row)| cx.new(|_| AgentRowView::new(row, index)))
                .collect();
            cx.notify();
        }
    }
}

impl Render for AgentsList {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let root = div().debug_selector(|| "sidebar-agents".into()).flex_none();
        if self.herdr.read(cx).live.tree.is_none() {
            return root;
        }
        root.flex()
            .flex_col()
            .max_h(relative(AGENTS_MAX_HEIGHT))
            .border_t_1()
            .border_color(rgb(LINE))
            .child(header(self.rows.len()))
            .child(
                div()
                    .id("agents-rows")
                    .flex_1()
                    .min_h_0()
                    .overflow_y_scroll()
                    .px(px(CARD_INSET_X))
                    .children(self.rows.iter().map(|row| {
                        row.clone()
                            .cached(StyleRefinement::default().w_full().h(px(AGENT_ROW_HEIGHT)))
                    })),
            )
    }
}

/// The `Agents` label on the left and `N running` on the right.
fn header(count: usize) -> Div {
    div()
        .debug_selector(|| "agents-header".into())
        .h(px(AGENTS_HEADER_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .justify_between()
        .px(px(SIDEBAR_PAD_X))
        .text_color(rgb(TEXT_DIM))
        .child("Agents")
        .child(
            div()
                .debug_selector(move || format!("agents-count-{count}"))
                .child(running_line(count)),
        )
}

/// One agent row: the status icon, the name, the detail and, when there is one, the link.
pub struct AgentRowView {
    row: AgentRow,
    index: usize,
    /// Renders so far. The tests count them to see which rows redraw.
    renders: usize,
}

impl AgentRowView {
    fn new(row: AgentRow, index: usize) -> Self {
        Self {
            row,
            index,
            renders: 0,
        }
    }

    /// Takes the new row. Redraws only when it differs from the row shown.
    fn set_row(&mut self, row: AgentRow, cx: &mut Context<Self>) {
        if self.row != row {
            self.row = row;
            cx.notify();
        }
    }
}

impl Render for AgentRowView {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        self.renders += 1;
        let index = self.index;
        let row = &self.row;
        let status = Status::from(row.status);
        let mut label = format!("{}, {}, {}", row.name, status.label(), row.detail);
        if let Some(link) = &row.link {
            label.push_str(", ");
            label.push_str(link);
        }
        div()
            .id(("agent-row", index))
            .debug_selector(move || format!("agent-row-{index}"))
            .role(Role::Button)
            .aria_label(label)
            .w_full()
            .h(px(AGENT_ROW_HEIGHT))
            .flex()
            .items_center()
            .gap(px(AGENT_GAP))
            .px(px(AGENT_ROW_PAD_X))
            .child(StatusIcon::new(("agent-status", index), status))
            .child(
                div()
                    .min_w_0()
                    .overflow_hidden()
                    .text_ellipsis()
                    .whitespace_nowrap()
                    .text_color(rgb(TEXT))
                    .child(row.name.clone()),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .overflow_hidden()
                    .text_ellipsis()
                    .whitespace_nowrap()
                    .text_color(rgb(TEXT_DIM_2))
                    .child(row.detail.clone()),
            )
            .when_some(row.link.clone(), |line, link| {
                line.child(
                    div()
                        .debug_selector(move || format!("agent-row-{index}-link"))
                        .flex_none()
                        .whitespace_nowrap()
                        .text_color(rgb(TEXT_2))
                        .child(link),
                )
            })
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::frame::Frame;
    use crate::status_bar::init_detached;
    use crate::theme::SIDEBAR_WIDTH;
    use contr_herdr::{
        AgentInfo, AgentState, ConnState, HerdrTree, HerdrUpdate, LiveHerdr, Pane, Tab, Workspace,
    };
    use gpui::{TestAppContext, VisualTestContext, size};
    use std::sync::Arc;

    /// One workspace with one tab per entry. The tab is labelled as given, has one pane `p{i}`, and
    /// that pane runs a `claude` agent in the given state. `None` is a plain pane.
    fn tree(tabs: &[(&str, Option<AgentState>)]) -> HerdrTree {
        let tabs = tabs
            .iter()
            .enumerate()
            .map(|(i, (label, state))| Tab {
                id: format!("t{i}"),
                label: (*label).into(),
                status: AgentState::Idle,
                focused: false,
                panes: vec![Pane {
                    id: format!("p{i}"),
                    label: None,
                    cwd: None,
                    focused: false,
                    agent: state.map(|status| AgentInfo {
                        name: "claude".into(),
                        status,
                    }),
                }],
            })
            .collect();
        HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: None,
            workspaces: vec![Workspace {
                id: "w1".into(),
                label: "w1".into(),
                status: AgentState::Idle,
                focused: false,
                tabs,
            }],
        }
    }

    fn connected(tree: HerdrTree) -> LiveHerdr {
        LiveHerdr {
            state: ConnState::Connected {
                server_version: "0.9.3".into(),
            },
            tree: Some(Arc::new(tree)),
        }
    }

    /// Registers a connection in `live` as `Herdr::init` would, without a reader.
    fn herdr_in(cx: &mut TestAppContext, live: LiveHerdr) -> Entity<Herdr> {
        cx.update(|app| {
            let herdr = app.new(|cx| Herdr::detached(live, cx));
            app.set_global(GlobalHerdr(herdr.clone()));
            herdr
        })
    }

    /// Shows the list alone, in a window as wide as the sidebar.
    fn show(cx: &mut TestAppContext) -> (Entity<AgentsList>, &mut VisualTestContext) {
        let (list, cx) = cx.add_window_view(|_, cx| AgentsList::new(cx));
        cx.simulate_resize(size(px(SIDEBAR_WIDTH), px(900.)));
        cx.run_until_parked();
        (list, cx)
    }

    /// Sends an update to the connection, as the reader would.
    fn send(cx: &mut VisualTestContext, herdr: &Entity<Herdr>, update: HerdrUpdate) {
        cx.update(|_, app| herdr.update(app, |herdr, cx| herdr.apply(update, cx)));
    }

    fn tree_update(tabs: &[(&str, Option<AgentState>)]) -> HerdrUpdate {
        HerdrUpdate::Tree(Arc::new(tree(tabs)))
    }

    fn row_views(
        list: &Entity<AgentsList>,
        cx: &mut VisualTestContext,
    ) -> Vec<Entity<AgentRowView>> {
        list.read_with(cx, |list, _| list.rows.clone())
    }

    fn renders(rows: &[Entity<AgentRowView>], cx: &mut VisualTestContext) -> Vec<usize> {
        rows.iter()
            .map(|row| row.read_with(cx, |row, _| row.renders))
            .collect()
    }

    #[gpui::test]
    fn not_connected_shows_no_section(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            LiveHerdr {
                state: ConnState::NotRunning {
                    reason: "no socket".into(),
                },
                tree: None,
            },
        );
        let (_list, cx) = show(cx);

        assert!(cx.debug_bounds("agents-header").is_none());
        assert!(cx.debug_bounds("agent-row-0").is_none());
        assert!(cx.debug_bounds("sidebar-agents").is_some());
    }

    #[gpui::test]
    fn connected_with_no_agents_shows_zero_running(cx: &mut TestAppContext) {
        herdr_in(cx, connected(tree(&[("scratch", None)])));
        let (_list, cx) = show(cx);

        assert!(cx.debug_bounds("agents-header").is_some());
        assert!(cx.debug_bounds("agents-count-0").is_some());
        assert!(cx.debug_bounds("agent-row-0").is_none());
    }

    #[gpui::test]
    fn one_row_per_agent_and_the_count_of_them(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            connected(tree(&[
                ("b-1", Some(AgentState::Working)),
                ("p-2", Some(AgentState::Idle)),
                ("scratch", None),
                ("b-3", Some(AgentState::Done)),
            ])),
        );
        let (_list, cx) = show(cx);

        assert!(cx.debug_bounds("agents-count-3").is_some());
        for selector in ["agent-row-0", "agent-row-1", "agent-row-2"] {
            assert!(cx.debug_bounds(selector).is_some(), "{selector} is missing");
        }
        assert!(
            cx.debug_bounds("agent-row-3").is_none(),
            "plain pane has a row"
        );
    }

    #[gpui::test]
    fn rows_are_one_line_inside_the_side_margins(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            connected(tree(&[
                ("b-1", Some(AgentState::Working)),
                ("p-2", Some(AgentState::Idle)),
            ])),
        );
        let (_list, cx) = show(cx);

        let first = cx.debug_bounds("agent-row-0").unwrap();
        let second = cx.debug_bounds("agent-row-1").unwrap();
        assert_eq!(
            first.size,
            size(px(SIDEBAR_WIDTH - 2. * CARD_INSET_X), px(AGENT_ROW_HEIGHT))
        );
        assert_eq!(first.origin.x, px(CARD_INSET_X));
        assert_eq!(second.origin.y, first.origin.y + px(AGENT_ROW_HEIGHT));
        assert_eq!(
            cx.debug_bounds("agents-header").unwrap().size.height,
            px(AGENTS_HEADER_HEIGHT)
        );
    }

    #[gpui::test]
    fn each_state_shows_its_icon(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            connected(tree(&[
                ("a", Some(AgentState::Working)),
                ("b", Some(AgentState::Blocked)),
                ("c", Some(AgentState::Unknown)),
                ("d", Some(AgentState::Done)),
            ])),
        );
        let (_list, cx) = show(cx);

        // Unknown shows as idle (D19).
        for selector in ["icon-working", "icon-blocked", "icon-idle", "icon-done"] {
            assert!(cx.debug_bounds(selector).is_some(), "{selector} is missing");
        }
    }

    #[gpui::test]
    fn a_row_takes_its_name_and_detail_from_the_tree(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            connected(tree(&[
                ("b-1", Some(AgentState::Working)),
                ("p-2", Some(AgentState::Idle)),
            ])),
        );
        let (list, cx) = show(cx);

        let shown: Vec<(String, String, AgentState)> = row_views(&list, cx)
            .iter()
            .map(|row| {
                row.read_with(cx, |row, _| {
                    (row.row.name.clone(), row.row.detail.clone(), row.row.status)
                })
            })
            .collect();
        assert_eq!(
            shown,
            [
                ("b-1".into(), "claude".into(), AgentState::Working),
                ("p-2".into(), "claude".into(), AgentState::Idle),
            ]
        );
    }

    #[gpui::test]
    fn a_row_with_a_link_shows_it(cx: &mut TestAppContext) {
        let row = AgentRow {
            pane_id: "p0".into(),
            tab_id: "t0".into(),
            workspace_id: "w1".into(),
            name: "b-1151".into(),
            detail: "haiku 5.5 · builder".into(),
            link: Some("#640".into()),
            status: AgentState::Idle,
            focused: false,
        };
        let (_view, cx) = cx.add_window_view(|_, _| AgentRowView::new(row, 0));
        cx.simulate_resize(size(px(SIDEBAR_WIDTH), px(AGENT_ROW_HEIGHT)));
        cx.run_until_parked();

        assert!(cx.debug_bounds("agent-row-0-link").is_some());
    }

    #[gpui::test]
    fn a_row_without_a_link_shows_none(cx: &mut TestAppContext) {
        let row = AgentRow {
            pane_id: "p0".into(),
            tab_id: "t0".into(),
            workspace_id: "w1".into(),
            name: "b-1151".into(),
            detail: "claude".into(),
            link: None,
            status: AgentState::Idle,
            focused: false,
        };
        let (_view, cx) = cx.add_window_view(|_, _| AgentRowView::new(row, 0));
        cx.simulate_resize(size(px(SIDEBAR_WIDTH), px(AGENT_ROW_HEIGHT)));
        cx.run_until_parked();

        assert!(cx.debug_bounds("agent-row-0").is_some());
        assert!(cx.debug_bounds("agent-row-0-link").is_none());
    }

    #[gpui::test]
    fn rows_follow_the_panes_added_and_removed(cx: &mut TestAppContext) {
        let herdr = herdr_in(cx, connected(tree(&[("a", Some(AgentState::Idle))])));
        let (_list, cx) = show(cx);
        assert!(cx.debug_bounds("agent-row-1").is_none());

        send(
            cx,
            &herdr,
            tree_update(&[("a", Some(AgentState::Idle)), ("b", Some(AgentState::Idle))]),
        );
        cx.run_until_parked();
        assert!(cx.debug_bounds("agents-count-2").is_some());
        assert!(cx.debug_bounds("agent-row-1").is_some());

        send(cx, &herdr, tree_update(&[("b", Some(AgentState::Idle))]));
        cx.run_until_parked();
        assert!(cx.debug_bounds("agents-count-1").is_some());
        assert!(cx.debug_bounds("agent-row-1").is_none());
        assert!(cx.debug_bounds("agent-row-0").is_some());
    }

    #[gpui::test]
    fn losing_the_connection_empties_the_section_and_regaining_it_restores_it(
        cx: &mut TestAppContext,
    ) {
        let herdr = herdr_in(cx, connected(tree(&[("a", Some(AgentState::Idle))])));
        let (_list, cx) = show(cx);
        assert!(cx.debug_bounds("agent-row-0").is_some());

        send(
            cx,
            &herdr,
            HerdrUpdate::State(ConnState::NotRunning {
                reason: "gone".into(),
            }),
        );
        cx.run_until_parked();
        assert!(cx.debug_bounds("agent-row-0").is_none());
        assert!(cx.debug_bounds("agents-header").is_none());

        send(cx, &herdr, tree_update(&[("a", Some(AgentState::Idle))]));
        cx.run_until_parked();
        assert!(cx.debug_bounds("agent-row-0").is_some());
    }

    #[gpui::test]
    fn a_change_to_one_row_redraws_only_that_row(cx: &mut TestAppContext) {
        let herdr = herdr_in(
            cx,
            connected(tree(&[
                ("a", Some(AgentState::Idle)),
                ("b", Some(AgentState::Idle)),
                ("c", Some(AgentState::Idle)),
            ])),
        );
        let (list, cx) = show(cx);
        let rows = row_views(&list, cx);
        let before = renders(&rows, cx);

        send(
            cx,
            &herdr,
            tree_update(&[
                ("a", Some(AgentState::Idle)),
                ("b", Some(AgentState::Blocked)),
                ("c", Some(AgentState::Idle)),
            ]),
        );
        cx.run_until_parked();

        let after = renders(&rows, cx);
        assert_eq!(after[0], before[0], "row a redrew");
        assert_eq!(after[1], before[1] + 1, "row b did not redraw");
        assert_eq!(after[2], before[2], "row c redrew");
        assert!(cx.debug_bounds("icon-blocked").is_some());
    }

    #[gpui::test]
    fn a_spinning_row_redraws_only_itself(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            connected(tree(&[
                ("a", Some(AgentState::Working)),
                ("b", Some(AgentState::Idle)),
            ])),
        );
        let (list, cx) = show(cx);
        let rows = row_views(&list, cx);
        let before = renders(&rows, cx);
        for _ in 0..3 {
            cx.update(|w, cx| w.simulate_next_frame(cx));
            cx.run_until_parked();
        }

        let after = renders(&rows, cx);
        assert!(after[0] > before[0], "working row did not redraw");
        assert_eq!(after[1], before[1], "idle row re-rendered");
    }

    #[gpui::test]
    fn not_connected_takes_no_height_in_the_sidebar(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            LiveHerdr {
                state: ConnState::NotRunning {
                    reason: "no socket".into(),
                },
                tree: None,
            },
        );
        cx.update(|app| init_detached(app, None));
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        assert_eq!(
            cx.debug_bounds("sidebar-agents").unwrap().size.height,
            px(0.)
        );
        let sidebar = cx.debug_bounds("sidebar").unwrap();
        let spaces = cx.debug_bounds("spaces-body").unwrap();
        assert_eq!(
            spaces.origin.y + spaces.size.height,
            sidebar.origin.y + sidebar.size.height,
            "the spaces take the whole sidebar"
        );
    }

    #[gpui::test]
    fn the_section_sits_at_the_sidebar_bottom(cx: &mut TestAppContext) {
        herdr_in(
            cx,
            connected(tree(&[
                ("a", Some(AgentState::Idle)),
                ("b", Some(AgentState::Idle)),
            ])),
        );
        cx.update(|app| init_detached(app, None));
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        let sidebar = cx.debug_bounds("sidebar").unwrap();
        let section = cx.debug_bounds("sidebar-agents").unwrap();
        let spaces = cx.debug_bounds("spaces-body").unwrap();
        assert_eq!(
            section.origin.y + section.size.height,
            sidebar.origin.y + sidebar.size.height
        );
        assert!(spaces.origin.y + spaces.size.height <= section.origin.y);
    }

    #[gpui::test]
    fn the_section_is_at_most_45_percent_of_the_sidebar(cx: &mut TestAppContext) {
        let many: Vec<(String, Option<AgentState>)> = (0..30)
            .map(|i| (format!("t-{i}"), Some(AgentState::Idle)))
            .collect();
        let labels: Vec<(&str, Option<AgentState>)> = many
            .iter()
            .map(|(label, state)| (label.as_str(), *state))
            .collect();
        herdr_in(cx, connected(tree(&labels)));
        cx.update(|app| init_detached(app, None));
        let (_view, cx) = cx.add_window_view(|_, cx| Frame::new(cx));
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();

        let sidebar = cx.debug_bounds("sidebar").unwrap();
        let section = cx.debug_bounds("sidebar-agents").unwrap();
        let limit = f32::from(sidebar.size.height) * AGENTS_MAX_HEIGHT;
        assert!(
            (f32::from(section.size.height) - limit).abs() < 1.,
            "section is {:?}, limit {limit}",
            section.size.height
        );
    }
}
