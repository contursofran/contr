package app

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// tabSpec is one tab of the test tree: its label, and the state of the agent
// in its one pane. A nil agent is a plain pane.
type tabSpec struct {
	label string
	agent *herdr.AgentState
}

func st(s herdr.AgentState) *herdr.AgentState { return &s }

func agentTab(label string, s herdr.AgentState) tabSpec { return tabSpec{label, st(s)} }

// agentsTree is one workspace with one tab per spec. Tab i has one pane p{i}.
func agentsTree(specs ...tabSpec) herdr.HerdrTree {
	var tabs []herdr.Tab
	for i, s := range specs {
		p := herdr.Pane{ID: fmt.Sprintf("p%d", i), Tokens: map[string]string{}}
		if s.agent != nil {
			p.Agent = &herdr.AgentInfo{Name: "claude", Status: *s.agent}
		}
		tabs = append(tabs, herdr.Tab{ID: fmt.Sprintf("t%d", i), Label: s.label, Status: herdr.StateIdle, Panes: []herdr.Pane{p}})
	}
	return herdr.HerdrTree{Version: "0.9.3", Workspaces: []herdr.Workspace{{ID: "w1", Label: "w1", Status: herdr.StateIdle, Tabs: tabs}}}
}

func idleTabs(labels ...string) []tabSpec {
	var specs []tabSpec
	for _, l := range labels {
		specs = append(specs, agentTab(l, herdr.StateIdle))
	}
	return specs
}

// agentsHarness shows the list alone in a window as wide as the sidebar.
type agentsHarness struct {
	t       *testing.T
	live    *fakeLive
	sel     *state.Selection
	list    *AgentsList
	board   *Board
	tt      *ui.Tester
	focused []string
}

func newAgents(t *testing.T, live herdr.LiveHerdr) *agentsHarness {
	t.Helper()
	h := &agentsHarness{t: t, live: &fakeLive{live: live}, sel: state.NewSelection(nil), board: NewDetachedBoard()}
	h.list = NewAgentsList(h.live, h.board, h.sel, nil)
	h.list.focus = func(id string, _ func(string)) { h.focused = append(h.focused, id) }
	h.tt = ui.NewTester(h.list.View, theme.SidebarWidth, theme.WindowHeight)
	return h
}

func agentsOf(specs ...tabSpec) herdr.LiveHerdr { return connectedWith(agentsTree(specs...)) }

// send replaces the live state, as the reader would, and draws a frame. It
// returns what the list's sync changed.
func (h *agentsHarness) send(l herdr.LiveHerdr) AgentsChange {
	h.t.Helper()
	h.live.setLive(l)
	change := h.list.Sync()
	h.tt.Frame()
	return change
}

// row is the label of row i, as it shows now.
func (h *agentsHarness) row(i int) string {
	h.t.Helper()
	rows := h.list.Rows()
	if i >= len(rows) {
		return "(no row)"
	}
	selected, _ := h.list.selectedPane()
	return AgentRowLabel(rows[i], rows[i].PaneID == selected)
}

func (h *agentsHarness) has(label string) bool { _, ok := h.tt.Find(label); return ok }

func (h *agentsHarness) rect(label string) ui.Rect { return box(h.t, h.tt, label) }

func (h *agentsHarness) click(i int) {
	h.t.Helper()
	if err := h.tt.Click(h.row(i)); err != nil {
		h.t.Fatal(err)
	}
}

// selected tells whether row i shows the card.
func (h *agentsHarness) selected(i int) bool {
	h.t.Helper()
	return strings.HasSuffix(h.row(i), ", selected") && h.has(h.row(i))
}

func (h *agentsHarness) selection() (state.Selected, bool) { return h.sel.Current() }

func (h *agentsHarness) pick(s state.Selected) {
	h.sel.Select(s)
	h.tt.Frame()
}

func (h *agentsHarness) builds() map[string]int {
	out := map[string]int{}
	for k, v := range h.list.builds {
		out[k] = v
	}
	return out
}

