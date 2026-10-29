package herdr

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// modelSnapshot builds a snapshot from the array bodies (no brackets) of
// workspaces, tabs and panes.
func modelSnapshot(t *testing.T, workspaces, tabs, panes string) WireSnapshot {
	t.Helper()
	return parseSnapshot(t, fmt.Sprintf(
		`{"version":"0.9.3","protocol":22,"workspaces":[%s],"tabs":[%s],"panes":[%s]}`,
		workspaces, tabs, panes))
}

// twoAgents has workspace w1, tab t1, and panes p1 (claude) and p2 (codex)
// in the given states.
func twoAgents(t *testing.T, p1, p2 string) WireSnapshot {
	return modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"working"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}`,
		fmt.Sprintf(`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":%q},
			{"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent":"codex","agent_status":%q}`, p1, p2))
}

func tree(s WireSnapshot) HerdrTree { return TreeFromSnapshot(&s) }

func str(s string) *string { return &s }

func state(s AgentState) *AgentState { return &s }

func wantStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGroupsTabsAndPanesUnderTheirParentsInOrder(t *testing.T) {
	s := modelSnapshot(t,
		`{"workspace_id":"w1","label":"one","agent_status":"idle"},
		 {"workspace_id":"w2","label":"two","agent_status":"working","focused":true}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"a","agent_status":"idle"},
		 {"tab_id":"t2","workspace_id":"w2","label":"b","agent_status":"working"},
		 {"tab_id":"t3","workspace_id":"w1","label":"c","agent_status":"done"}`,
		`{"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
		 {"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
		 {"pane_id":"p3","tab_id":"t2","workspace_id":"w2","agent_status":"unknown","focused":true},
		 {"pane_id":"p4","tab_id":"t3","workspace_id":"w1","agent_status":"unknown"}`)
	s.FocusedPaneID = str("p3")

	tr := tree(s)

	if tr.Version != "0.9.3" || tr.FocusedPaneID == nil || *tr.FocusedPaneID != "p3" {
		t.Fatalf("%q %v", tr.Version, tr.FocusedPaneID)
	}
	var ws, w1Tabs, t1Panes []string
	for _, w := range tr.Workspaces {
		ws = append(ws, w.ID)
	}
	for _, tab := range tr.Workspaces[0].Tabs {
		w1Tabs = append(w1Tabs, tab.ID)
	}
	for _, p := range tr.Workspaces[0].Tabs[0].Panes {
		t1Panes = append(t1Panes, p.ID)
	}
	wantStrings(t, ws, []string{"w1", "w2"})
	wantStrings(t, w1Tabs, []string{"t1", "t3"})
	wantStrings(t, t1Panes, []string{"p2", "p1"})
	if !tr.Workspaces[1].Focused {
		t.Error("w2 not focused")
	}
	if tr.Workspaces[0].Tabs[1].Panes[0].ID != "p4" {
		t.Error("p4")
	}
	p3 := tr.Workspaces[1].Tabs[0].Panes[0]
	if p3.ID != "p3" || !p3.Focused {
		t.Errorf("%+v", p3)
	}
	if tr.TabCount() != 3 || tr.PaneCount() != 4 {
		t.Errorf("%d %d", tr.TabCount(), tr.PaneCount())
	}
}

func TestDropsTabsAndPanesWhoseParentIsMissing(t *testing.T) {
	s := modelSnapshot(t,
		`{"workspace_id":"w1","label":"one","agent_status":"idle"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"idle"},
		 {"tab_id":"t9","workspace_id":"gone","label":"orphan","agent_status":"idle"}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
		 {"pane_id":"p9","tab_id":"gone-tab","workspace_id":"w1","agent_status":"unknown"},
		 {"pane_id":"p8","tab_id":"t9","workspace_id":"gone","agent_status":"unknown"}`)

	tr := tree(s)

	if tr.TabCount() != 1 || tr.PaneCount() != 1 {
		t.Errorf("%d %d", tr.TabCount(), tr.PaneCount())
	}
	wantStrings(t, tr.PaneIDs(), []string{"p1"})
}

func TestJoinsAgentByPaneAndPrefersDisplayAgent(t *testing.T) {
	s := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"working"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","display_agent":"Claude Code","agent_status":"working"},
		 {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent":"codex","agent_status":"blocked"},
		 {"pane_id":"p3","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}`)

	tr := tree(s)
	panes := tr.Workspaces[0].Tabs[0].Panes

	if want := (&AgentInfo{Name: "Claude Code", Status: StateWorking}); !reflect.DeepEqual(panes[0].Agent, want) {
		t.Errorf("%+v", panes[0].Agent)
	}
	if want := (&AgentInfo{Name: "codex", Status: StateBlocked}); !reflect.DeepEqual(panes[1].Agent, want) {
		t.Errorf("%+v", panes[1].Agent)
	}
	if panes[2].Agent != nil {
		t.Errorf("%+v", panes[2].Agent)
	}
	if tr.PaneCount() != 3 {
		t.Error(tr.PaneCount())
	}
}

