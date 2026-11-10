package titlebar

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
)

// labelTitleBar is the label of the frame's title bar row (app.LabelTitleBar). The tests
// build the bar in a row of their own.
const labelTitleBar = "title bar"

// tabsOf is the tabs of project p: the orchestrator "lead" (selected), a plain terminal with a
// title and a plain terminal with none.
func tabsOf() []tabmodel.Tab {
	return []tabmodel.Tab{
		{Address: "o", Title: "lead", Orchestrator: true, Selected: true},
		{Address: "t1", Title: "go test ./...", Plain: true},
		{Address: "t2", Title: "$ repo", Plain: true},
	}
}

// barTest is a bare title bar in a 1160x40 window.
type barTest struct {
	bar       *TitleBar
	tt        *ui.Tester
	picked    []tabmodel.Tab
	closed    []tabmodel.Tab
	news      int
	searches  int
	panelOpen bool
	toggles   int
}

func newBar(t *testing.T, tabs []tabmodel.Tab, panelOpen bool) *barTest {
	t.Helper()
	b := &barTest{bar: &TitleBar{}, panelOpen: panelOpen}
	b.bar.SelectTab = func(tab tabmodel.Tab) { b.picked = append(b.picked, tab) }
	b.bar.CloseTab = func(tab tabmodel.Tab) { b.closed = append(b.closed, tab) }
	b.bar.NewTab = func() { b.news++ }
	b.bar.Search = func() { b.searches++ }
	b.bar.SetTabs(tabs)
	b.tt = ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Label(labelTitleBar).Fill().AlignItems(ui.Center).Background(theme.Current().Ground).
			PaddingEnd(theme.TitleBarPadEnd).Children(func() {
			b.bar.Build(c, b.panelOpen, false, func() { b.toggles++ })
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
	return apptest.Box(t, b.tt, label)
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
	tabs := []tabmodel.Tab{{Address: "w", Title: "#5 · fix-x", State: tabmodel.TabState(agents.Worker{Status: "blocked", Live: true}), Selected: true}}
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
	none.bar.NewTab = nil
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
	var bar TitleBar
	if !bar.SetTabs(tabsOf()) {
		t.Error("new tabs did not report a change")
	}
	if bar.SetTabs(tabsOf()) {
		t.Error("equal tabs reported a change")
	}
	changed := tabsOf()
	changed[1].Title = "vim"
	if !bar.SetTabs(changed) {
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

func TestManyTabsShrinkThenClip(t *testing.T) {
	var tabs []tabmodel.Tab
	for n := range 40 {
		tabs = append(tabs, tabmodel.Tab{Address: fmt.Sprintf("t%d", n), Title: fmt.Sprintf("terminal-number-%d", n), Plain: true, Selected: n == 0})
	}
	b := newBar(t, tabs, true)
	row := b.rect(t, LabelTabs)
	last, ok := b.tt.Find("terminal-number-39")
	if ok && last.X+last.W > row.X+row.W {
		t.Errorf("the last tab %+v sticks out of the row %+v", last, row)
	}
	toggle := b.rect(t, LabelFilesToggle)
	// The layout sums floats, so allow a rounding error.
	if math.Abs(float64(toggle.X+toggle.W-(1160-theme.TitleBarPadEnd))) > 0.01 || toggle.W != theme.SizeIconButton {
		t.Errorf("toggle %+v, want %d wide at %d px from the right edge", toggle, theme.SizeIconButton, theme.TitleBarPadEnd)
	}
}

func TestATabTitleLongerThanTheMaxWidthIsCut(t *testing.T) {
	long := "a-very-long-terminal-title-that-goes-past-the-maximum-width-of-a-tab"
	b := newBar(t, []tabmodel.Tab{{Address: "t", Title: long, Plain: true, Selected: true}}, true)
	if w := b.rect(t, long).W; w > theme.SizeTabMaxWidth {
		t.Errorf("tab is %v wide, want at most %d", w, theme.SizeTabMaxWidth)
	}
}

// stripOf copies the pixels of the bar between x0 and x1 on the tab's middle rows.
func stripOf(b *barTest, x0, x1 float32) []ui.Color {
	img := b.tt.Image()
	var out []ui.Color
	for y := 8; y < 32; y++ {
		for x := int(x0); x < int(x1); x++ {
			out = append(out, apptest.RGB(img, float32(x), float32(y)))
		}
	}
	return out
}

// A long title ends in an ellipsis inside the tab, and the close button stays in view (#448, D175).
// The shown title and the close button look as in a tab with the literal text "start…".
func TestALongTitleEndsInAnEllipsisInsideTheTab(t *testing.T) {
	long := "Herdr worker orchestrator with a very long title"
	b := newBar(t, []tabmodel.Tab{{Address: "t", Title: long, Plain: true, Selected: true}}, true)
	b.tt.Frame()
	tab := b.rect(t, long)
	closeBox := b.rect(t, LabelCloseTab)
	if closeBox.X+closeBox.W > tab.X+tab.W {
		t.Errorf("close button %+v sticks out of the tab %+v", closeBox, tab)
	}
	if tab.W > theme.SizeTabMaxWidth {
		t.Errorf("tab is %v wide, want at most %d", tab.W, theme.SizeTabMaxWidth)
	}
	// Overflow check: the gap before the close button is bare.
	limit := closeBox.X - theme.Space8
	gap := stripOf(b, limit, closeBox.X)
	for _, c := range gap {
		if c != gap[0] {
			t.Fatal("the title runs into the gap before the close button")
		}
	}
	// The cut ends in an ellipsis: the ink in the last 10 px of the title is dots on the baseline,
	// none above the middle of the line.
	img := b.tt.Image()
	fill := apptest.RGB(img, limit, 6)
	dots := 0
	for y := 8; y < 32; y++ {
		for x := limit - 10; x < limit; x++ {
			if c := apptest.RGB(img, x, float32(y)); !apptest.Near(c, fill, 24) {
				dots++
				if y < 19 {
					t.Fatalf("ink at row %d near the end of the title: a letter was cut, not an ellipsis", y)
				}
			}
		}
	}
	if dots == 0 {
		t.Error("no ellipsis dots at the end of the title")
	}
}

func TestATabTitleThatFitsHasNoEllipsis(t *testing.T) {
	b := newBar(t, []tabmodel.Tab{{Address: "t", Title: "zsh", Plain: true, Selected: true}}, true)
	for _, s := range b.tt.Texts() {
		if strings.HasSuffix(s, "…") {
			t.Errorf("text %q has an ellipsis", s)
		}
	}
}

func TestTheTabTooltipHoldsTheFullTitle(t *testing.T) {
	long := "Herdr worker orchestrator with a very long title"
	count := func(b *barTest) (n int) {
		for _, s := range b.tt.Texts() {
			if strings.Contains(s, long) {
				n++
			}
		}
		return n
	}
	for name, tip := range map[string]string{"no tip": "", "tip with a name": "w"} {
		b := newBar(t, []tabmodel.Tab{{Address: "t", Title: long, Tip: tip, Plain: true, Selected: true}}, true)
		before := count(b)
		r := b.rect(t, long)
		b.tt.Move(r.X+r.W/2, r.Y+r.H/2)
		b.tt.Advance(time.Second)
		if count(b) != before+1 {
			t.Errorf("%s: no tooltip with the full title; texts %q", name, b.tt.Texts())
		}
	}
	if got := tabTooltip(tabmodel.Tab{Title: "a", Tip: "w"}); got != "a · w" {
		t.Errorf("tooltip %q, want %q", got, "a · w")
	}
	if got := tabTooltip(tabmodel.Tab{Title: "a", Tip: "a · t1"}); got != "a · t1" {
		t.Errorf("tooltip %q, want the tip as given", got)
	}
}

// ---- The bar follows the agent state (through the frame) ----

// ---- Close tab and the diff summary through the frame ----

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
	b.bar.Search = nil
	b.tt.Frame()
	if b.has(LabelSearch) {
		t.Error("the bar has a Search button with no handler")
	}
}

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

func TestTabMenuListsCloseAndCloseOthers(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.bar.CloseOthers = func(tabmodel.Tab) {}
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
	b.bar.CloseOthers = func(k tabmodel.Tab) { kept = append(kept, k) }
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
	b.bar.CloseOthers = func(k tabmodel.Tab) { kept = append(kept, k) }
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
	b.bar.CloseOthers = func(tabmodel.Tab) { called++ }
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
	b.bar.MoveTab = func(from, to string) { moves = append(moves, move{from, to}) }
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
	b.bar.MoveTab = func(from, to string) { moves = append(moves, move{from, to}) }
	b.tt.Frame()
	dragTab(t, b, "lead", "go test ./...")
	dragTab(t, b, "go test ./...", "lead")
	if len(moves) != 0 {
		t.Errorf("moves %v, want none", moves)
	}
}

func TestTabMenuStopsAnAgentTab(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var stopped []tabmodel.Tab
	b.bar.StopTab = func(t tabmodel.Tab) { stopped = append(stopped, t) }
	b.tt.Frame()
	if err := b.tt.RightClick("lead"); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(tabmodel.MenuStop); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0].Address != "o" {
		t.Errorf("stopped %+v, want o", stopped)
	}
	if len(b.closed) != 0 {
		t.Errorf("closed %+v", b.closed)
	}
}

func TestTabMenuOnAPlainTabHasNoStop(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.bar.StopTab = func(tabmodel.Tab) { t.Error("Stop ran on a plain tab") }
	b.tt.Frame()
	if err := b.tt.RightClick("go test ./..."); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(b.tt.Menu(), tabmodel.MenuStop) {
		t.Errorf("menu %q has Stop", b.tt.Menu())
	}
}

func TestTabMenuClosesAnAgentTab(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var gone []tabmodel.Tab
	b.bar.CloseAgent = func(t tabmodel.Tab) { gone = append(gone, t) }
	b.tt.Frame()
	b.tt.RightClick("lead")
	if err := b.tt.ChooseMenuItem(MenuClose); err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0].Address != "o" || len(b.closed) != 0 {
		t.Errorf("closed agent %+v, plain %+v", gone, b.closed)
	}
}

// The selected tab has the panel fill, as the owner tuned it with the Settings sliders (#448, D175).
func TestTheSelectedTabHasThePanelFillAndFollowsTheSliders(t *testing.T) {
	for _, tc := range apptest.ThemeCases {
		themetest.Use(t, tc.Src)
		for _, a := range []float64{-1, 0.3} { // -1: no override
			theme.ClearTints()
			if a >= 0 {
				theme.SetTint(tc.Src == mygo.ThemeDark, nil, &a)
			}
			t.Cleanup(theme.ClearTints)
			b := newBar(t, tabsOf(), true)
			b.tt.Frame()
			sel := b.rect(t, "lead")
			got := apptest.RGB(b.tt.Image(), sel.X+sel.W-3, sel.Y+sel.H/2)
			want := theme.Over(theme.Current().Stage, tc.Pal.Ground)
			if !apptest.Near(got, want, 10) {
				t.Errorf("%s panel alpha %v: selected tab %v, want the stage fill %v", tc.Name, a, got, want)
			}
		}
	}
}

// The selected tab is a pill: the `selected` fill over the glass, and no fill on a tab that is not
// selected (docs/spec/Tab.md, #437).
func TestTheSelectedTabIsAPillInBothThemes(t *testing.T) {
	for _, tc := range apptest.ThemeCases {
		themetest.Use(t, tc.Src)
		b := newBar(t, tabsOf(), true)
		b.tt.Frame()
		img := b.tt.Image()
		sel, other := b.rect(t, "lead"), b.rect(t, "go test ./...")
		// A point at the right edge inside the tab, clear of the glyph and the title.
		at := func(r ui.Rect) ui.Color { return apptest.RGB(img, r.X+r.W-3, r.Y+r.H/2) }
		ground := tc.Pal.Ground
		if want := theme.Over(theme.Current().Stage, ground); !apptest.Near(at(sel), want, 14) {
			t.Errorf("%s: selected tab %v, want the stage fill %v", tc.Name, at(sel), want)
		}
		if !apptest.Near(at(other), ground, 3) {
			t.Errorf("%s: tab that is not selected %v, want the bare ground %v", tc.Name, at(other), ground)
		}
	}
}

// The selected tab has no ring (D249): its edge has the same color as its fill.
func TestTheSelectedTabHasNoRing(t *testing.T) {
	for _, tc := range apptest.ThemeCases {
		themetest.Use(t, tc.Src)
		b := newBar(t, tabsOf(), true)
		b.tt.Frame()
		img := b.tt.Image()
		sel := b.rect(t, "lead")
		fill := apptest.RGB(img, sel.X+sel.W-3, sel.Y+sel.H/2)
		if edge := apptest.RGB(img, sel.X+sel.W/2, sel.Y); !apptest.Near(edge, fill, 2) {
			t.Errorf("%s: selected tab edge %v, fill %v: want no ring", tc.Name, edge, fill)
		}
	}
}

// The tab row has 8pt above and 8pt below it in the title bar (D177): the panels start
// where the title bar ends.
func TestTabRowHasEqualSpaceAboveAndBelow(t *testing.T) {
	bar := &TitleBar{SelectTab: func(tabmodel.Tab) {}, CloseTab: func(tabmodel.Tab) {}, NewTab: func() {}, Search: func() {}}
	bar.SetTabs(tabsOf())
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Label(labelTitleBar).FillWidth().Height(theme.TitleBarHeight).AlignItems(ui.Center).Children(func() {
			bar.Build(c, true, false, func() {})
		})
	}, 1160, 100)
	tab, ok := tt.Find("lead")
	if !ok {
		t.Fatalf("no lead tab; texts %q", tt.Texts())
	}
	if tab.Y != theme.PanelGap || theme.TitleBarHeight-(tab.Y+tab.H) != theme.PanelGap || tab.H != theme.SizeRowHeight {
		t.Errorf("tab %+v in a %v bar, want %v above and below", tab, theme.TitleBarHeight, theme.PanelGap)
	}
}

