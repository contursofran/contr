package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/state"
)

// ---- model: order and move ----

func plainTabs(addrs ...string) []Tab {
	tabs := []Tab{{Address: "o", Title: "lead", Orchestrator: true}}
	for _, a := range addrs {
		tabs = append(tabs, Tab{Address: a, Title: a, Plain: true})
	}
	return tabs
}

func TestOrderTabsKeepsTheAgentFirstAndFollowsTheOrder(t *testing.T) {
	got := addrs(OrderTabs(plainTabs("t1", "t2", "t3"), []string{"t3", "t1", "t2"}))
	if want := []string{"o", "t3", "t1", "t2"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	// An order that names the agent tab does not move it.
	got = addrs(OrderTabs(plainTabs("t1", "t2"), []string{"t2", "o", "t1"}))
	if want := []string{"o", "t2", "t1"}; !slices.Equal(got, want) {
		t.Errorf("order with the agent named %v, want %v", got, want)
	}
}

func TestOrderTabsPutsUnknownTabsAfterTheKnownOnesInTheirOwnOrder(t *testing.T) {
	got := addrs(OrderTabs(plainTabs("t1", "new1", "t2", "new2"), []string{"t2", "gone", "t1"}))
	if want := []string{"o", "t2", "t1", "new1", "new2"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if got := addrs(OrderTabs(plainTabs("t1", "t2"), nil)); !slices.Equal(got, []string{"o", "t1", "t2"}) {
		t.Errorf("no order changed the tabs: %v", got)
	}
}

func TestMoveTabTakesThePlaceOfTheTabItIsDroppedOn(t *testing.T) {
	tabs := plainTabs("t1", "t2", "t3")
	cases := []struct {
		from, to string
		want     []string
	}{
		{"t1", "t3", []string{"t2", "t3", "t1"}}, // right: after the target
		{"t3", "t1", []string{"t3", "t1", "t2"}}, // left: before the target
		{"t1", "t2", []string{"t2", "t1", "t3"}},
	}
	for _, c := range cases {
		got, ok := MoveTab(tabs, c.from, c.to)
		if !ok || !slices.Equal(got, c.want) {
			t.Errorf("MoveTab(%s, %s) = %v, %v; want %v", c.from, c.to, got, ok, c.want)
		}
	}
}

func TestMoveTabNeverMovesOrDisplacesTheAgentTab(t *testing.T) {
	tabs := plainTabs("t1", "t2")
	for _, c := range [][2]string{{"o", "t1"}, {"t1", "o"}, {"t1", "t1"}, {"t1", "nope"}, {"nope", "t1"}} {
		if got, ok := MoveTab(tabs, c[0], c[1]); ok {
			t.Errorf("MoveTab(%s, %s) = %v, want no move", c[0], c[1], got)
		}
	}
}

func TestOnlyAPlainTabIsMovable(t *testing.T) {
	if (Tab{Orchestrator: true}).Movable() || (Tab{}).Movable() {
		t.Error("an agent tab is movable")
	}
	if !(Tab{Plain: true}).Movable() {
		t.Error("a plain tab is not movable")
	}
}

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
	b.bar.closeOthers = func(Tab) {}
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
	var kept []Tab
	b.bar.closeOthers = func(k Tab) { kept = append(kept, k) }
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
	var kept []Tab
	b.bar.closeOthers = func(k Tab) { kept = append(kept, k) }
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
	b := newBar(t, []Tab{{Address: "o", Title: "lead", Orchestrator: true, Selected: true},
		{Address: "t1", Title: "only", Plain: true}}, true)
	called := 0
	b.bar.closeOthers = func(Tab) { called++ }
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
	cf.closeOtherTabs(Tab{Address: "tp2", Plain: true})
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
	cf.closeOtherTabs(Tab{Address: "o", Orchestrator: true})
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
	cf.closeTab(Tab{Address: "tp", Plain: true})
	if got := termsOfProjectP(cf); !slices.Equal(got, []string{"o", "tp3", "tp2"}) {
		t.Errorf("after closing tp: %v", got)
	}
}
