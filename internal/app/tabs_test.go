package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
)

func orchW(addr, name, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Role: agents.RoleOrchestrator, Live: true}
}

func workW(addr, name, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Role: "builder", Live: true}
}

func termW(addr, name, project, worktree string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Worktree: worktree, Role: agents.RoleTerminal, Live: true}
}

func addrs(tabs []Tab) []string {
	var out []string
	for _, t := range tabs {
		out = append(out, t.Address)
	}
	return out
}

// twoAgents is project p: orchestrator o, worker a, and two plain terminals; project q has one
// plain terminal that must not show for p.
func twoAgents() agentstate.Snapshot {
	return agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchW("o", "orch", "p")},
		Workers:       []agents.Worker{workW("a", "a", "p"), workW("x", "x", "q")},
		Terminals: []agents.Worker{
			termW("t1", "term-1", "p", "/work/repo"),
			termW("tq", "term-q", "q", "/work/other"),
			termW("t2", "term-2", "p", "/work/repo"),
		},
	}
}

func TestTitleTabsAreTheSessionsThenTheWorkerThenThePlainTerminals(t *testing.T) {
	s := twoAgents()
	if got := addrs(TitleTabs(s, "p", "", "o", "")); !slices.Equal(got, []string{"o", "t1", "t2"}) {
		t.Fatalf("tabs of o = %v, want [o t1 t2]", got)
	}
	// A worker of the same project: the sessions, its own tab, the same plain terminals.
	if got := addrs(TitleTabs(s, "p", "", "a", "")); !slices.Equal(got, []string{"o", "a", "t1", "t2"}) {
		t.Fatalf("tabs of a = %v, want [o a t1 t2]", got)
	}
	// An agent of another project: its own project's terminals.
	if got := addrs(TitleTabs(s, "q", "", "x", "")); !slices.Equal(got, []string{"x", "tq"}) {
		t.Fatalf("tabs of x = %v, want [x tq]", got)
	}
}

func TestTitleTabsMarkKindAndSelection(t *testing.T) {
	tabs := TitleTabs(twoAgents(), "p", "", "o", "t1")
	o, t1 := tabs[0], tabs[1]
	if !o.Orchestrator || o.Plain || o.Closable() || o.Selected {
		t.Errorf("agent tab = %+v", o)
	}
	if !t1.Plain || !t1.Closable() || !t1.Selected || t1.Title != "$ repo" {
		t.Errorf("plain tab = %+v", t1)
	}
	for _, tab := range TitleTabs(twoAgents(), "p", "", "o", "") {
		if tab.Selected {
			t.Errorf("tab %s selected with no selection", tab.Address)
		}
	}
}

func TestTitleTabsWithoutAnAgentKeepTheSessionsAndPlainTerminals(t *testing.T) {
	got := addrs(TitleTabs(twoAgents(), "p", "", "", ""))
	if !slices.Equal(got, []string{"o", "t1", "t2"}) {
		t.Fatalf("tabs = %v, want the session and the plain terminals", got)
	}
	if got := TitleTabs(agentstate.Snapshot{}, "", "", "", ""); len(got) != 0 {
		t.Fatalf("tabs with no agents = %v", got)
	}
}

// An ended worker has no tab (D112); an ended orchestrator and a plain terminal stay.
func TestTitleTabsSkipAnEndedWorker(t *testing.T) {
	s := twoAgents()
	s.Workers[0].Live = false
	if got := addrs(TitleTabs(s, "p", "", "a", "")); !slices.Equal(got, []string{"o", "t1", "t2"}) {
		t.Errorf("tabs of an ended worker = %v", got)
	}
	s.Orchestrators[0].Live = false
	if got := addrs(TitleTabs(s, "p", "", "o", "")); !slices.Equal(got, []string{"o", "t1", "t2"}) {
		t.Errorf("tabs of an ended orchestrator = %v", got)
	}
}

func TestTabTitle(t *testing.T) {
	titled := termW("t", "term-1", "p", "/work/repo")
	titled.Title = "go test ./..."
	worker := workW("a", "name", "p")
	worker.Issue, worker.Worktree = "#138", "/code/contr-wt/agent-138-tab-model"
	noIssue := workW("b", "name", "p")
	noIssue.Worktree = "/x/wt"
	for _, c := range []struct {
		w    agents.Worker
		want string
	}{
		{titled, "go test ./..."},
		{termW("t", "term-1", "p", "/work/repo"), "$ repo"},
		{termW("t", "term-1", "p", ""), "term-1"},
		{orchW("o", "lead", "p"), "lead"},
		{worker, "#138 · agent-138-tab-model"},
		{noIssue, "name"},
	} {
		if got := tabTitle(c.w); got != c.want {
			t.Errorf("tabTitle(%+v) = %q, want %q", c.w, got, c.want)
		}
	}
}

func TestTabStateFollowsTheAgentStatus(t *testing.T) {
	for _, c := range []struct {
		status string
		live   bool
		want   kit.GlyphState
	}{
		{"working", true, kit.GlyphWorking},
		{"blocked", true, kit.GlyphNeedsYou},
		{"error", true, kit.GlyphNeedsYou},
		{"idle", true, kit.GlyphIdle},
		{"working", false, kit.GlyphIdle},
	} {
		if got := tabState(agents.Worker{Status: c.status, Live: c.live}); got != c.want {
			t.Errorf("tabState(%q, live %v) = %v, want %v", c.status, c.live, got, c.want)
		}
	}
}