func notRunningLive() herdr.LiveHerdr { return herdr.LiveHerdr{State: notRunning()} }

func running(n int) string { return herdr.RunningLine(n) }

// Port of the Rust test not_connected_shows_no_section.
func TestNotConnectedShowsNoSection(t *testing.T) {
	h := newAgents(t, notRunningLive())
	if h.has(LabelAgentsHeader) || h.has(LabelAgents) || h.has("Agents") {
		t.Errorf("the section shows; texts %q", h.tt.Texts())
	}
}

// Port of connected_with_no_agents_shows_zero_running.
func TestConnectedWithNoAgentsShowsZeroRunning(t *testing.T) {
	h := newAgents(t, agentsOf(tabSpec{label: "scratch"}))
	if !h.has(LabelAgentsHeader) || !h.has(running(0)) {
		t.Errorf("want the header with 0 running; texts %q", h.tt.Texts())
	}
	if len(h.list.Rows()) != 0 {
		t.Error("a plain pane has a row")
	}
}

// Port of one_row_per_agent_and_the_count_of_them.
func TestOneRowPerAgentAndTheCountOfThem(t *testing.T) {
	h := newAgents(t, agentsOf(
		agentTab("b-1", herdr.StateWorking), agentTab("p-2", herdr.StateIdle),
		tabSpec{label: "scratch"}, agentTab("b-3", herdr.StateDone)))
	if !h.has(running(3)) {
		t.Errorf("want 3 running; texts %q", h.tt.Texts())
	}
	rows := h.list.Rows()
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	for i := range rows {
		if !h.has(h.row(i)) {
			t.Errorf("row %d is missing", i)
		}
	}
	if h.has("scratch, idle, claude") {
		t.Error("the plain pane has a row")
	}
}

// Port of rows_are_one_line_inside_the_side_margins.
func TestRowsAreOneLineInsideTheSideMargins(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("b-1", herdr.StateWorking), agentTab("p-2", herdr.StateIdle)))
	first, second := h.rect(h.row(0)), h.rect(h.row(1))
	if first.W != theme.SidebarWidth-2*theme.CardInsetX || first.H != theme.AgentRowHeight {
		t.Errorf("row %+v, want %d x %d", first, theme.SidebarWidth-2*theme.CardInsetX, theme.AgentRowHeight)
	}
	if first.X != theme.CardInsetX {
		t.Errorf("row starts at %v, want %d", first.X, theme.CardInsetX)
	}
	if second.Y != first.Y+theme.AgentRowHeight {
		t.Errorf("second row at %v, want %v", second.Y, first.Y+theme.AgentRowHeight)
	}
	if got := h.rect(LabelAgentsHeader).H; got != theme.AgentsHeaderHeight {
		t.Errorf("header is %v high, want %d", got, theme.AgentsHeaderHeight)
	}
}

// Port of each_state_shows_its_icon.
func TestEachStateShowsItsIcon(t *testing.T) {
	h := newAgents(t, agentsOf(
		agentTab("a", herdr.StateWorking), agentTab("b", herdr.StateBlocked),
		agentTab("c", herdr.StateUnknown), agentTab("d", herdr.StateDone)))
	// Unknown shows as idle (D19).
	for _, icon := range []string{"working", "blocked", "idle", "done"} {
		if !h.has(icon) {
			t.Errorf("icon %q is missing", icon)
		}
	}
}

// Port of a_row_takes_its_name_and_detail_from_the_tree.
func TestARowTakesItsNameAndDetailFromTheTree(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("b-1", herdr.StateWorking), agentTab("p-2", herdr.StateIdle)))
	type shown struct {
		name, detail string
		status       herdr.AgentState
	}
	var got []shown
	for _, r := range h.list.Rows() {
		got = append(got, shown{r.Name, r.Detail, r.Status})
	}
	want := []shown{{"b-1", "claude", herdr.StateWorking}, {"p-2", "claude", herdr.StateIdle}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
}