func TestAPaneWithoutAnAgentNameHasNoAgentWhateverItsStatus(t *testing.T) {
	s := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"working"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"working"}`)

	if tree(s).Workspaces[0].Tabs[0].Panes[0].Agent != nil {
		t.Error("agent present")
	}
}

func TestMapsEachHerdrStatusOneToOne(t *testing.T) {
	words := []string{"idle", "working", "blocked", "done", "unknown"}
	states := []AgentState{StateIdle, StateWorking, StateBlocked, StateDone, StateUnknown}
	for i, word := range words {
		s := modelSnapshot(t,
			fmt.Sprintf(`{"workspace_id":"w1","label":"contr","agent_status":%q}`, word),
			fmt.Sprintf(`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":%q}`, word),
			fmt.Sprintf(`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":%q}`, word))

		tr := tree(s)

		want := states[i]
		if want.Label() != word {
			t.Errorf("label %q != %q", want.Label(), word)
		}
		ws := tr.Workspaces[0]
		if ws.Status != want || ws.Tabs[0].Status != want || ws.Tabs[0].Panes[0].Agent.Status != want {
			t.Errorf("%s: %v %v %v", word, ws.Status, ws.Tabs[0].Status, ws.Tabs[0].Panes[0].Agent.Status)
		}
	}
}

func TestStatusChangesReportsOnlyThePaneThatChanged(t *testing.T) {
	before := twoAgents(t, "idle", "working")
	after := twoAgents(t, "idle", "blocked")

	changes := tree(before).StatusChanges(tree(after))

	want := []StatusChange{{PaneID: "p2", Agent: str("codex"), From: state(StateWorking), To: state(StateBlocked)}}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("%+v", changes)
	}
}

func TestAnAgentAppearingIsAChangeFromNone(t *testing.T) {
	before := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"unknown"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"unknown"}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}`)
	after := twoAgents(t, "idle", "idle")

	changes := tree(before).StatusChanges(tree(after))

	want := []StatusChange{{PaneID: "p1", Agent: str("claude"), To: state(StateIdle)}}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("%+v", changes)
	}
}

func TestAnAgentLeavingAPaneIsAChangeToNoneNamedAsBefore(t *testing.T) {
	before := twoAgents(t, "idle", "working")
	after := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"idle"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"idle"}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"idle"},
		 {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}`)

	changes := tree(before).StatusChanges(tree(after))

	want := []StatusChange{{PaneID: "p2", Agent: str("codex"), From: state(StateWorking)}}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("%+v", changes)
	}
}

func TestANewOrGonePaneIsNotAStatusChange(t *testing.T) {
	onePane := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"working"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"working"}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"working"}`)
	before := tree(onePane)
	after := tree(twoAgents(t, "working", "idle"))

	if c := before.StatusChanges(after); len(c) != 0 {
		t.Errorf("%+v", c)
	}
	if c := after.StatusChanges(before); len(c) != 0 {
		t.Errorf("%+v", c)
	}
}

func TestSameShapeIgnoresStatusAndFocus(t *testing.T) {
	before := twoAgents(t, "idle", "idle")
	after := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"working"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"blocked","focused":true}`,
		`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent":"claude","agent_status":"blocked","focused":true},
		 {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent":"codex","agent_status":"working"}`)

	if !tree(before).SameShape(tree(after)) {
		t.Error("shape differs")
	}
}

func TestSameShapeIsFalseWhenATabIsAdded(t *testing.T) {
	before := twoAgents(t, "idle", "idle")
	after := twoAgents(t, "idle", "idle")
	after.Tabs = append(after.Tabs, parseTab(t, `{"tab_id":"t2","workspace_id":"w1","label":"second","agent_status":"idle"}`))

	if tree(before).SameShape(tree(after)) {
		t.Error("shape equal")
	}
}

func parseTab(t *testing.T, text string) WireTab {
	t.Helper()
	s := modelSnapshot(t, `{"workspace_id":"w1","label":"x","agent_status":"idle"}`, text, "")
	return s.Tabs[0]
}

func TestSameShapeIsFalseWhenALabelChanges(t *testing.T) {
	before := twoAgents(t, "idle", "idle")
	after := twoAgents(t, "idle", "idle")
	after.Tabs[0].Label = "renamed"

	if tree(before).SameShape(tree(after)) {
		t.Error("shape equal")
	}
}

func TestSameShapeIsFalseWhenAPaneLabelChanges(t *testing.T) {
	before := twoAgents(t, "idle", "idle")
	after := twoAgents(t, "idle", "idle")
	after.Panes[1].Label = str("renamed")

	if tree(before).SameShape(tree(after)) {
		t.Error("shape equal")
	}
}

