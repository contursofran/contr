package app

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
	"github.com/contursofran/contr/internal/review"
)

// tabsOf is the tabs of project p: the orchestrator "lead" (selected), a plain terminal with a
// title and a plain terminal with none.
func tabsOf() []Tab {
	return []Tab{
		{Address: "o", Title: "lead", Orchestrator: true, Selected: true},
		{Address: "t1", Title: "go test ./...", Plain: true},
		{Address: "t2", Title: "$ repo", Plain: true},
	}
}

// barTest is a bare title bar in a 1160x40 window.
type barTest struct {
	bar       *titleBar
	tt        *ui.Tester
	picked    []Tab
	closed    []Tab
	news      int
	searches  int
	panelOpen bool
	toggles   int
}

func newBar(t *testing.T, tabs []Tab, panelOpen bool) *barTest {
	t.Helper()
	b := &barTest{bar: &titleBar{}, panelOpen: panelOpen}
	b.bar.selectTab = func(tab Tab) { b.picked = append(b.picked, tab) }
	b.bar.closeTab = func(tab Tab) { b.closed = append(b.closed, tab) }
	b.bar.newTab = func() { b.news++ }
	b.bar.search = func() { b.searches++ }
	b.bar.setTabs(tabs)
	b.tt = ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Label(LabelTitleBar).Fill().AlignItems(ui.Center).Background(theme.Current().Ground).
			PaddingEnd(theme.TitleBarPadEnd).Children(func() {
			b.bar.build(c, b.panelOpen, false, func() { b.toggles++ })
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

func TestTabsShowTheirTitlesAndGlyphs(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	lead, t1 := b.rect(t, "lead"), b.rect(t, "go test ./...")
	if lead.X >= t1.X {
		t.Errorf("agent tab %+v is not left of the plain tab %+v", lead, t1)
	}
	if lead.H != theme.SizeRowHeight {
		t.Errorf("tab is %v high, want %d", lead.H, theme.SizeRowHeight)
	}
	for _, text := range []string{"lead", "go test ./...", "$ repo"} {
		if !b.tt.HasText(text) {
			t.Errorf("no text %q; texts %q", text, b.tt.Texts())
		}
	}
	// The orchestrator shows the Claude glyph, which says its state.
	if !b.has("idle") {
		t.Error("the orchestrator tab has no status glyph")
	}
}

func TestAWorkerTabGlyphSaysItsState(t *testing.T) {
	tabs := []Tab{{Address: "w", Title: "#5 · fix-x", State: tabState(agents.Worker{Status: "blocked", Live: true}), Selected: true}}
	b := newBar(t, tabs, true)
	if !b.has("needs you") {
		t.Errorf("a blocked agent shows no needs-you glyph; texts %q", b.tt.Texts())
	}
}

func TestClickingATabSelectsIt(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.click(t, "go test ./...")
	if len(b.picked) != 1 || b.picked[0].Address != "t1" || !b.picked[0].Plain {
		t.Errorf("picked %+v, want the t1 plain tab", b.picked)
	}
}

func TestClickingTheSelectedTabDoesNothing(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.click(t, "lead")
	if len(b.picked) != 0 {
		t.Errorf("picked %+v", b.picked)
	}
}

func TestOnlyTheSelectedPlainTabHasACloseButton(t *testing.T) {
	// The selected agent tab: no close button (D85).
	if b := newBar(t, tabsOf(), true); b.has(LabelCloseTab) {
		t.Error("an agent tab has a close button")
	}
	// A selected plain tab: one close button; it closes that tab and selects nothing.
	tabs := tabsOf()
	tabs[0].Selected, tabs[1].Selected = false, true
	b := newBar(t, tabs, true)
	t1 := b.rect(t, "go test ./...")
	if closeBtn := b.rect(t, LabelCloseTab); !inside(t1, closeBtn) || closeBtn.W != 16 || closeBtn.H != 16 {
		t.Errorf("close button %+v is not a 16px box in the selected tab %+v", closeBtn, t1)
	}
	b.click(t, LabelCloseTab)
	if len(b.closed) != 1 || b.closed[0].Address != "t1" || len(b.picked) != 0 {
		t.Errorf("closed %+v, picked %+v; want t1 closed and nothing picked", b.closed, b.picked)
	}
}

func TestNewTabButtonFollowsTheTabsAndStaysWithNoTabs(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	row, plus := b.rect(t, LabelTabs), b.rect(t, LabelNewTab)
	if plus.X < row.X+row.W-0.5 || plus.W != theme.SizeIconButton {
		t.Errorf("the + button %+v is not right after the tabs %+v", plus, row)
	}
	b.click(t, LabelNewTab)
	if b.news != 1 {
		t.Errorf("+ ran newTab %d times", b.news)
	}
	if e := newBar(t, nil, true); !e.has(LabelNewTab) {
		t.Error("the + button hides with no tabs")
	}
	none := newBar(t, nil, true)
	none.bar.newTab = nil
	none.tt.Frame()
	if none.has(LabelNewTab) {
		t.Error("the + button shows with no newTab action")
	}
}

func TestTheBarHasNoSplitNorPRButton(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	for _, label := range []string{"Split", "Pull request", "Review"} {
		if b.has(label) {
			t.Errorf("the bar has a %q button", label)
		}
	}
}

func TestNoTabsShowsOnlyPlusAndTheFilesToggle(t *testing.T) {
	b := newBar(t, nil, true)
	if b.has(LabelTabs) && b.rect(t, LabelTabs).W != 0 {
		t.Errorf("the tabs row has width %v", b.rect(t, LabelTabs).W)
	}
	if !b.has(LabelFilesToggle) || !b.has(LabelTitleDrag) || !b.has(LabelNewTab) {
		t.Errorf("a part is missing; texts %q", b.tt.Texts())
	}
}

func TestPanelToggleEmitsTogglePanel(t *testing.T) {
	b := newBar(t, nil, true)
	b.click(t, LabelFilesToggle)
	if b.toggles != 1 {
		t.Errorf("toggles = %d, want 1", b.toggles)
	}
}

func TestSetTabsNotifiesOnlyWhenTheTabsChange(t *testing.T) {
	var bar titleBar
	if !bar.setTabs(tabsOf()) {
		t.Error("new tabs did not report a change")
	}
	if bar.setTabs(tabsOf()) {
		t.Error("equal tabs reported a change")
	}
	changed := tabsOf()
	changed[1].Title = "vim"
	if !bar.setTabs(changed) {
		t.Error("a changed title did not report a change")
	}
}

func inside(outer, inner ui.Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.W <= outer.X+outer.W && inner.Y+inner.H <= outer.Y+outer.H
}

func TestFilesToggleIsPressedWhileTheFilesShow(t *testing.T) {
	// A pressed toggle draws its icon in a brighter ink than a released one, so the
	// pixels of its box differ.
	ink := func(open bool) uint32 {
		b := newBar(t, nil, open)
		b.tt.Frame()
		r := b.rect(t, LabelFilesToggle)
		img := b.tt.Image()
		var sum uint32
		for y := int(r.Y); y < int(r.Y+r.H); y++ {
			for x := int(r.X); x < int(r.X+r.W); x++ {
				red, green, blue, _ := img.At(x, y).RGBA()
				sum += red + green + blue
			}
		}
		return sum
	}
	if open, closed := ink(true), ink(false); open == closed {
		t.Errorf("the toggle looks the same open and closed: %d", open)
	}
}

// The state of the toggle is the frame's, and MyGo builds the view for each frame, so this
// checks that the toggle follows the state and keeps its place.
func TestThePanelToggleFollowsTheFrameAndKeepsItsPlace(t *testing.T) {
	f, tt := newTester(t)
	if err := tt.Click(LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	if f.PanelOpen() {
		t.Error("the toggle should close the panel")
	}
	if got := box(t, tt, LabelFilesToggle).X + theme.SizeIconButton; got != theme.WindowWidth-theme.TitleBarPadEnd {
		t.Errorf("toggle right edge %v, want %d", got, theme.WindowWidth-theme.TitleBarPadEnd)
	}
}

func TestManyTabsShrinkThenClip(t *testing.T) {
	var tabs []Tab
	for n := range 40 {
		tabs = append(tabs, Tab{Address: fmt.Sprintf("t%d", n), Title: fmt.Sprintf("terminal-number-%d", n), Plain: true, Selected: n == 0})
	}
	b := newBar(t, tabs, true)
	row := b.rect(t, LabelTabs)
	last, ok := b.tt.Find("terminal-number-39")
	if ok && last.X+last.W > row.X+row.W {
		t.Errorf("the last tab %+v sticks out of the row %+v", last, row)
	}
	toggle := b.rect(t, LabelFilesToggle)
	if toggle.X+toggle.W != 1160-theme.TitleBarPadEnd || toggle.W != theme.SizeIconButton {
		t.Errorf("toggle %+v, want %d wide at %d px from the right edge", toggle, theme.SizeIconButton, theme.TitleBarPadEnd)
	}
}

func TestATabTitleLongerThanTheMaxWidthIsCut(t *testing.T) {
	long := "a-very-long-terminal-title-that-goes-past-the-maximum-width-of-a-tab"
	b := newBar(t, []Tab{{Address: "t", Title: long, Plain: true, Selected: true}}, true)
	if w := b.rect(t, long).W; w > theme.SizeTabMaxWidth {
		t.Errorf("tab is %v wide, want at most %d", w, theme.SizeTabMaxWidth)
	}
}

// ---- The bar follows the agent state (through the frame) ----

// agentFrame is a frame whose agents come from snap, with a selection.
func agentFrame(t *testing.T, snap *agentstate.Snapshot) (*Frame, *ui.Tester) {
	t.Helper()
	f, tt := newTester(t)
	f.Selection = state.NewSelection(nil)
	f.agentSnap = func() agentstate.Snapshot { return *snap }
	return f, tt
}

// tabSnap is project p with the orchestrator lead and the workers b1 (working) and b2 (idle),
// and project q with the worker b3.
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

func tabTexts(tt *ui.Tester) []string {
	var out []string
	for _, s := range tt.Texts() {
		if slices.Contains([]string{"lead", "b1", "b2", "b3", "$ repo", "$ other"}, s) {
			out = append(out, s)
		}
	}
	return slices.Compact(out) // the title is both the label and the text
}

func TestTheBarShowsTheTabsOfTheStartAgent(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{termW("t1", "term-1", "p", "/work/repo"), termW("tq", "term-q", "q", "/work/other")}
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"lead", "$ repo"}) {
		t.Errorf("tabs %q, want the orchestrator and the plain terminal of p", got)
	}
}

func TestTabsFollowTheSelectedAgent(t *testing.T) {
	snap := tabSnap()
	snap.Workers[2] = workW("w3", "b3", "p")
	snap.Terminals = []agents.Worker{termW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"b1", "$ repo"}) {
		t.Fatalf("tabs %q, want b1 and the terminal", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w3"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"b3", "$ repo"}) {
		t.Fatalf("tabs %q, want b3 and the terminal", got)
	}
	// A plain terminal keeps the agent whose tabs show.
	f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "t1"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"b3", "$ repo"}) {
		t.Errorf("tabs %q after selecting the terminal, want b3 kept", got)
	}
	// An agent of another project shows its own project's terminals.
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w4"})
	snap.Workers = append(snap.Workers, workW("w4", "b4", "q"))
	tt.Frame()
	if slices.Contains(tabTexts(tt), "$ repo") {
		t.Errorf("a plain terminal of project p shows for project q: %q", tt.Texts())
	}
}