func sampleAgentRow(link *string) herdr.AgentRow {
	return herdr.AgentRow{
		PaneID: "p0", TabID: "t0", WorkspaceID: "w1", Name: "b-1151",
		Detail: "haiku 5.5 · builder", Link: link, Status: herdr.StateIdle,
	}
}

// rowAlone shows one row, built the way the list builds it.
func rowAlone(t *testing.T, row herdr.AgentRow) *ui.Tester {
	t.Helper()
	l := NewAgentsList(nil, nil, nil, nil)
	return ui.NewTester(func(c *ui.Context) { l.row(c, row, false) }, theme.SidebarWidth, theme.AgentRowHeight)
}

// Port of a_row_with_a_link_shows_it.
func TestARowWithALinkShowsIt(t *testing.T) {
	link := "#640"
	tt := rowAlone(t, sampleAgentRow(&link))
	if !tt.HasText("#640") {
		t.Errorf("the link is missing; texts %q", tt.Texts())
	}
}

// Port of a_row_without_a_link_shows_none.
func TestARowWithoutALinkShowsNone(t *testing.T) {
	row := sampleAgentRow(nil)
	row.Detail = "claude"
	tt := rowAlone(t, row)
	if _, ok := tt.Find(AgentRowLabel(row, false)); !ok {
		t.Fatalf("the row is missing; texts %q", tt.Texts())
	}
	if tt.HasText("#640") || len(tt.Texts()) != 4 {
		t.Errorf("texts %q, want the label, the icon, the name and the detail only", tt.Texts())
	}
}

// Port of rows_follow_the_panes_added_and_removed.
func TestRowsFollowThePanesAddedAndRemoved(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("a", herdr.StateIdle)))
	if len(h.list.Rows()) != 1 {
		t.Fatal("want one row")
	}
	h.send(agentsOf(agentTab("a", herdr.StateIdle), agentTab("b", herdr.StateIdle)))
	if !h.has(running(2)) || !h.has(h.row(1)) {
		t.Errorf("want 2 rows; texts %q", h.tt.Texts())
	}
	h.send(agentsOf(agentTab("b", herdr.StateIdle)))
	if !h.has(running(1)) || h.has("a, idle, claude") || !h.has("b, idle, claude") {
		t.Errorf("want only b; texts %q", h.tt.Texts())
	}
}

// Port of losing_the_connection_empties_the_section_and_regaining_it_restores_it.
func TestLosingTheConnectionEmptiesTheSectionAndRegainingItRestoresIt(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("a", herdr.StateIdle)))
	if !h.has(h.row(0)) {
		t.Fatal("the row should show")
	}
	h.send(notRunningLive())
	if h.has("a, idle, claude") || h.has(LabelAgentsHeader) {
		t.Errorf("the section still shows; texts %q", h.tt.Texts())
	}
	h.send(agentsOf(agentTab("a", herdr.StateIdle)))
	if !h.has("a, idle, claude") {
		t.Error("the row did not come back")
	}
}

// Port of a_change_to_one_row_redraws_only_that_row. MyGo builds the whole
// list for a redraw, so what is checked is what Sync reports: only row 1
// changed, and the list was not rebuilt.
func TestAChangeToOneRowRedrawsOnlyThatRow(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("a", herdr.StateIdle), agentTab("b", herdr.StateIdle), agentTab("c", herdr.StateIdle)))
	change := h.send(agentsOf(agentTab("a", herdr.StateIdle), agentTab("b", herdr.StateBlocked), agentTab("c", herdr.StateIdle)))
	if change.Rebuilt || !reflect.DeepEqual(change.Rows, []int{1}) {
		t.Errorf("change %+v, want row 1 only", change)
	}
	if !h.has("blocked") {
		t.Error("the blocked icon is missing")
	}
	if again := h.send(agentsOf(agentTab("a", herdr.StateIdle), agentTab("b", herdr.StateBlocked), agentTab("c", herdr.StateIdle))); again.Any() {
		t.Errorf("the same tree changed %+v", again)
	}
}

