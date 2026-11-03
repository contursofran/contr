package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/owner"
)

func connectedWith(tree herdr.HerdrTree) herdr.LiveHerdr {
	return herdr.LiveHerdr{State: connected(), Tree: &tree}
}

// tabsOf is the tabs of project p: orchestrator "lead" (selected), workers b1 (working, issue
// #5 in worktree .../fix-x) and b2 (blocked, no issue).
func tabsOf() []AgentTab {
	return []AgentTab{
		{Address: "o", Label: "lead", Orchestrator: true, Icon: workerIcon(agents.Worker{Status: "idle", Live: true}), Selected: true},
		{Address: "w1", Label: "#5 · fix-x", Icon: workerIcon(agents.Worker{Status: "working", Live: true})},
		{Address: "w2", Label: "b2", Icon: workerIcon(agents.Worker{Status: "blocked", Live: true})},
	}
}

// barTest is a bare title bar in a 1160x40 window.
type barTest struct {
	bar       *titleBar
	tt        *ui.Tester
	picked    []AgentTab
	panelOpen bool
	toggles   int
}

func newBar(t *testing.T, tabs []AgentTab, panelOpen bool) *barTest {
	t.Helper()
	b := &barTest{bar: &titleBar{}, panelOpen: panelOpen}
	b.bar.selectTab = func(tab AgentTab) { b.picked = append(b.picked, tab) }
	b.bar.setTabs(tabs)
	b.tt = ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Label(LabelTitleBar).Fill().AlignItems(ui.Stretch).
			PaddingEnd(theme.TitleBarPadEnd).Children(func() {
			b.bar.build(c, b.panelOpen, func() { b.toggles++ })
		})
	}, 1160, 40)
	return b
}