func TestClickingATabInTheFrameSelectsIt(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{termW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	tt.Frame()
	if err := tt.Click("$ repo"); err != nil {
		t.Fatal(err)
	}
	if sel, ok := f.Selection.Current(); !ok || sel != (state.Selected{Kind: state.KindTerminal, ID: "t1"}) {
		t.Errorf("selection %+v %v", sel, ok)
	}
	tt.Frame()
	if err := tt.Click("lead"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindOrchestrator, ID: "o"}) {
		t.Errorf("selection %+v, want the orchestrator", sel)
	}
}

func TestAWorkerGoneFromTheSnapshotLeavesTheBar(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if !slices.Contains(tabTexts(tt), "b1") {
		t.Fatalf("tabs %q", tabTexts(tt))
	}
	snap.Workers = snap.Workers[1:]
	tt.Frame()
	if slices.Contains(tabTexts(tt), "b1") {
		t.Errorf("tabs %q still show the gone worker", tabTexts(tt))
	}
}

func TestAStatusChangeInTheSnapshotRedrawsTheGlyph(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if absent(tt, "working") {
		t.Fatal("a working worker shows no working glyph")
	}
	snap.Workers = append([]agents.Worker(nil), snap.Workers...)
	snap.Workers[0].Status = "blocked"
	tt.Frame()
	if absent(tt, "needs you") || !absent(tt, "working") {
		t.Errorf("the tab should show needs you; texts %q", tt.Texts())
	}
}

