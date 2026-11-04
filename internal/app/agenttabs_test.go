package app

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/owner"
)

func orchW(addr, name, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Role: agents.RoleOrchestrator, Live: true}
}

func workW(addr, name, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Role: "builder", Live: true}
}

func addrs(tabs []AgentTab) []string {
	var out []string
	for _, t := range tabs {
		out = append(out, t.Address)
	}
	return out
}

func TestAgentTabsPutOrchestratorsFirst(t *testing.T) {
	s := agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchW("o", "orch", "p")},
		Workers:       []agents.Worker{workW("a", "a", "p"), workW("b", "b", "p")},
	}
	got := addrs(AgentTabs(s, "p", ""))
	if len(got) != 3 || got[0] != "o" || got[1] != "a" || got[2] != "b" {
		t.Fatalf("order = %v, want [o a b]", got)
	}
	if !AgentTabs(s, "p", "")[0].Orchestrator {
		t.Fatal("first tab is not an orchestrator")
	}
}

func TestAgentTabsKeepOnlyTheOpenProject(t *testing.T) {
	s := agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchW("o", "orch", "p"), orchW("oq", "orchq", "q")},
		Workers:       []agents.Worker{workW("a", "a", "p"), workW("x", "x", "q")},
	}
	got := addrs(AgentTabs(s, "p", ""))
	if len(got) != 2 || got[0] != "o" || got[1] != "a" {
		t.Fatalf("tabs = %v, want [o a]", got)
	}
}

func TestAgentTabsMarkTheSelectedAddress(t *testing.T) {
	s := agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchW("o", "orch", "p")},
		Workers:       []agents.Worker{workW("a", "a", "p")},
	}
	for _, tab := range AgentTabs(s, "p", "a") {
		if tab.Selected != (tab.Address == "a") {
			t.Fatalf("tab %s Selected = %v", tab.Address, tab.Selected)
		}
	}
	for _, tab := range AgentTabs(s, "p", "") {
		if tab.Selected {
			t.Fatalf("tab %s selected with no selection", tab.Address)
		}
	}
}

func TestAgentTabLabelIsIssueAndWorktree(t *testing.T) {
	w := workW("a", "name", "p")
	w.Issue, w.Worktree = "#138", "/code/contr-wt/agent-138-tab-model"
	got := AgentTabs(agentstate.Snapshot{Workers: []agents.Worker{w}}, "p", "")[0].Label
	if got != "#138 · agent-138-tab-model" {
		t.Fatalf("label = %q", got)
	}
}

func TestAgentTabLabelFallsBackToName(t *testing.T) {
	for _, w := range []agents.Worker{
		func() agents.Worker { w := workW("a", "name", "p"); w.Worktree = "/x/wt"; return w }(),
		func() agents.Worker { w := workW("a", "name", "p"); w.Issue = "#1"; return w }(),
	} {
		got := AgentTabs(agentstate.Snapshot{Workers: []agents.Worker{w}}, "p", "")[0].Label
		if got != "name" {
			t.Fatalf("label = %q, want name", got)
		}
	}
}

func needsSnap() agentstate.Snapshot {
	blocked := workW("w", "w", "p")
	blocked.Status = "blocked"
	other := workW("w2", "w2", "q")
	other.Status = "blocked"
	return agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchW("o", "orch", "p"), orchW("o2", "orch2", "p")},
		Workers:       []agents.Worker{blocked, other},
		Questions: []owner.Question{
			{ID: "D1", Project: "p", Title: "open"},
			{ID: "D2", Project: "p", Title: "done"},
			{ID: "D3", Project: "q", Title: "elsewhere"},
		},
		Todos:    []owner.Todo{{ID: "T1", Project: "p", Text: "x"}},
		Resolved: []string{"D2"},
	}
}

func TestOrchestratorNeedsCountsUnresolvedItemsOfItsProject(t *testing.T) {
	s := needsSnap()
	// blocked worker + D1 + T1 in p; D2 resolved; q items excluded.
	if got := NeedsCount(s, "p"); got != 3 {
		t.Fatalf("NeedsCount = %d, want 3", got)
	}
	tabs := AgentTabs(s, "p", "")
	if tabs[0].Needs != 3 {
		t.Fatalf("first orchestrator Needs = %d, want 3", tabs[0].Needs)
	}
	if tabs[1].Needs != 0 || tabs[2].Needs != 0 {
		t.Fatalf("badge on other tabs: %+v", tabs[1:])
	}
}

func TestOpenProjectFollowsTheSelectedAgent(t *testing.T) {
	s := needsSnap()
	if got := OpenProject(s, state.Selected{Kind: state.KindWorker, ID: "w2"}, true, "p"); got != "q" {
		t.Fatalf("project = %q, want q", got)
	}
	if got := OpenProject(s, state.Selected{Kind: state.KindOrchestrator, ID: "o"}, true, "q"); got != "p" {
		t.Fatalf("project = %q, want p", got)
	}
}