// Port of a_spinning_row_redraws_only_itself. The working icon draws its own
// turn: frames of the turn build no row, so no row view runs for them.
func TestASpinningRowRedrawsOnlyItself(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("a", herdr.StateWorking), agentTab("b", herdr.StateIdle)))
	before := h.builds()
	for i := 0; i < 3; i++ {
		h.tt.Advance(100 * time.Millisecond)
	}
	if !reflect.DeepEqual(h.builds(), before) {
		t.Errorf("builds %v -> %v: frames of the turn built rows", before, h.builds())
	}
	if !h.tt.FramePending() {
		t.Error("the working icon should ask for frames")
	}
	idle := newAgents(t, agentsOf(agentTab("a", herdr.StateIdle)))
	idle.tt.Advance(100 * time.Millisecond)
	if idle.tt.FramePending() {
		t.Error("a list with no working agent asks for frames")
	}
}

// frameWith shows the whole frame with the agents list in the sidebar.
func frameWith(t *testing.T, live herdr.LiveHerdr) (*Frame, *ui.Tester) {
	t.Helper()
	src := &fakeLive{live: live}
	f := NewFrame(src)
	f.Slots.Agents = NewAgentsList(src, nil, state.NewSelection(nil), nil).View
	return f, ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
}

// Port of not_connected_takes_no_height_in_the_sidebar.
func TestNotConnectedTakesNoHeightInTheSidebar(t *testing.T) {
	_, tt := frameWith(t, notRunningLive())
	if r, ok := tt.Find(LabelAgents); ok && r.H != 0 {
		t.Errorf("the section is %v high", r.H)
	}
	sidebar := box(t, tt, LabelSidebar)
	if wrapper := box(t, tt, LabelSidebar); wrapper.H != sidebar.H {
		t.Error("the sidebar changed size")
	}
}

// Port of the_section_sits_at_the_sidebar_bottom.
func TestTheSectionSitsAtTheSidebarBottom(t *testing.T) {
	_, tt := frameWith(t, agentsOf(agentTab("a", herdr.StateIdle), agentTab("b", herdr.StateIdle)))
	sidebar, section := box(t, tt, LabelSidebar), box(t, tt, LabelAgents)
	if section.Y+section.H != sidebar.Y+sidebar.H {
		t.Errorf("section %+v does not end at the sidebar %+v", section, sidebar)
	}
	want := float32(1 + theme.AgentsHeaderHeight + 2*theme.AgentRowHeight)
	if section.H != want {
		t.Errorf("section is %v high, want %v", section.H, want)
	}
}

// Port of the_section_is_at_most_45_percent_of_the_sidebar.
func TestTheSectionIsAtMost45PercentOfTheSidebar(t *testing.T) {
	var specs []tabSpec
	for i := 0; i < 30; i++ {
		specs = append(specs, agentTab(fmt.Sprintf("t-%d", i), herdr.StateIdle))
	}
	_, tt := frameWith(t, agentsOf(specs...))
	sidebar, section := box(t, tt, LabelSidebar), box(t, tt, LabelAgents)
	limit := sidebar.H * theme.AgentsMaxShare
	if d := section.H - limit; d < -1 || d > 1 {
		t.Errorf("section is %v high, limit %v", section.H, limit)
	}
}

// Port of a_click_selects_the_row_and_focuses_its_pane.
func TestAClickSelectsTheRowAndFocusesItsPane(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("a", herdr.StateIdle), agentTab("b", herdr.StateWorking)))
	h.click(1)
	if !reflect.DeepEqual(h.focused, []string{"p1"}) {
		t.Errorf("focused %v, want p1", h.focused)
	}
	if sel, ok := h.selection(); !ok || sel != (state.Selected{Kind: state.KindAgent, ID: "p1"}) {
		t.Errorf("selection %+v", sel)
	}
	if !h.selected(1) || h.selected(0) {
		t.Errorf("card on the wrong rows; texts %q", h.tt.Texts())
	}
}