func TestNoAgentSourceShowsNoTabs(t *testing.T) {
	_, tt := newTester(t)
	if !absent(tt, LabelTabs) && box(t, tt, LabelTabs).W != 0 {
		t.Error("the tabs row shows without agents")
	}
	if absent(tt, LabelFilesToggle) {
		t.Error("the files toggle should stay")
	}
}

func TestArrowKeysChooseTheNextTabAndSelectIt(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{termW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	tt.Frame()
	if err := tt.Click("lead"); err != nil { // focus the tab list
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "t1"}) {
		t.Errorf("selection %+v, want the plain terminal", sel)
	}
}

func TestSelectingATabClosesTheReviewView(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{termW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Review, _ = newTestReview()
	f.ToggleReview()
	tt.Frame()
	if err := tt.Click("$ repo"); err != nil {
		t.Fatal(err)
	}
	if f.ReviewOpen() {
		t.Error("the Review view stays open after a tab click")
	}
}

// ---- Close tab and the diff summary through the frame ----

func TestTheCloseButtonStopsAPlainTerminalAndNeverAnAgent(t *testing.T) {
	snap := termSnap()
	cf := newCloseFrame(t, &snap)
	// The agent's own tab is selected: no close button, so no way to stop it from the bar.
	cf.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	cf.tt.Frame()
	if !absent(cf.tt, LabelCloseTab) {
		t.Fatal("an agent tab has a close button")
	}
	cf.closeTab(Tab{Address: "o", Orchestrator: true}) // even a direct call stops nothing
	if len(cf.stopped) != 0 {
		t.Errorf("stopped %v", cf.stopped)
	}
	// The plain terminal's tab is selected: the button stops it.
	cf.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	cf.tt.Frame()
	if err := cf.tt.Click(LabelCloseTab); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cf.stopped, []string{"tp"}) {
		t.Errorf("stopped %v, want [tp]", cf.stopped)
	}
}

