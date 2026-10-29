package herdr

import (
	"slices"
	"testing"
)

// agentsPane is a pane spec: id, label (empty for none), agent kind (empty
// for no agent) and state.
type agentsPane struct {
	id, label, agent string
	state            AgentState
}

type agentsTab struct {
	id, label string
	panes     []agentsPane
}

type agentsWS struct {
	id   string
	tabs []agentsTab
}

func agentsStr(s string) *string { return &s }

// agentsTree builds a tree from the specs. Workspace and tab states are idle.
// focused names the focused pane ("" for none).
func agentsTree(focused string, workspaces ...agentsWS) HerdrTree {
	tree := HerdrTree{Version: "0.9.3"}
	if focused != "" {
		tree.FocusedPaneID = agentsStr(focused)
	}
	for _, w := range workspaces {
		ws := Workspace{ID: w.id, Label: w.id, Status: StateIdle}
		for _, t := range w.tabs {
			tab := Tab{ID: t.id, Label: t.label, Status: StateIdle}
			for _, p := range t.panes {
				pane := Pane{ID: p.id, Focused: focused == p.id, Tokens: map[string]string{}}
				if p.label != "" {
					pane.Label = agentsStr(p.label)
				}
				if p.agent != "" {
					pane.Agent = &AgentInfo{Name: p.agent, Status: p.state}
				}
				tab.Panes = append(tab.Panes, pane)
			}
			ws.Tabs = append(ws.Tabs, tab)
		}
		tree.Workspaces = append(tree.Workspaces, ws)
	}
	return tree
}

// agentsOneTab is a tree with one workspace w1 and one tab t1.
func agentsOneTab(focused, label string, panes ...agentsPane) HerdrTree {
	return agentsTree(focused, agentsWS{"w1", []agentsTab{{"t1", label, panes}}})
}

func agentsIdle(id, label, agent string) agentsPane {
	return agentsPane{id, label, agent, StateIdle}
}

func agentsMeta(name, model, role *string) *AgentMeta {
	return &AgentMeta{Name: name, Model: model, Role: role}
}