func (b *barTest) click(t *testing.T, label string) {
	t.Helper()
	if err := b.tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func (b *barTest) has(label string) bool { _, ok := b.tt.Find(label); return ok }

func (b *barTest) rect(t *testing.T, label string) ui.Rect {
	t.Helper()
	return box(t, b.tt, label)
}

func inside(outer, inner ui.Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.W <= outer.X+outer.W && inner.Y+inner.H <= outer.Y+outer.H
}

func TestTabsShowOrchestratorFirstWithItsMark(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	lead := b.rect(t, "orchestrator lead, idle")
	w1 := b.rect(t, "#5 · fix-x, working")
	if lead.X >= w1.X {
		t.Errorf("orchestrator tab %+v is not left of the worker tab %+v", lead, w1)
	}
	if !b.tt.HasText(orchestratorMark + "lead") {
		t.Errorf("no mark before the orchestrator name; texts %q", b.tt.Texts())
	}
	if b.tt.HasText(orchestratorMark + "b2") {
		t.Error("a worker shows the mark")
	}
}

func TestOnlyTheSelectedTabIsUnderlined(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	lead := b.rect(t, "orchestrator lead, idle")
	underline := b.rect(t, LabelTabActive)
	if underline.H != theme.TabUnderline || underline.Y+underline.H != lead.Y+lead.H || !inside(lead, underline) {
		t.Errorf("underline %+v, want %d high at the bottom of the selected tab %+v", underline, theme.TabUnderline, lead)
	}
	tabs := tabsOf()
	tabs[0].Selected, tabs[2].Selected = false, true
	b2 := newBar(t, tabs, true)
	if got, want := b2.rect(t, LabelTabActive), b2.rect(t, "b2, blocked"); !inside(want, got) {
		t.Errorf("underline %+v is not under the b2 tab %+v", got, want)
	}
}

func TestClickingATabSelectsItsAgent(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.click(t, "b2, blocked")
	if len(b.picked) != 1 || b.picked[0].Address != "w2" || b.picked[0].Orchestrator {
		t.Errorf("picked %+v, want the w2 worker tab", b.picked)
	}
}

func TestClickingTheSelectedTabDoesNothing(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.click(t, "orchestrator lead, idle")
	b.bar.clickTab(0)
	b.bar.clickTab(99)
	b.bar.clickTab(-1)
	if len(b.picked) != 0 {
		t.Errorf("picked %+v", b.picked)
	}
}

func TestNeedsBadgeShowsTheCountAndHidesAtZero(t *testing.T) {
	tabs := tabsOf()
	tabs[0].Needs = 3
	b := newBar(t, tabs, true)
	lead := b.rect(t, "orchestrator lead, idle")
	if badge := b.rect(t, LabelNeedsBadge(3)); !inside(lead, badge) || !b.tt.HasText("3") {
		t.Errorf("badge %+v is not in the orchestrator tab %+v; texts %q", badge, lead, b.tt.Texts())
	}
	if b0 := newBar(t, tabsOf(), true); b0.has(LabelNeedsBadge(0)) || b0.tt.HasText("0") {
		t.Error("the badge shows at 0")
	}
}

func TestNoAgentsShowsOnlyReviewAndPanelToggle(t *testing.T) {
	b := newBar(t, nil, true)
	if b.has(LabelTabs) && b.rect(t, LabelTabs).W != 0 {
		t.Errorf("the tabs row has width %v", b.rect(t, LabelTabs).W)
	}
	if !b.has(LabelPanelToggle) || !b.has(LabelTitleDrag) {
		t.Error("the panel toggle and the drag stretch should show")
	}
	b.bar.toggleReview = func() {}
	b.tt.Frame()
	if !b.has(LabelReviewToggle) {
		t.Error("the Review button should show")
	}
}

func TestPanelToggleEmitsTogglePanel(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.click(t, LabelPanelToggle)
	if b.toggles != 1 || len(b.picked) != 0 {
		t.Errorf("toggles %d, picked %v; want one toggle and no selection", b.toggles, b.picked)
	}
}

func TestSetTabsNotifiesOnlyWhenTheTabsChange(t *testing.T) {
	b := &titleBar{}
	if !b.setTabs(tabsOf()) {
		t.Error("new tabs should change the bar")
	}
	if b.setTabs(tabsOf()) {
		t.Error("equal tabs should not change the bar")
	}
	if !b.setTabs(nil) {
		t.Error("no tabs should change the bar")
	}
}

func TestPanelGlyphFollowsThePanelState(t *testing.T) {
	// The open glyph has a filled part; the closed one has none. The bare bar has a white
	// background, so the filled part is darker.
	fill := func(open bool) uint32 {
		b := newBar(t, nil, open)
		b.tt.Frame()
		r := b.rect(t, LabelPanelToggle)
		img := b.tt.Image()
		// A point inside the right part of the glyph.
		x := int(r.X+r.W/2) + theme.TitleGlyphWidth/2 - 2 // inside the filled part
		y := int(r.Y + r.H/2)
		red, green, blue, _ := img.At(x, y).RGBA()
		return red + green + blue
	}
	if open, closed := fill(true), fill(false); open >= closed {
		t.Errorf("the open glyph is not filled on its right part: open %d, closed %d", open, closed)
	}
}

// The Rust bar held its own panel state and redrew only when the value changed. In the Go
// frame the state is the frame's and MyGo builds the view for each frame, so this checks that
// the toggle follows the state and keeps its place.
func TestThePanelToggleFollowsTheFrameAndKeepsItsPlace(t *testing.T) {
	f, _, tt := newTester(t, connected())
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if f.PanelOpen() {
		t.Error("the toggle should close the panel")
	}
	if got := box(t, tt, LabelPanelToggle).X + theme.TitleButtonSize; got != theme.WindowWidth-theme.TitleBarPadEnd {
		t.Errorf("toggle right edge %v, want %d", got, theme.WindowWidth-theme.TitleBarPadEnd)
	}
}

func TestManyTabsShrinkToTheMinimumThenClip(t *testing.T) {
	// 17 tabs at the minimum (17 x 72 = 1224) do not fit the 1124 px left after the buttons,
	// so the row clips.
	var tabs []AgentTab
	for n := range 17 {
		tabs = append(tabs, AgentTab{Address: fmt.Sprintf("a%d", n), Label: fmt.Sprintf("agent-number-%d", n),
			Icon: workerIcon(agents.Worker{Status: "idle", Live: true}), Selected: n == 0})
	}
	b := newBar(t, tabs, true)
	// MyGo reports the visible part of a clipped element: a tab that fits is at least the
	// minimum wide, and the row cuts the tabs that do not.
	row := b.rect(t, LabelTabs)
	for n := range 15 {
		tab := b.rect(t, fmt.Sprintf("agent-number-%d, idle", n))
		if tab.W < theme.TabMinWidth {
			t.Errorf("tab %d is %v wide, want at least %d", n, tab.W, theme.TabMinWidth)
		}
	}
	if last := b.rect(t, "agent-number-15, idle"); last.W >= theme.TabMinWidth || last.X+last.W > row.X+row.W {
		t.Errorf("tab 15 %+v should be cut by the row %+v", last, row)
	}
	toggle := b.rect(t, LabelPanelToggle)
	if toggle.X+toggle.W != 1160-theme.TitleBarPadEnd || toggle.W != theme.TitleButtonSize {
		t.Errorf("toggle %+v, want %d wide at 8 px from the right edge", toggle, theme.TitleButtonSize)
	}
}

// ---- The bar follows the agent state (through the frame) ----

// agentFrame is a frame whose agents come from snap, with a selection.
func agentFrame(t *testing.T, snap *agentstate.Snapshot) (*Frame, *ui.Tester) {
	t.Helper()
	f, _, tt := newTester(t, connected())
	f.Selection = state.NewSelection(nil)
	f.agentSnap = func() agentstate.Snapshot { return *snap }
	return f, tt
}

func tabSnap() agentstate.Snapshot {
	w1 := workW("w1", "b1", "p")
	w1.Status = "working"
	w2 := workW("w2", "b2", "p")
	w2.Status = "idle"
	other := workW("w3", "b3", "q")
	other.Status = "idle"
	o := orchW("o", "lead", "p")
	o.Status = "idle"
	return agentstate.Snapshot{Orchestrators: []agents.Worker{o}, Workers: []agents.Worker{w1, w2, other}}
}

func TestTheBarShowsTheTabsOfTheStartProject(t *testing.T) {
	snap := tabSnap()
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	for _, label := range []string{"orchestrator lead, idle", "b1, working", "b2, idle"} {
		if absent(tt, label) {
			t.Errorf("no tab %q; texts %q", label, tt.Texts())
		}
	}
	if !absent(tt, "b3, idle") {
		t.Error("a worker of another project shows")
	}
	if !absent(tt, LabelTabActive) {
		t.Error("a tab is underlined with no selection")
	}
}

func TestClickingATabInTheFrameSelectsIt(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	tt.Frame()
	if err := tt.Click("b1, working"); err != nil {
		t.Fatal(err)
	}
	if sel, ok := f.Selection.Current(); !ok || sel != (state.Selected{Kind: state.KindWorker, ID: "w1"}) {
		t.Errorf("selection %+v %v", sel, ok)
	}
	tt.Frame()
	if absent(tt, LabelTabActive) {
		t.Error("the selected tab is not underlined")
	}
	if err := tt.Click("orchestrator lead, idle"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindOrchestrator, ID: "o"}) {
		t.Errorf("selection %+v, want the orchestrator", sel)
	}
}