func TestTheDiffSummaryFollowsTheSelectedAgentAndReadsOnlyThroughTheTree(t *testing.T) {
	repo := reviewScratch(t)
	repo.git("checkout", "-b", "feature")
	write(t, repo.dir, "new.go", "a\nb\nc\n") // untracked: +3
	snap := tabSnap()
	snap.Workers[0].Worktree = repo.dir
	f, tt := agentFrame(t, &snap)
	f.panelOpen = false // the files panel is hidden: the summary still reads
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	tt.Frame()
	if got := f.Files.Shown(); got != repo.dir {
		t.Fatalf("the tree reads %q, want the selected worker's worktree", got)
	}
	if err := tt.Click("3 lines added, 1 file. Open review"); err != nil {
		t.Fatalf("no diff summary; texts %q: %v", tt.Texts(), err)
	}
	sum, ok := f.Diff.Summary()
	if want := (review.Summary{Added: 3, Files: 1}); !ok || sum != want {
		t.Errorf("summary = %+v, %v; want %+v", sum, ok, want)
	}
	f.Files.Close()
}

func TestTheDiffSummaryClickOpensReviewInTheFrame(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Review, _ = newTestReview()
	f.Diff = readyDiff(review.Summary{Added: 1, Files: 1})
	tt.Frame()
	if err := tt.Click("1 line added, 1 file. Open review"); err != nil {
		t.Fatal(err)
	}
	if !f.ReviewOpen() {
		t.Error("the click did not open Review")
	}
	tt.Frame()
	if err := tt.Click("1 line added, 1 file. Open review"); err != nil || !f.ReviewOpen() {
		t.Errorf("a second click closed Review (err %v)", err)
	}
}

func TestSearchButtonSitsBeforeHideFilesAndOpensTheSearch(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	search, files := b.rect(t, LabelSearch), b.rect(t, LabelFilesToggle)
	if search.X+search.W > files.X || search.Y != files.Y {
		t.Errorf("Search at %+v, Hide files at %+v: want Search left of it on the same row", search, files)
	}
	if diff := files.X - (search.X + search.W); diff != theme.Space8 {
		t.Errorf("gap between Search and Hide files %v, want %v", diff, theme.Space8)
	}
	b.click(t, LabelSearch)
	if b.searches != 1 {
		t.Errorf("searches = %d, want 1", b.searches)
	}
}

func TestNoSearchButtonWithoutASearchHandler(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.bar.search = nil
	b.tt.Frame()
	if b.has(LabelSearch) {
		t.Error("the bar has a Search button with no handler")
	}
}

// The selected tab is a pill: the `selected` fill over the glass, and no fill on a tab that is not
// selected (docs/spec/Tab.md, #437).
func TestTheSelectedTabIsAPillInBothThemes(t *testing.T) {
	for _, tc := range themeCases {
		themetest.Use(t, tc.src)
		b := newBar(t, tabsOf(), true)
		b.tt.Frame()
		img := b.tt.Image()
		sel, other := b.rect(t, "lead"), b.rect(t, "go test ./...")
		// A point at the right edge inside the tab, clear of the glyph and the title.
		at := func(r ui.Rect) ui.Color { return rgb(img, r.X+r.W-3, r.Y+r.H/2) }
		ground := tc.pal.Ground
		if want := theme.Over(tc.pal.Selected, ground); !near(at(sel), want, 14) {
			t.Errorf("%s: selected tab %v, want the selected fill %v", tc.name, at(sel), want)
		}
		if !near(at(other), ground, 3) {
			t.Errorf("%s: tab that is not selected %v, want the bare ground %v", tc.name, at(other), ground)
		}
	}
}