// Port of a_second_click_on_the_selected_row_focuses_it_again.
func TestASecondClickOnTheSelectedRowFocusesItAgain(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b")...))
	h.click(0)
	h.click(0)
	if !reflect.DeepEqual(h.focused, []string{"p0", "p0"}) {
		t.Errorf("focused %v, want p0 twice", h.focused)
	}
	if sel, _ := h.selection(); sel.ID != "p0" || !h.selected(0) {
		t.Errorf("selection %+v, card %v", sel, h.selected(0))
	}
}

// Port of the_last_clicked_row_is_the_selection.
func TestTheLastClickedRowIsTheSelection(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b", "c")...))
	h.click(0)
	h.click(2)
	if !reflect.DeepEqual(h.focused, []string{"p0", "p2"}) {
		t.Errorf("focused %v", h.focused)
	}
	if h.selected(0) || h.selected(1) || !h.selected(2) {
		t.Errorf("card on the wrong rows; texts %q", h.tt.Texts())
	}
}

// Port of a_new_selection_redraws_only_the_rows_whose_mark_changed. A
// selection does not rebuild the list, and the card moves from row to row.
func TestANewSelectionRedrawsOnlyTheRowsWhoseMarkChanged(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b", "c")...))
	h.pick(state.Selected{Kind: state.KindAgent, ID: "p1"})
	if h.selected(0) || !h.selected(1) || h.selected(2) {
		t.Errorf("want the card on row 1; texts %q", h.tt.Texts())
	}
	if h.list.Sync().Any() {
		t.Error("a selection changed the rows")
	}
	h.pick(state.Selected{Kind: state.KindAgent, ID: "p2"})
	if h.selected(0) || h.selected(1) || !h.selected(2) {
		t.Errorf("want the card on row 2; texts %q", h.tt.Texts())
	}
}

// Port of a_selection_from_elsewhere_clears_the_agent_card.
func TestASelectionFromElsewhereClearsTheAgentCard(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b")...))
	h.click(1)
	h.pick(state.Selected{Kind: state.KindWorktree, ID: "x"})
	if sel, _ := h.selection(); sel != (state.Selected{Kind: state.KindWorktree, ID: "x"}) {
		t.Errorf("selection %+v", sel)
	}
	if h.selected(1) {
		t.Error("the card stayed")
	}
}

// Port of a_selected_agent_that_leaves_the_tree_is_deselected.
func TestASelectedAgentThatLeavesTheTreeIsDeselected(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b")...))
	h.click(1)
	h.send(agentsOf(idleTabs("a")...))
	if sel, ok := h.selection(); ok {
		t.Errorf("selection %+v, want none", sel)
	}
	if h.selected(0) {
		t.Error("a card shows")
	}
}

// Port of a_worktree_selection_survives_a_tree_without_agents.
func TestAWorktreeSelectionSurvivesATreeWithoutAgents(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b")...))
	h.pick(state.Selected{Kind: state.KindWorktree, ID: "x"})
	h.send(agentsOf(tabSpec{label: "a"}))
	if sel, _ := h.selection(); sel != (state.Selected{Kind: state.KindWorktree, ID: "x"}) {
		t.Errorf("selection %+v, want the worktree", sel)
	}
}

// Port of losing_the_connection_keeps_the_selection_for_its_return.
func TestLosingTheConnectionKeepsTheSelectionForItsReturn(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b")...))
	h.click(1)
	h.send(notRunningLive())
	if sel, _ := h.selection(); sel.ID != "p1" {
		t.Errorf("selection %+v, want p1", sel)
	}
	h.send(agentsOf(idleTabs("a", "b")...))
	if !h.selected(1) {
		t.Error("the card did not come back")
	}
}

