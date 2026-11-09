package app

import (
	"slices"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/tabmodel"
)

// ---- model: order and move ----

// ---- middle-click ----

// ---- context menu on the bar ----

// ---- through the frame ----

func termsOfProjectP(cf *closeFrame) []string {
	var out []string
	for _, tab := range cf.titleTabs() {
		out = append(out, tab.Address)
	}
	return out
}

func newThreeTabFrame(t *testing.T) *closeFrame {
	t.Helper()
	snap := termSnap()
	snap.Terminals = append(snap.Terminals,
		agents.Worker{Name: "term-2", Address: "tp2", Project: "p", Role: agents.RoleTerminal, Worktree: "/work/p", Live: true},
		agents.Worker{Name: "term-3", Address: "tp3", Project: "p", Role: agents.RoleTerminal, Worktree: "/work/p", Live: true})
	cf := newCloseFrame(t, &snap)
	cf.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	cf.tt.Frame()
	return cf
}

func TestCloseOthersStopsTheOtherPlainTerminalsOnly(t *testing.T) {
	cf := newThreeTabFrame(t)
	cf.closeOtherTabs(tabmodel.Tab{Address: "tp2", Plain: true})
	if slices.Contains(cf.stopped, "tp2") || slices.Contains(cf.stopped, "o") {
		t.Errorf("stopped %v: the kept tab or the agent was stopped", cf.stopped)
	}
	slices.Sort(cf.stopped)
	if want := []string{"tp", "tp3"}; !slices.Equal(cf.stopped, want) {
		t.Errorf("stopped %v, want %v", cf.stopped, want)
	}
}

func TestCloseOthersFromTheAgentTabStopsEveryPlainTerminalButNotTheAgent(t *testing.T) {
	cf := newThreeTabFrame(t)
	cf.closeOtherTabs(tabmodel.Tab{Address: "o", Orchestrator: true})
	slices.Sort(cf.stopped)
	if want := []string{"tp", "tp2", "tp3"}; !slices.Equal(cf.stopped, want) {
		t.Errorf("stopped %v, want %v", cf.stopped, want)
	}
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o"}) {
		t.Errorf("tabs left %v, want [o]", got)
	}
}

func TestFrameMoveTabReordersThePlainTabsAndKeepsTheAgentFirst(t *testing.T) {
	cf := newThreeTabFrame(t)
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o", "tp", "tp2", "tp3"}) {
		t.Fatalf("start %v", got)
	}
	cf.moveTab("tp3", "tp")
	cf.tt.Frame() // the bar takes the new order, as the frame after a drop does
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o", "tp3", "tp", "tp2"}) {
		t.Errorf("after moving tp3 to the front: %v", got)
	}
	cf.moveTab("tp3", "tp2")
	cf.tt.Frame()
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o", "tp", "tp2", "tp3"}) {
		t.Errorf("after moving tp3 to the end: %v", got)
	}
	// The agent's tab neither moves nor takes a drop.
	cf.moveTab("o", "tp2")
	cf.moveTab("tp2", "o")
	cf.tt.Frame()
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o", "tp", "tp2", "tp3"}) {
		t.Errorf("a move with the agent tab changed the order: %v", got)
	}
}

func TestAClosedTerminalLeavesTheOrderOfTheOthers(t *testing.T) {
	cf := newThreeTabFrame(t)
	cf.moveTab("tp3", "tp")
	cf.closeTab(tabmodel.Tab{Address: "tp", Plain: true})
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o", "tp3", "tp2"}) {
		t.Errorf("after closing tp: %v", got)
	}
}
