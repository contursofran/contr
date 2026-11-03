package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
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

func agentsOf(specs ...tabSpec) herdr.LiveHerdr { return connectedWith(agentsTree(specs...)) }

var agentsNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

type agentsHarness struct {
	t    *testing.T
	snap agentstate.Snapshot
	sel  *state.Selection
	list *AgentsList
	tt   *ui.Tester
}

func newAgents(t *testing.T, s agentstate.Snapshot) *agentsHarness {
	t.Helper()
	h := &agentsHarness{t: t, snap: s, sel: state.NewSelection(nil)}
	h.list = NewAgentsList(func() agentstate.Snapshot { return h.snap }, h.sel)
	h.list.now = func() time.Time { return agentsNow }
	h.tt = ui.NewTester(h.list.View, theme.SidebarWidth, theme.WindowHeight)
	return h
}

func (h *agentsHarness) has(label string) bool { _, ok := h.tt.Find(label); return ok }

func (h *agentsHarness) y(label string) float32 { return box(h.t, h.tt, label).Y }

func worker(name, status string) agents.Worker {
	return agents.Worker{Name: name, Address: "t-" + name, Live: true, Status: status, Command: "claude", LastOutput: agentsNow.Add(-3 * time.Minute)}
}

func orchestrator(name, status string) agents.Worker {
	w := worker(name, status)
	w.Role = agents.RoleOrchestrator
	return w
}

func TestOrchestratorsSectionOnTopWhateverState(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{
		Orchestrators: []agents.Worker{orchestrator("lead", "done")},
		Workers:       []agents.Worker{worker("b1", "working")},
	})
	if !h.has(LabelOrchestratorsHeader) || !h.has(LabelAgentsHeader) {
		t.Fatalf("headers missing; texts %q", h.tt.Texts())
	}
	lead, b1 := h.y("lead, done, claude · 3m"), h.y("b1, working, claude · 3m")
	if !(h.y(LabelOrchestratorsHeader) < lead && lead < h.y(LabelAgentsHeader) && h.y(LabelAgentsHeader) < b1) {
		t.Errorf("order: orch header %v, lead %v, agents header %v, b1 %v",
			h.y(LabelOrchestratorsHeader), lead, h.y(LabelAgentsHeader), b1)
	}
}

func TestNoOrchestratorsHidesSection(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{worker("b1", "idle")}})
	if h.has(LabelOrchestratorsHeader) || !h.has(LabelAgentsHeader) {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestNoAgentsShowsNothing(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{})
	if h.has(LabelAgents) || h.has(LabelAgentsHeader) || h.has(LabelOrchestratorsHeader) {
		t.Errorf("texts %q", h.tt.Texts())
	}
	// A list with no source shows nothing either.
	if NewAgentsList(nil, nil).snapshot().Workers != nil {
		t.Error("nil source has workers")
	}
}

func TestRowShowsCommandAndAge(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{worker("b1", "working")}})
	if !h.has("b1, working, claude · 3m") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if !strings.Contains(strings.Join(h.tt.Texts(), "|"), "claude · 3m") {
		t.Errorf("no sub line in %q", h.tt.Texts())
	}
	if !h.has("1 running") {
		t.Errorf("no count in %q", h.tt.Texts())
	}
}

func TestAgeText(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{{5 * time.Second, "5s"}, {59 * time.Second, "59s"}, {3 * time.Minute, "3m"}, {2*time.Hour + 5*time.Minute, "2h"}, {50 * time.Hour, "50h"}, {-time.Second, ""}}
	for _, c := range cases {
		if got := AgeText(agentsNow, agentsNow.Add(-c.ago)); got != c.want {
			t.Errorf("%v: %q, want %q", c.ago, got, c.want)
		}
	}
	if got := AgeText(agentsNow, time.Time{}); got != "" {
		t.Errorf("zero time: %q", got)
	}
}