// Port of a_tree_that_adds_a_pane_keeps_the_card_on_its_row.
func TestATreeThatAddsAPaneKeepsTheCardOnItsRow(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a", "b")...))
	h.click(1)
	h.send(agentsOf(idleTabs("a", "b", "c")...))
	if !h.has(running(3)) || h.selected(0) || !h.selected(1) || h.selected(2) {
		t.Errorf("card on the wrong rows; texts %q", h.tt.Texts())
	}
}

// Port of clicks_still_focus_after_the_rows_are_rebuilt.
func TestClicksStillFocusAfterTheRowsAreRebuilt(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("a")...))
	h.send(agentsOf(idleTabs("a", "b")...))
	h.click(1)
	if !reflect.DeepEqual(h.focused, []string{"p1"}) {
		t.Errorf("focused %v, want p1", h.focused)
	}
}

// Port of a_selected_agent_shows_its_card_when_the_list_opens.
func TestASelectedAgentShowsItsCardWhenTheListOpens(t *testing.T) {
	sel := state.NewSelection(nil)
	sel.Select(state.Selected{Kind: state.KindAgent, ID: "p1"})
	h := &agentsHarness{t: t, live: &fakeLive{live: agentsOf(idleTabs("a", "b")...)}, sel: sel, board: NewDetachedBoard()}
	h.list = NewAgentsList(h.live, h.board, sel, nil)
	h.tt = ui.NewTester(h.list.View, theme.SidebarWidth, theme.WindowHeight)
	if !h.selected(1) || h.selected(0) {
		t.Errorf("card on the wrong rows; texts %q", h.tt.Texts())
	}
}

// Port of without_a_board_rows_use_the_tab_name.
func TestWithoutABoardRowsUseTheTabName(t *testing.T) {
	h := newAgents(t, agentsOf(agentTab("b-1", herdr.StateWorking)))
	row := h.list.Rows()[0]
	if row.Name != "b-1" || row.Detail != "claude" || row.Link != nil {
		t.Errorf("row %+v", row)
	}
	list := NewAgentsList(h.live, nil, nil, nil)
	if r := list.Rows()[0]; r.Name != "b-1" {
		t.Errorf("a list with no board shows %+v", r)
	}
}

// putBoard puts the v1 board json in b as a finished read would.
func putBoard(t *testing.T, b *Board, json string) {
	t.Helper()
	snap, err := board.Parse([]byte(json))
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.snap = snap
	b.mu.Unlock()
}

func boardJSON(agents, rows string) string {
	return fmt.Sprintf(`{"version": 1, "session": "s", "rev": 1, "agents": %s, "rows": %s}`, agents, rows)
}

// Port of bridge_names_roles_and_models_replace_the_fallback.
func TestBridgeNamesRolesAndModelsReplaceTheFallback(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("tab-a", "tab-b")...))
	agents := `[{"pane_id": "p0", "name": "b-1151", "model": "haiku 5.5", "role": "builder"}]`
	rows := `[{"key": "k", "label": "#640", "pane_id": "p0"}]`
	putBoard(t, h.board, boardJSON(agents, rows))
	h.send(agentsOf(idleTabs("tab-a", "tab-b")...))
	first, second := h.list.Rows()[0], h.list.Rows()[1]
	if first.Name != "b-1151" || first.Detail != "haiku 5.5 · builder" || first.Link == nil || *first.Link != "#640" {
		t.Errorf("first row %+v", first)
	}
	if !h.tt.HasText("#640") {
		t.Error("the link is not shown")
	}
	if second.Name != "tab-b" || second.Detail != "claude" || second.Link != nil {
		t.Errorf("second row %+v", second)
	}
}