func TestOpenProjectKeepsTheLastWithAWorktreeSelection(t *testing.T) {
	s := needsSnap()
	if got := OpenProject(s, state.Selected{Kind: state.KindWorktree, ID: "/r"}, true, "q"); got != "q" {
		t.Fatalf("project = %q, want q", got)
	}
	if got := OpenProject(s, state.Selected{}, false, "q"); got != "q" {
		t.Fatalf("project = %q, want q", got)
	}
}

func TestOpenProjectStartsAtTheFirstOrchestrator(t *testing.T) {
	s := agentstate.Snapshot{Orchestrators: []agents.Worker{orchW("o", "orch", "first")}}
	if got := OpenProject(s, state.Selected{}, false, ""); got != "first" {
		t.Fatalf("project = %q, want first", got)
	}
}

func TestOpenProjectWithNoAgentsIsEmpty(t *testing.T) {
	if got := OpenProject(agentstate.Snapshot{}, state.Selected{}, false, ""); got != "" {
		t.Fatalf("project = %q, want empty", got)
	}
}

func TestOpenProjectWithAnUnknownAddressKeeps(t *testing.T) {
	s := needsSnap()
	if got := OpenProject(s, state.Selected{Kind: state.KindWorker, ID: "gone"}, true, "q"); got != "q" {
		t.Fatalf("project = %q, want q", got)
	}
}

func termW(addr, name, project, worktree string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Worktree: worktree, Role: agents.RoleTerminal, Live: true}
}

func TestAgentTabsPlainLast(t *testing.T) {
	s := agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchW("o", "orch", "p")},
		Workers:       []agents.Worker{workW("a", "a", "p")},
		Terminals: []agents.Worker{
			termW("t1", "term-1", "p", "/work/repo"),
			termW("tq", "term-q", "q", "/work/other"),
		},
	}
	tabs := AgentTabs(s, "p", "t1")
	got := addrs(tabs)
	if len(got) != 3 || got[0] != "o" || got[1] != "a" || got[2] != "t1" {
		t.Fatalf("order = %v, want [o a t1]", got)
	}
	p := tabs[2]
	if !p.Plain || p.Orchestrator || !p.Selected || p.Label != "$ repo" {
		t.Fatalf("plain tab = %+v", p)
	}
	if tabs[0].Plain || tabs[1].Plain {
		t.Fatalf("agent tab marked plain: %+v", tabs[:2])
	}
}

func TestAgentTabsPlainLabel(t *testing.T) {
	titled := termW("t1", "term-1", "p", "/work/repo")
	titled.Title = "vim main.go"
	bare := termW("t2", "term-2", "p", "")
	s := agentstate.Snapshot{Terminals: []agents.Worker{titled, bare}}
	tabs := AgentTabs(s, "p", "")
	if tabs[0].Label != "vim main.go" {
		t.Errorf("titled label = %q, want the title", tabs[0].Label)
	}
	if tabs[1].Label != "term-2" {
		t.Errorf("no title, no folder label = %q, want the name", tabs[1].Label)
	}
}

func TestAgentTabsPlainNoBadge(t *testing.T) {
	s := needsSnap()
	s.Terminals = []agents.Worker{termW("t1", "term-1", "p", "/work/repo")}
	for _, tab := range AgentTabs(s, "p", "") {
		if tab.Plain && tab.Needs != 0 {
			t.Fatalf("plain tab has badge %d", tab.Needs)
		}
	}
	// With no orchestrator, a plain tab must not take the badge.
	only := agentstate.Snapshot{
		Terminals: []agents.Worker{termW("t1", "term-1", "p", "/r")},
		Questions: []owner.Question{{ID: "D1", Project: "p", Title: "open"}},
	}
	if tabs := AgentTabs(only, "p", ""); tabs[0].Needs != 0 {
		t.Fatalf("plain tab badge = %d, want 0", tabs[0].Needs)
	}
}

func TestOpenProjectTerminal(t *testing.T) {
	s := needsSnap()
	s.Terminals = []agents.Worker{termW("t1", "term-1", "q", "/r")}
	if got := OpenProject(s, state.Selected{Kind: state.KindTerminal, ID: "t1"}, true, "p"); got != "q" {
		t.Fatalf("project = %q, want q", got)
	}
	if got := OpenProject(s, state.Selected{Kind: state.KindTerminal, ID: "gone"}, true, "p"); got != "p" {
		t.Fatalf("unknown terminal: project = %q, want p", got)
	}
}

func TestNeedsCountIgnoresTerminals(t *testing.T) {
	s := needsSnap()
	want := NeedsCount(s, "p")
	blocked := termW("t1", "term-1", "p", "/r")
	blocked.Status = "blocked"
	s.Terminals = []agents.Worker{blocked}
	if got := NeedsCount(s, "p"); got != want {
		t.Fatalf("NeedsCount = %d, want %d", got, want)
	}
}