func TestSelectingAWorktreeKeepsTheOpenProjectTabs(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w3"})
	tt.Frame()
	if absent(tt, "b3, idle") || !absent(tt, "b1, working") {
		t.Fatalf("the bar should show project q; texts %q", tt.Texts())
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: "/repo/wt"})
	tt.Frame()
	if absent(tt, "b3, idle") || !absent(tt, LabelTabActive) {
		t.Errorf("a worktree selection should keep project q and underline nothing; texts %q", tt.Texts())
	}
}

func TestAWorkerGoneFromTheSnapshotLeavesTheBar(t *testing.T) {
	snap := tabSnap()
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	snap.Workers = snap.Workers[1:]
	tt.Frame()
	if !absent(tt, "b1, working") || absent(tt, "b2, idle") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestAStatusChangeInTheSnapshotRedrawsTheIcon(t *testing.T) {
	snap := tabSnap()
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	snap.Workers = append([]agents.Worker(nil), snap.Workers...)
	snap.Workers[0].Status = "blocked"
	tt.Frame()
	if absent(tt, "b1, blocked") || !absent(tt, "b1, working") {
		t.Errorf("the tab should show blocked; texts %q", tt.Texts())
	}
}

func TestTheFrameBadgeCountsTheOpenItemsOfTheProject(t *testing.T) {
	snap := tabSnap()
	snap.Questions = []owner.Question{{ID: "D1", Project: "p", Title: "open"}}
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	if absent(tt, LabelNeedsBadge(1)) {
		t.Errorf("no badge; texts %q", tt.Texts())
	}
}

func TestNoAgentSourceShowsNoTabs(t *testing.T) {
	_, _, tt := newTester(t, connected())
	if !absent(tt, LabelTabs) && box(t, tt, LabelTabs).W != 0 {
		t.Error("the tabs row shows without agents")
	}
	if absent(tt, LabelPanelToggle) {
		t.Error("the panel toggle should stay")
	}
}
