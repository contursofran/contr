package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/tabmodel"
)

// ---- model: order and move ----

// ---- middle-click ----

func TestMiddleClickClosesOnReleaseInsideTheTab(t *testing.T) {
	box := ui.Rect{W: 120, H: 30}
	closed := 0
	closeFn := func() { closed++ }
	if !middleClick(ui.InputEvent{Kind: ui.InputPointerDown, Button: 2, X: 10, Y: 10}, box, closeFn) || closed != 0 {
		t.Fatalf("the press must be taken and close nothing; closed %d", closed)
	}
	if !middleClick(ui.InputEvent{Kind: ui.InputPointerUp, Button: 2, X: 10, Y: 10}, box, closeFn) || closed != 1 {
		t.Fatalf("the release must close the tab; closed %d", closed)
	}
	// A release outside the tab is a cancel.
	middleClick(ui.InputEvent{Kind: ui.InputPointerUp, Button: 2, X: 200, Y: 10}, box, closeFn)
	middleClick(ui.InputEvent{Kind: ui.InputPointerUp, Button: 2, X: 10, Y: -4}, box, closeFn)
	if closed != 1 {
		t.Errorf("a release outside closed the tab; closed %d", closed)
	}
}

func TestOtherButtonsAndEventsAreNotMiddleClicks(t *testing.T) {
	box := ui.Rect{W: 120, H: 30}
	closed := 0
	for _, ev := range []ui.InputEvent{
		{Kind: ui.InputPointerDown, Button: 0, X: 5, Y: 5},
		{Kind: ui.InputPointerUp, Button: 0, X: 5, Y: 5},
		{Kind: ui.InputPointerUp, Button: 1, X: 5, Y: 5},
		{Kind: ui.InputPointerMove, Button: 2, X: 5, Y: 5},
		{Kind: ui.InputKeyDown, X: 5, Y: 5},
	} {
		if middleClick(ev, box, func() { closed++ }) {
			t.Errorf("event %+v was taken", ev)
		}
	}
	if closed != 0 {
		t.Errorf("closed %d", closed)
	}
}

// ---- context menu on the bar ----

func TestTabMenuListsCloseAndCloseOthers(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.bar.closeOthers = func(tabmodel.Tab) {}
	b.tt.Frame()
	if err := b.tt.RightClick("go test ./..."); err != nil {
		t.Fatal(err)
	}
	if got, want := b.tt.Menu(), []string{MenuClose, MenuCloseOthers}; !slices.Equal(got, want) {
		t.Errorf("menu %q, want %q", got, want)
	}
}

func TestTabMenuCloseClosesThatPlainTab(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	if err := b.tt.RightClick("$ repo"); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(MenuClose); err != nil {
		t.Fatal(err)
	}
	if len(b.closed) != 1 || b.closed[0].Address != "t2" {
		t.Errorf("closed %+v, want t2", b.closed)
	}
}

func TestTabMenuCloseOthersKeepsTheTabThatWasClicked(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var kept []tabmodel.Tab
	b.bar.closeOthers = func(k tabmodel.Tab) { kept = append(kept, k) }
	b.tt.Frame()
	if err := b.tt.RightClick("go test ./..."); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(MenuCloseOthers); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Address != "t1" {
		t.Errorf("close others kept %+v, want t1", kept)
	}
	if len(b.closed) != 0 {
		t.Errorf("the tab itself closed: %+v", b.closed)
	}
}

func TestTabMenuOnAnAgentTabCannotCloseIt(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var kept []tabmodel.Tab
	b.bar.closeOthers = func(k tabmodel.Tab) { kept = append(kept, k) }
	b.tt.Frame()
	if err := b.tt.RightClick("lead"); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(MenuClose); err == nil {
		t.Error("Close on the agent tab could be chosen")
	}
	if len(b.closed) != 0 {
		t.Errorf("closed %+v", b.closed)
	}
	// Close others on the agent tab is for the plain terminals; the frame never closes the agent.
	if err := b.tt.ChooseMenuItem(MenuCloseOthers); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Address != "o" {
		t.Errorf("close others kept %+v, want o", kept)
	}
}

func TestTabMenuCloseOthersIsOffWithoutAnotherPlainTab(t *testing.T) {
	b := newBar(t, []tabmodel.Tab{{Address: "o", Title: "lead", Orchestrator: true, Selected: true},
		{Address: "t1", Title: "only", Plain: true}}, true)
	called := 0
	b.bar.closeOthers = func(tabmodel.Tab) { called++ }
	b.tt.Frame()
	if err := b.tt.RightClick("only"); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(MenuCloseOthers); err == nil || called != 0 {
		t.Errorf("Close others on the only plain tab: err %v, called %d", err, called)
	}
}

// ---- drag on the bar ----

type move struct{ from, to string }

func dragTab(t *testing.T, b *barTest, from, to string) {
	t.Helper()
	a, z := b.rect(t, from), b.rect(t, to)
	b.tt.Press(a.X+a.W/2, a.Y+a.H/2)
	b.tt.Move(a.X+a.W/2+12, a.Y+a.H/2)
	b.tt.Move(z.X+z.W/2, z.Y+z.H/2)
	b.tt.Release(z.X+z.W/2, z.Y+z.H/2)
	b.tt.Frame()
}

func TestDraggingAPlainTabOntoAnotherMovesIt(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var moves []move
	b.bar.moveTab = func(from, to string) { moves = append(moves, move{from, to}) }
	b.tt.Frame()
	dragTab(t, b, "go test ./...", "$ repo")
	if !slices.Equal(moves, []move{{"t1", "t2"}}) {
		t.Errorf("moves %v, want t1 onto t2", moves)
	}
	if len(b.picked) != 0 {
		t.Errorf("a drag selected a tab: %+v", b.picked)
	}
}

func TestDraggingAnAgentTabOrOntoItMovesNothing(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var moves []move
	b.bar.moveTab = func(from, to string) { moves = append(moves, move{from, to}) }
	b.tt.Frame()
	dragTab(t, b, "lead", "go test ./...")
	dragTab(t, b, "go test ./...", "lead")
	if len(moves) != 0 {
		t.Errorf("moves %v, want none", moves)
	}
}

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