func TestRowWithoutCommandOrOutputHasOneLine(t *testing.T) {
	w := agents.Worker{Name: "b1", Address: "a", Live: true}
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{w}})
	if !h.has("b1, idle") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	if got := box(t, h.tt, "b1, idle").H; got != 26 {
		t.Errorf("row is %v high, want 26", got)
	}
}

func TestLongNameEllipsis(t *testing.T) {
	long := strings.Repeat("very-long-worker-name-", 6)
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{worker(long, "working")}})
	row := box(t, h.tt, long+", working, claude · 3m")
	if row.W != theme.SidebarWidth-2*theme.CardInsetX {
		t.Errorf("row %+v wide, want %d", row, theme.SidebarWidth-2*theme.CardInsetX)
	}
	text := box(t, h.tt, long+", working, claude · 3m text")
	if text.X+text.W > row.X+row.W {
		t.Errorf("name overflows the row: text %+v row %+v", text, row)
	}
}

func TestFinishedWorkerShowsExitCode(t *testing.T) {
	two := 2
	w := worker("b1", "")
	w.Live, w.ExitCode = false, &two
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{w}})
	if !h.has("b1, done, claude · exit 2 · 3m") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if !h.has("0 running") {
		t.Errorf("a finished worker counts as running: %q", h.tt.Texts())
	}
}

func TestStatusIcons(t *testing.T) {
	cases := map[string]string{"working": "working", "blocked": "blocked", "error": "blocked", "done": "done", "idle": "idle", "": "idle"}
	for status, icon := range cases {
		if got := workerIcon(agents.Worker{Live: true, Status: status}).Label(); got != icon {
			t.Errorf("status %q: icon %q, want %q", status, got, icon)
		}
	}
	if got := workerIcon(agents.Worker{Status: "working"}).Label(); got != "done" {
		t.Errorf("ended program: icon %q, want done", got)
	}
}

func TestClickSelectsWorker(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{worker("b1", "idle")}})
	if err := h.tt.Click("b1, idle, claude · 3m"); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.sel.Current(); !ok || got != (state.Selected{Kind: state.KindWorker, ID: "t-b1"}) {
		t.Errorf("selection %+v %v", got, ok)
	}
	h.tt.Frame()
	if !h.has("b1, idle, claude · 3m") {
		t.Error("row is gone")
	}
}

func TestClickSelectsOrchestrator(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{Orchestrators: []agents.Worker{orchestrator("lead", "idle")}})
	if err := h.tt.Click("lead, idle, claude · 3m"); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.sel.Current(); !ok || got != (state.Selected{Kind: state.KindOrchestrator, ID: "t-lead"}) {
		t.Errorf("selection %+v %v", got, ok)
	}
}

func TestSyncClearsAGoneSelectionOnly(t *testing.T) {
	h := newAgents(t, agentstate.Snapshot{Workers: []agents.Worker{worker("b1", "idle")}})
	h.sel.Select(state.Selected{Kind: state.KindWorker, ID: "t-b1"})
	h.list.Sync()
	if _, ok := h.sel.Current(); !ok {
		t.Fatal("a present worker left the selection")
	}
	h.snap = agentstate.Snapshot{}
	h.list.Sync()
	if _, ok := h.sel.Current(); ok {
		t.Error("a gone worker stayed selected")
	}
	h.sel.Select(state.Selected{Kind: state.KindOrchestrator, ID: "t-lead"})
	h.list.Sync()
	if _, ok := h.sel.Current(); ok {
		t.Error("a gone orchestrator stayed selected")
	}
	h.sel.Select(state.Selected{Kind: state.KindWorktree, ID: "/x"})
	h.list.Sync()
	if _, ok := h.sel.Current(); !ok {
		t.Error("a worktree selection was cleared")
	}
}

func notRunningLive() herdr.LiveHerdr { return herdr.LiveHerdr{State: notRunning()} }