func agentsNames(rows []AgentRow) []string {
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

func agentsEqual[T comparable](t *testing.T, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func agentsEqualStr(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPanesWithoutAnAgentGiveNoRow(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", ""), agentsIdle("p2", "", "claude"))
	rows := AgentRows(tree, nil)
	if len(rows) != 1 || rows[0].PaneID != "p2" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestATreeWithoutAgentsGivesNoRows(t *testing.T) {
	tree := agentsOneTab("", "a", agentsIdle("p1", "", ""))
	if rows := AgentRows(tree, nil); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows := AgentRows(agentsTree(""), nil); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRowsFollowWorkspaceTabThenPaneAcrossWorkspaces(t *testing.T) {
	tree := agentsTree("",
		agentsWS{"w1", []agentsTab{
			{"t1", "a", []agentsPane{agentsIdle("p1", "", ""), agentsIdle("p2", "", "claude")}},
			{"t2", "b", []agentsPane{{"p3", "", "codex", StateWorking}}},
		}},
		agentsWS{"w2", []agentsTab{
			{"t3", "c", []agentsPane{{"p4", "", "claude", StateDone}}},
		}},
	)
	var ids []string
	for _, r := range AgentRows(tree, nil) {
		ids = append(ids, r.WorkspaceID+"/"+r.TabID+"/"+r.PaneID)
	}
	agentsEqual(t, ids, []string{"w1/t1/p2", "w1/t2/p3", "w2/t3/p4"})
}

func TestTheNameIsTheTabLabelForASingleAgent(t *testing.T) {
	tree := agentsOneTab("", "b-1151", agentsIdle("p1", "ignored", "claude"))
	agentsEqualStr(t, AgentRows(tree, nil)[0].Name, "b-1151")
}

func TestTwoAgentsInATabAreNamedByPaneLabelOrNumber(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "left", "claude"), agentsIdle("p2", "", "codex"))
	agentsEqual(t, agentsNames(AgentRows(tree, nil)), []string{"planner · left", "planner · 2"})
}

func TestTheNumberCountsOnlyTheAgentPanesOfTheTab(t *testing.T) {
	tree := agentsOneTab("", "planner",
		agentsIdle("p0", "", ""), agentsIdle("p1", "", "claude"), agentsIdle("p2", "", "codex"))
	agentsEqual(t, agentsNames(AgentRows(tree, nil)), []string{"planner · 1", "planner · 2"})
}

func TestABlankPaneLabelIsNoLabel(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "   ", "claude"), agentsIdle("p2", "", "codex"))
	agentsEqual(t, agentsNames(AgentRows(tree, nil)), []string{"planner · 1", "planner · 2"})
}

func TestABlankTabLabelFallsBackToTheAgentKind(t *testing.T) {
	for _, label := range []string{"", "   "} {
		tree := agentsOneTab("", label, agentsIdle("p1", "", "claude"))
		if got := AgentRows(tree, nil)[0].Name; got != "claude" {
			t.Fatalf("label %q: name = %q", label, got)
		}
	}
}

func TestABlankTabLabelKeepsTheSplitSuffixWithEachAgentKind(t *testing.T) {
	tree := agentsOneTab("", " ", agentsIdle("p1", "left", "claude"), agentsIdle("p2", "", "codex"))
	agentsEqual(t, agentsNames(AgentRows(tree, nil)), []string{"claude · left", "codex · 2"})
}

func TestTheBridgeNameOverridesTheTabLabelAndTheSplitName(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"), agentsIdle("p2", "", "codex"))
	bridge := func(pane string) *AgentMeta {
		if pane == "p1" {
			return agentsMeta(agentsStr("b-1151"), nil, nil)
		}
		return nil
	}
	rows := AgentRows(tree, bridge)
	agentsEqualStr(t, rows[0].Name, "b-1151")
	agentsEqualStr(t, rows[1].Name, "planner · 2")
}

func TestABlankBridgeNameIsNoName(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"))
	rows := AgentRows(tree, func(string) *AgentMeta { return agentsMeta(agentsStr("  "), nil, nil) })
	agentsEqualStr(t, rows[0].Name, "planner")
}

func TestTheDetailIsTheAgentKindWithoutBridgeData(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"))
	agentsEqualStr(t, AgentRows(tree, nil)[0].Detail, "claude")
	// A bridge that names the agent but gives no model or role keeps the agent kind.
	named := AgentRows(tree, func(string) *AgentMeta { return agentsMeta(agentsStr("b-1151"), nil, nil) })
	agentsEqualStr(t, named[0].Detail, "claude")
}

func TestTheDetailJoinsModelAndRole(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"))
	rows := AgentRows(tree, func(string) *AgentMeta {
		return agentsMeta(nil, agentsStr("haiku 5.5"), agentsStr("builder"))
	})
	agentsEqualStr(t, rows[0].Detail, "haiku 5.5 · builder")
}

func TestTheDetailHasOnlyThePartThatIsPresent(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"))
	roleOnly := AgentRows(tree, func(string) *AgentMeta { return agentsMeta(nil, nil, agentsStr("builder")) })
	modelOnly := AgentRows(tree, func(string) *AgentMeta { return agentsMeta(nil, agentsStr("haiku 5.5"), nil) })
	agentsEqualStr(t, roleOnly[0].Detail, "builder")
	agentsEqualStr(t, modelOnly[0].Detail, "haiku 5.5")
}

func TestBlankModelAndRoleFallBackToTheAgentKind(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"))
	rows := AgentRows(tree, func(string) *AgentMeta { return agentsMeta(nil, agentsStr(""), agentsStr("  ")) })
	agentsEqualStr(t, rows[0].Detail, "claude")
}

func TestTheLinkIsPassedThroughAsGiven(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"), agentsIdle("p2", "", "codex"))
	rows := AgentRows(tree, func(pane string) *AgentMeta {
		link := "PR 12"
		if pane == "p1" {
			link = "#640"
		}
		return &AgentMeta{Link: &link}
	})
	if rows[0].Link == nil || *rows[0].Link != "#640" {
		t.Fatalf("row 0 link = %v", rows[0].Link)
	}
	if rows[1].Link == nil || *rows[1].Link != "PR 12" {
		t.Fatalf("row 1 link = %v", rows[1].Link)
	}
}

func TestAMissingOrBlankLinkIsNone(t *testing.T) {
	tree := agentsOneTab("", "planner", agentsIdle("p1", "", "claude"))
	none := AgentRows(tree, nil)
	blank := AgentRows(tree, func(string) *AgentMeta { return &AgentMeta{Link: agentsStr(" ")} })
	if none[0].Link != nil || blank[0].Link != nil {
		t.Fatalf("links = %v, %v", none[0].Link, blank[0].Link)
	}
}

func TestAllFiveStatesPassThroughOneToOne(t *testing.T) {
	states := []AgentState{StateIdle, StateWorking, StateBlocked, StateDone, StateUnknown}
	var panes []agentsPane
	for i, s := range states {
		panes = append(panes, agentsPane{"p" + string(rune('1'+i)), "", "claude", s})
	}
	var got []AgentState
	for _, r := range AgentRows(agentsOneTab("", "planner", panes...), nil) {
		got = append(got, r.Status)
	}
	agentsEqual(t, got, states)
}

func TestOnlyTheFocusedPaneIsFocused(t *testing.T) {
	tree := agentsOneTab("p2", "planner", agentsIdle("p1", "", "claude"), agentsIdle("p2", "", "codex"))
	var got []bool
	for _, r := range AgentRows(tree, nil) {
		got = append(got, r.Focused)
	}
	agentsEqual(t, got, []bool{false, true})
}

func TestAFocusOnAPaneWithoutAnAgentOrAnUnknownPaneMarksNoRow(t *testing.T) {
	for _, focus := range []string{"p1", "gone"} {
		tree := agentsOneTab(focus, "planner", agentsIdle("p1", "", ""), agentsIdle("p2", "", "claude"))
		for _, r := range AgentRows(tree, nil) {
			if r.Focused {
				t.Fatalf("focus %q marks row %s", focus, r.PaneID)
			}
		}
	}
}

func TestTheBridgeIsAskedForEachAgentPaneByItsID(t *testing.T) {
	tree := agentsOneTab("", "planner",
		agentsIdle("p1", "", "claude"), agentsIdle("p2", "", ""), agentsIdle("p3", "", "codex"))
	var asked []string
	rows := AgentRows(tree, func(pane string) *AgentMeta {
		asked = append(asked, pane)
		return nil
	})
	agentsEqual(t, asked, []string{"p1", "p3"})
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
}

func TestRunningLineReadsZeroOneAndMany(t *testing.T) {
	agentsEqualStr(t, RunningLine(0), "0 running")
	agentsEqualStr(t, RunningLine(1), "1 running")
	agentsEqualStr(t, RunningLine(3), "3 running")
}

func TestRunningCountsEveryListedAgentIncludingIdleAndDone(t *testing.T) {
	tree := agentsOneTab("", "planner",
		agentsPane{"p1", "", "claude", StateWorking},
		agentsPane{"p2", "", "claude", StateIdle},
		agentsPane{"p3", "", "claude", StateDone})
	agentsEqualStr(t, RunningLine(len(AgentRows(tree, nil))), "3 running")
}