func TestTabAgentFollowsTheSelectedAgent(t *testing.T) {
	s := twoAgents()
	if got := TabAgent(s, state.Selected{Kind: state.KindWorker, ID: "a"}, true, "p", "", "o"); got != "a" {
		t.Errorf("selected worker: agent = %q, want a", got)
	}
	if got := TabAgent(s, state.Selected{Kind: state.KindOrchestrator, ID: "o"}, true, "p", "", "a"); got != "o" {
		t.Errorf("selected orchestrator: agent = %q, want o", got)
	}
}

func TestTabAgentKeepsTheLastAgentWhileAPlainTerminalIsSelected(t *testing.T) {
	s := twoAgents()
	sel := state.Selected{Kind: state.KindTerminal, ID: "t1"}
	if got := TabAgent(s, sel, true, "p", "", "a"); got != "a" {
		t.Errorf("agent = %q, want the kept a", got)
	}
	// A kept agent of another project does not hold: the first orchestrator of the project does.
	if got := TabAgent(s, sel, true, "p", "", "x"); got != "o" {
		t.Errorf("agent = %q, want o", got)
	}
	// No orchestrator: the first live worker.
	s.Orchestrators = nil
	if got := TabAgent(s, sel, true, "p", "", ""); got != "a" {
		t.Errorf("agent = %q, want a", got)
	}
	if got := TabAgent(agentstate.Snapshot{}, state.Selected{}, false, "", "", ""); got != "" {
		t.Errorf("agent = %q with no agents", got)
	}
}

func TestOpenProjectFollowsTheSelection(t *testing.T) {
	s := twoAgents()
	for _, c := range []struct {
		sel  state.Selected
		ok   bool
		keep string
		want string
	}{
		{state.Selected{Kind: state.KindWorker, ID: "x"}, true, "p", "q"},
		{state.Selected{Kind: state.KindOrchestrator, ID: "o"}, true, "q", "p"},
		{state.Selected{Kind: state.KindTerminal, ID: "tq"}, true, "p", "q"},
		{state.Selected{Kind: state.KindTerminal, ID: "gone"}, true, "p", "p"},
		{state.Selected{Kind: state.KindWorker, ID: "gone"}, true, "q", "q"},
		{state.Selected{Kind: state.KindWorktree, ID: "/r"}, true, "q", "q"},
		{state.Selected{}, false, "q", "q"},
		{state.Selected{}, false, "", "p"}, // the first orchestrator's project at start
	} {
		if got := OpenProject(s, c.sel, c.ok, c.keep); got != c.want {
			t.Errorf("OpenProject(%+v, %v, keep %q) = %q, want %q", c.sel, c.ok, c.keep, got, c.want)
		}
	}
	if got := OpenProject(agentstate.Snapshot{}, state.Selected{}, false, ""); got != "" {
		t.Errorf("OpenProject with no agents = %q", got)
	}
}

func TestForeignTabsUseTheReadableNameAndKeepTheAddressInTheTip(t *testing.T) {
	w := agents.Worker{Address: "t1nh", Name: "t1nh", Shown: "contr 2", Role: agents.RoleTerminal, Foreign: true, Live: true, Project: "contr"}
	if got := tabTitle(w); got != "contr 2" {
		t.Errorf("title %q", got)
	}
	if got := tabTip(w); got != "contr 2 · t1nh" {
		t.Errorf("tip %q", got)
	}
	named := agents.Worker{Address: "t2", Name: "scout", Role: agents.RoleTerminal, Foreign: true, Live: true}
	if got := tabTitle(named); got != "scout" {
		t.Errorf("title %q", got)
	}
	own := agents.Worker{Address: "t3", Name: "w", Role: agents.RoleTerminal, Title: "vim", Live: true}
	if got := tabTitle(own); got != "vim" || tabTip(own) != "w" {
		t.Errorf("own terminal %q %q", got, tabTip(own))
	}
}

func TestForeignRowLabelUsesTheReadableName(t *testing.T) {
	w := agents.Worker{Address: "t1nh", Name: "t1nh", Shown: "contr", Role: agents.RoleOrchestrator, Foreign: true, Live: true}
	if got := AgentLabel(w, "", ""); !strings.HasPrefix(got, "contr,") {
		t.Errorf("label %q", got)
	}
	if got := rowTip(w); got != "contr · t1nh" {
		t.Errorf("tip %q", got)
	}
}

// Every Claude session of the project is a tab, the same set whichever is selected (D190).
func TestTitleTabsShareEverySessionOfTheProject(t *testing.T) {
	s := twoAgents()
	s.Orchestrators = append(s.Orchestrators, orchW("o2", "orch-2", "p"), orchW("oq", "orch-q", "q"))
	for _, agent := range []string{"o", "o2"} {
		if got := addrs(TitleTabs(s, "p", "", agent, agent)); !slices.Equal(got, []string{"o", "o2", "t1", "t2"}) {
			t.Errorf("tabs with %s selected = %v, want [o o2 t1 t2]", agent, got)
		}
	}
	tabs := TitleTabs(s, "p", "", "o2", "o2")
	if tabs[0].Selected || !tabs[1].Selected {
		t.Errorf("selection marks: %+v", tabs[:2])
	}
}