// TestEachChipSitsOnItsIconInsideItsElement is #645: a tab's chip on its glyph, each button's
// chip on its icon, all inside their element and the window, also Hide files at the right edge.
func TestEachChipSitsOnItsIconInsideItsElement(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.bar.Hints = true
	b.tt.Frame()
	for _, c := range []struct{ chip, in string }{
		{"1", "lead"}, {"2", "go test ./..."}, {"3", "$ repo"},
		{"T", LabelNewTab}, {"F", LabelSearch}, {"⇧E", LabelFilesToggle},
	} {
		chip := apptest.ChipOn(t, b.tt, kit.KeyHintLabel(c.chip), c.in, 1160, 40)
		if c.chip == "1" {
			apptest.Centered(t, "chip 1", chip, "the glyph", b.rect(t, "idle"))
		}
		if c.chip != "1" && c.chip != "2" && c.chip != "3" {
			apptest.Centered(t, "chip "+c.chip, chip, c.in, b.rect(t, c.in))
		}
	}
}

// The orchestrate pill sits right of the update pill and left of Search, after the drag stretch;
// it gets Narrow as compact (D253).
func TestOrchestratePillSitsLeftOfSearchAndGetsNarrow(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var compact []bool
	b.bar.Update = func(c *ui.Context) { ui.Box(c).Label("update").Size(40, 28).Shrink(0) }
	b.bar.Orchestrate = func(c *ui.Context, narrow bool) {
		compact = append(compact, narrow)
		ui.Box(c).Label("pill").Size(60, 28).Shrink(0).MarginEnd(theme.Space8)
	}
	b.tt.Frame()
	drag, upd, pill, search := b.rect(t, LabelTitleDrag), b.rect(t, "update"), b.rect(t, "pill"), b.rect(t, LabelSearch)
	if drag.X+drag.W > upd.X || upd.X+upd.W > pill.X || pill.X+pill.W+theme.Space8 != search.X {
		t.Errorf("drag %+v, update %+v, pill %+v, Search %+v: want them in this order, the pill space-8 before Search",
			drag, upd, pill, search)
	}
	b.bar.Narrow = true
	b.tt.Frame()
	if len(compact) < 2 || compact[0] || !compact[len(compact)-1] {
		t.Errorf("compact %v: want false, then true after Narrow", compact)
	}
}

// A tab that is selected fades its fill and ink in MotionSelect, then asks for no frame (D256).
func TestSelectingATabFadesThenRests(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.tt.Frame()
	b.tt.Advance(time.Second)
	if b.tt.FramePending() {
		t.Fatal("a frame is asked at rest")
	}
	tabs := tabsOf()
	tabs[0].Selected, tabs[1].Selected = false, true
	b.bar.SetTabs(tabs)
	b.tt.Frame()
	b.tt.Advance(theme.MotionSelect / 2)
	if !b.tt.FramePending() {
		t.Error("no frame asked half way: the selection does not fade")
	}
	b.tt.Advance(theme.MotionSelect/2 + 20*time.Millisecond)
	if b.tt.FramePending() {
		t.Errorf("a frame is asked after %v; want none at rest", theme.MotionSelect)
	}
	if !b.has(LabelCloseTab) {
		t.Error("the selected plain tab has no close button at once")
	}
}