// Port of a_new_board_redraws_only_the_changed_row.
func TestANewBoardRedrawsOnlyTheChangedRow(t *testing.T) {
	h := newAgents(t, agentsOf(idleTabs("tab-a", "tab-b")...))
	first := boardJSON(`[{"pane_id": "p0", "name": "b-1151"}]`, "[]")
	putBoard(t, h.board, first)
	h.list.Sync()
	second := boardJSON(`[{"pane_id": "p0", "name": "b-1151"}, {"pane_id": "p1", "role": "reviewer"}]`, "[]")
	putBoard(t, h.board, second)
	if change := h.list.Sync(); change.Rebuilt || !reflect.DeepEqual(change.Rows, []int{1}) {
		t.Errorf("change %+v, want row 1 only", change)
	}
	putBoard(t, h.board, second)
	if change := h.list.Sync(); change.Any() {
		t.Errorf("an unchanged board changed %+v", change)
	}
}

// Port of a_row_label_has_its_link.
func TestARowLabelHasItsLink(t *testing.T) {
	link := "#640"
	if got := AgentRowLabel(sampleAgentRow(&link), false); got != "b-1151, idle, haiku 5.5 · builder, #640" {
		t.Errorf("label %q", got)
	}
	if got := AgentRowLabel(sampleAgentRow(nil), false); got != "b-1151, idle, haiku 5.5 · builder" {
		t.Errorf("label %q", got)
	}
}

// Port of the_selected_row_says_so_in_its_label.
func TestTheSelectedRowSaysSoInItsLabel(t *testing.T) {
	link := "#640"
	row := sampleAgentRow(&link)
	if got := AgentRowLabel(row, true); got != "b-1151, idle, haiku 5.5 · builder, #640, selected" {
		t.Errorf("label %q", got)
	}
	if strings.Contains(AgentRowLabel(row, false), "selected") {
		t.Error("an unselected row says selected")
	}
}

// ---- Not in Rust: focus errors and the wiring ----

func TestAFocusThatFailsReachesTheStatusMessage(t *testing.T) {
	var lines []string
	missing := filepath.Join(t.TempDir(), "missing.sock")
	focusPane(func() (string, bool) { return missing, true }, "p1", func(s string) { lines = append(lines, s) })
	if len(lines) != 1 || lines[0] == "" {
		t.Errorf("lines %q, want one failure line", lines)
	}
}

func TestAFocusWithNoSocketPathSendsNothing(t *testing.T) {
	focusPane(func() (string, bool) { return "", false }, "p1", func(s string) { t.Errorf("reported %q", s) })
}

func TestActivateGivesTheStatusMessageToTheFocus(t *testing.T) {
	status := state.NewStatusMessage(nil)
	l := NewAgentsList(nil, nil, state.NewSelection(nil), status)
	l.focus = func(_ string, report func(string)) { report("herdr is not running") }
	l.Activate("p1")
	if text, ok := status.Text(); !ok || text != "herdr is not running" {
		t.Errorf("status %q, %v", text, ok)
	}
}

func TestAttachMakesTheBoardAndTheAgentsList(t *testing.T) {
	// Update only queues: the test runs the sync itself on this goroutine.
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	defer stop()
	if f.Board == nil || f.AgentsList == nil || f.Slots.Agents == nil {
		t.Fatal("Attach should make the board and the agents list")
	}
	// The board follows a tree that herdr sent: the window runs the sync.
	f.syncHerdr()
	if v := f.Board.View(); v.Bridge != nil {
		t.Errorf("bridge %+v, want none without a tree", v.Bridge)
	}
}

func TestAttachPutsTheBoardPanelInThePanelSlot(t *testing.T) {
	// A board directory that holds nothing: the board has no bridge.
	t.Setenv(board.DirEnv, t.TempDir())
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	defer stop()
	if f.Panel == nil || f.Slots.Panel == nil {
		t.Fatal("Attach should make the board panel and put it in the panel slot")
	}
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	panel, ok := tt.Find(LabelPanel)
	header, hok := tt.Find(LabelBoardHeader)
	if !ok || !hok || header.X < panel.X || header.X+header.W > panel.X+panel.W {
		t.Errorf("the board panel box %v should hold the board header %v", panel, header)
	}
	if !tt.HasText(EmptyText(false, "")) {
		t.Errorf("with no bridge the panel should show %q", EmptyText(false, ""))
	}
}