func TestPaneIDsFollowTreeOrder(t *testing.T) {
	s := twoAgents(t, "idle", "idle")
	slices.Reverse(s.Panes)

	wantStrings(t, tree(s).PaneIDs(), []string{"p2", "p1"})
}

func TestPaneIDsFollowTreeOrderAcrossTabs(t *testing.T) {
	// The snapshot lists t2's pane first; the tree puts each pane under its tab.
	s := modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"idle"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"a","agent_status":"idle"},
		 {"tab_id":"t2","workspace_id":"w1","label":"b","agent_status":"idle"}`,
		`{"pane_id":"p3","tab_id":"t2","workspace_id":"w1","agent_status":"unknown"},
		 {"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"},
		 {"pane_id":"p2","tab_id":"t1","workspace_id":"w1","agent_status":"unknown"}`)

	wantStrings(t, tree(s).PaneIDs(), []string{"p1", "p2", "p3"})
}

func TestStatusChangeLogLinesNameThePaneAndBothStates(t *testing.T) {
	change := func(from, to *AgentState) StatusChange {
		return StatusChange{PaneID: "w2:p9Z", Agent: str("claude"), From: from, To: to}
	}
	cases := []struct {
		got, want string
	}{
		{change(state(StateWorking), state(StateIdle)).LogLine(), "herdr status pane=w2:p9Z agent=claude working -> idle"},
		{change(nil, state(StateBlocked)).LogLine(), "herdr status pane=w2:p9Z agent=claude - -> blocked"},
		{change(state(StateDone), nil).LogLine(), "herdr status pane=w2:p9Z agent=claude done -> -"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestStatusChangeLogLineWithoutAgentUsesDash(t *testing.T) {
	got := StatusChange{PaneID: "p1"}.LogLine()
	if want := "herdr status pane=p1 agent=- - -> -"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLogLinesMatchTheDocumentedFormat(t *testing.T) {
	s := modelSnapshot(t,
		`{"workspace_id":"w2","label":"contr","agent_status":"working"}`,
		`{"tab_id":"w2:t9Y","workspace_id":"w2","label":"planner","agent_status":"working"}`,
		`{"pane_id":"w2:p90","tab_id":"w2:t9Y","workspace_id":"w2","agent":"claude","agent_status":"working","focused":true},
		 {"pane_id":"w2:p91","tab_id":"w2:t9Y","workspace_id":"w2","agent_status":"unknown"}`)

	got := strings.Join(tree(s).LogLines(), "\n")

	want := strings.Join([]string{
		`  ws w2 "contr" working`,
		`    tab w2:t9Y "planner" working`,
		`      pane w2:p90 claude working focused`,
		`      pane w2:p91 - -`,
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// paneWithTokens has workspace w1, tab t1 and one idle pane p1 whose tokens
// is the given JSON object.
func paneWithTokens(t *testing.T, tokens string) WireSnapshot {
	return modelSnapshot(t,
		`{"workspace_id":"w1","label":"contr","agent_status":"idle"}`,
		`{"tab_id":"t1","workspace_id":"w1","label":"main","agent_status":"idle"}`,
		fmt.Sprintf(`{"pane_id":"p1","tab_id":"t1","workspace_id":"w1","agent_status":"idle","tokens":%s}`, tokens))
}

func TestFromSnapshotCopiesPaneTokensAsHerdrSentThem(t *testing.T) {
	tr := tree(paneWithTokens(t, `{"contr_board":"1 s1 3","x":"y"}`))

	tokens := tr.Workspaces[0].Tabs[0].Panes[0].Tokens
	if len(tokens) != 2 || tokens["contr_board"] != "1 s1 3" || tokens["x"] != "y" {
		t.Errorf("%v", tokens)
	}
}

func TestAPaneWithoutTokensHasAnEmptyMap(t *testing.T) {
	tr := tree(twoAgents(t, "idle", "working"))

	for _, pane := range tr.panes() {
		if pane.Tokens == nil || len(pane.Tokens) != 0 {
			t.Errorf("%s: %#v", pane.ID, pane.Tokens)
		}
	}
}

func TestAChangedOrRemovedTokenMakesTheTreesUnequalButKeepsTheShape(t *testing.T) {
	before := tree(paneWithTokens(t, `{"contr_board":"1 s1 3"}`))
	changed := tree(paneWithTokens(t, `{"contr_board":"1 s1 4"}`))
	removed := tree(paneWithTokens(t, `{}`))

	for _, next := range []HerdrTree{changed, removed} {
		if before.Equal(next) {
			t.Error("trees equal")
		}
		if !before.SameShape(next) {
			t.Error("shape differs")
		}
		if c := before.StatusChanges(next); len(c) != 0 {
			t.Errorf("%+v", c)
		}
	}
	if !before.Equal(tree(paneWithTokens(t, `{"contr_board":"1 s1 3"}`))) {
		t.Error("same trees unequal")
	}
}
