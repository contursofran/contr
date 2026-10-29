package app

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
)

func item(id, label string, status herdr.AgentState, active bool) herdr.TabItem {
	return herdr.TabItem{ID: id, Label: label, Status: status, Active: active}
}

// threeTabs is w1 with three tabs: t1 working and active, t2 blocked, t3 unknown. Split target p1.
func threeTabs() herdr.TabStrip {
	return herdr.TabStrip{
		WorkspaceID: "w1",
		Tabs: []herdr.TabItem{
			item("w1:t1", "api", herdr.StateWorking, true),
			item("w1:t2", "docs", herdr.StateBlocked, false),
			item("w1:t3", "3", herdr.StateUnknown, false),
		},
		SplitPaneID: "w1:p1",
	}
}

// barTest is a bare title bar in a 1160x40 window.
type barTest struct {
	bar       *titleBar
	tt        *ui.Tester
	sent      []TabCommand
	panelOpen bool
	toggles   int
}

func newBar(t *testing.T, strip herdr.TabStrip, panelOpen bool) *barTest {
	t.Helper()
	b := &barTest{bar: &titleBar{}, panelOpen: panelOpen}
	b.bar.send = func(c TabCommand) { b.sent = append(b.sent, c) }
	b.bar.setStrip(strip)
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

func TestTabsShowTheirIconsAndOnlyTheActiveOneIsUnderlined(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	api := b.rect(t, "api, working")
	docs := b.rect(t, "docs, blocked")
	other := b.rect(t, "3, idle") // unknown shows idle (Rust D19)
	if b.has("4, idle") {
		t.Error("a fourth tab shows")
	}
	underline := b.rect(t, LabelTabActive)
	if underline.H != theme.TabUnderline || underline.Y+underline.H != api.Y+api.H {
		t.Errorf("underline %+v, want %d high at the bottom of the active tab %+v", underline, theme.TabUnderline, api)
	}
	if !inside(api, underline) {
		t.Errorf("underline %+v is not inside the active tab %+v", underline, api)
	}
	for name, tab := range map[string]ui.Rect{"working": api, "blocked": docs, "idle": other} {
		icon := b.rect(t, name)
		if !inside(tab, icon) {
			t.Errorf("icon %q %+v is not inside its tab %+v", name, icon, tab)
		}
	}
}

func TestClickingAnInactiveTabAsksToFocusIt(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	b.click(t, "docs, blocked")
	if want := []TabCommand{{TabFocus, "w1:t2"}}; !reflect.DeepEqual(b.sent, want) {
		t.Errorf("sent %v, want %v", b.sent, want)
	}
}

func TestClickingTheActiveTabEmitsNothing(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	b.click(t, "api, working")
	if len(b.sent) != 0 {
		t.Errorf("sent %v", b.sent)
	}
}

func TestPlusAsksForATabInTheWorkspace(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	b.click(t, LabelTabNew)
	if want := []TabCommand{{TabCreate, "w1"}}; !reflect.DeepEqual(b.sent, want) {
		t.Errorf("sent %v, want %v", b.sent, want)
	}
}

func TestAClickOnATabOutsideTheStripEmitsNothing(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	b.bar.clickTab(99)
	b.bar.clickTab(-1)
	if len(b.sent) != 0 {
		t.Errorf("sent %v", b.sent)
	}
}

func TestSplitAsksToSplitTheActivePaneRight(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	b.click(t, LabelSplit)
	if want := []TabCommand{{TabSplitRight, "w1:p1"}}; !reflect.DeepEqual(b.sent, want) {
		t.Errorf("sent %v, want %v", b.sent, want)
	}
}

func TestPanelToggleEmitsTogglePanel(t *testing.T) {
	b := newBar(t, threeTabs(), true)
	b.click(t, LabelPanelToggle)
	if b.toggles != 1 || len(b.sent) != 0 {
		t.Errorf("toggles %d, sent %v; want one toggle and no command", b.toggles, b.sent)
	}
}

func TestEmptyStripShowsOnlyThePanelToggle(t *testing.T) {
	b := newBar(t, herdr.TabStrip{}, true)
	for _, label := range []string{LabelTabNew, LabelSplit} {
		if b.has(label) {
			t.Errorf("%q shows with an empty strip", label)
		}
	}
	if b.has(LabelTabs) && b.rect(t, LabelTabs).W != 0 {
		t.Errorf("the tabs row has width %v", b.rect(t, LabelTabs).W)
	}
	if !b.has(LabelPanelToggle) || !b.has(LabelTitleDrag) {
		t.Error("the panel toggle and the drag stretch should show")
	}
}

func TestWorkspaceWithoutAPaneHidesSplitButKeepsPlus(t *testing.T) {
	b := newBar(t, herdr.TabStrip{WorkspaceID: "w1"}, true)
	if b.has(LabelSplit) || !b.has(LabelTabNew) {
		t.Fatal("split should be hidden and plus shown")
	}
	b.click(t, LabelTabNew)
	if want := []TabCommand{{TabCreate, "w1"}}; !reflect.DeepEqual(b.sent, want) {
		t.Errorf("sent %v, want %v", b.sent, want)
	}
}

func TestPanelGlyphFollowsThePanelState(t *testing.T) {
	// The open glyph has a filled part; the closed one has none. The bare bar has a white
	// background, so the filled part is darker.
	fill := func(open bool) uint32 {
		b := newBar(t, herdr.TabStrip{}, open)
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

func TestSetStripNotifiesOnlyWhenTheStripChanges(t *testing.T) {
	b := &titleBar{}
	if !b.setStrip(threeTabs()) {
		t.Error("a new strip should change the bar")
	}
	if b.setStrip(threeTabs()) {
		t.Error("an equal strip should not change the bar")
	}
	if !b.setStrip(herdr.TabStrip{}) {
		t.Error("an empty strip should change the bar")
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
	// 16 tabs at the minimum (16 x 72 = 1152) do not fit the 1068 px left after "+" and the
	// buttons, so the row clips.
	strip := herdr.TabStrip{WorkspaceID: "w1", SplitPaneID: "w1:p1"}
	for n := range 16 {
		strip.Tabs = append(strip.Tabs, item(fmt.Sprintf("w1:t%d", n), fmt.Sprintf("agent-number-%d", n), herdr.StateIdle, n == 0))
	}
	b := newBar(t, strip, true)
	// MyGo reports the visible part of a clipped element: a tab that fits is at least the
	// minimum wide, and the row cuts the tabs that do not.
	row := b.rect(t, LabelTabs)
	for n := range 14 {
		tab := b.rect(t, fmt.Sprintf("agent-number-%d, idle", n))
		if tab.W < theme.TabMinWidth {
			t.Errorf("tab %d is %v wide, want at least %d", n, tab.W, theme.TabMinWidth)
		}
	}
	if last := b.rect(t, "agent-number-14, idle"); last.W >= theme.TabMinWidth || last.X+last.W > row.X+row.W {
		t.Errorf("tab 14 %+v should be cut by the row %+v", last, row)
	}
	toggle := b.rect(t, LabelPanelToggle)
	if toggle.X+toggle.W != 1160-theme.TitleBarPadEnd || toggle.W != theme.TitleButtonSize {
		t.Errorf("toggle %+v, want %d wide at 8 px from the right edge", toggle, theme.TitleButtonSize)
	}
}

// ---- The strip follows the herdr tree (through the frame) ----

func hpane(id string, focused bool) herdr.Pane { return herdr.Pane{ID: id, Focused: focused} }

func htab(id, label string, status herdr.AgentState, focused bool, p herdr.Pane) herdr.Tab {
	return herdr.Tab{ID: id, Label: label, Status: status, Focused: focused, Panes: []herdr.Pane{p}}
}

// sampleTree is the tree that these tests start from. w1 is the focused workspace: "api" (t1,
// working, focused, pane w1:p1) and "docs" (t2, blocked, pane w1:p2). w2 has "other" (t3, idle).
// herdr's focused pane is w1:p1. Tests change a copy of it.
func sampleTree() herdr.HerdrTree {
	focused := "w1:p1"
	return herdr.HerdrTree{
		Version:       "0.9.3",
		FocusedPaneID: &focused,
		Workspaces: []herdr.Workspace{
			{ID: "w1", Label: "w1", Status: herdr.StateWorking, Focused: true, Tabs: []herdr.Tab{
				htab("w1:t1", "api", herdr.StateWorking, true, hpane("w1:p1", true)),
				htab("w1:t2", "docs", herdr.StateBlocked, false, hpane("w1:p2", false)),
			}},
			{ID: "w2", Label: "w2", Status: herdr.StateIdle, Tabs: []herdr.Tab{
				htab("w2:t3", "other", herdr.StateIdle, true, hpane("w2:p3", true)),
			}},
		},
	}
}

func connectedWith(tree herdr.HerdrTree) herdr.LiveHerdr {
	return herdr.LiveHerdr{State: connected(), Tree: &tree}
}

func TestTheStripStartsFromTheHerdrTree(t *testing.T) {
	_, live, tt := newTester(t, connected())
	live.setLive(connectedWith(sampleTree()))
	tt.Frame()
	for _, label := range []string{"api, working", "docs, blocked", LabelTabActive, LabelTabNew, LabelSplit} {
		if absent(tt, label) {
			t.Errorf("no %q", label)
		}
	}
	if !absent(tt, "other, idle") {
		t.Error("w2 is not the current workspace")
	}
}

func TestAStatusChangeFromHerdrRedrawsTheIcon(t *testing.T) {
	_, live, tt := newTester(t, connected())
	live.setLive(connectedWith(sampleTree()))
	tt.Frame()
	tree := sampleTree()
	tree.Workspaces[0].Tabs[0].Status = herdr.StateIdle
	live.setLive(connectedWith(tree))
	tt.Frame()
	if absent(tt, "api, idle") || !absent(tt, "api, working") {
		t.Errorf("the tab should show idle; texts %q", tt.Texts())
	}
}

func TestAFocusChangeFromHerdrMovesTheUnderline(t *testing.T) {
	_, live, tt := newTester(t, connected())
	live.setLive(connectedWith(sampleTree()))
	tt.Frame()
	before := box(t, tt, LabelTabActive)
	tree := sampleTree()
	tree.Workspaces[0].Tabs[0].Focused = false
	tree.Workspaces[0].Tabs[1].Focused = true
	p2 := "w1:p2"
	tree.FocusedPaneID = &p2
	live.setLive(connectedWith(tree))
	tt.Frame()
	after := box(t, tt, LabelTabActive)
	if after.X <= before.X || !inside(box(t, tt, "docs, blocked"), after) {
		t.Errorf("underline %+v -> %+v, want it under the docs tab", before, after)
	}
}

func TestAChangeInAnotherWorkspaceDoesNotRedraw(t *testing.T) {
	var b titleBar
	b.setStrip(herdr.TabStripFromTree(ptr(sampleTree())))
	other := sampleTree()
	other.Workspaces[1].Tabs[0].Status = herdr.StateWorking
	if b.setStrip(herdr.TabStripFromTree(&other)) {
		t.Error("a change in another workspace changed the bar")
	}
	// The same check the other way round: a change in this workspace does change the bar.
	own := sampleTree()
	own.Workspaces[0].Tabs[1].Status = herdr.StateDone
	if !b.setStrip(herdr.TabStripFromTree(&own)) {
		t.Error("a change in this workspace did not change the bar")
	}
}

func ptr[T any](v T) *T { return &v }

func TestLosingHerdrRemovesTheTabs(t *testing.T) {
	_, live, tt := newTester(t, connected())
	live.setLive(connectedWith(sampleTree()))
	tt.Frame()
	if absent(tt, "api, working") {
		t.Fatal("the tab should show")
	}
	live.setLive(herdr.LiveHerdr{State: notRunning()})
	tt.Frame()
	for _, label := range []string{"api, working", LabelTabNew, LabelSplit} {
		if !absent(tt, label) {
			t.Errorf("%q still shows", label)
		}
	}
	if absent(tt, LabelPanelToggle) {
		t.Error("the panel toggle should stay")
	}
}
