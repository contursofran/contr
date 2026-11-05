package app

// The title bar (docs/spec/TitleBar.md): the tabs of the selected agent, the New tab button, the
// diff summary and the Hide files button. The sidebar toggle and the room for the window buttons
// are drawn by the frame. The methods run on the UI thread.

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the title bar parts. A tab's label is its title.
const (
	LabelTabs      = "tabs"
	LabelTitleDrag = "title drag"
	// LabelNewTab is the "+" button after the tabs: it opens a plain terminal (D82).
	LabelNewTab = "New tab"
	// LabelCloseTab is the close button of the selected plain terminal tab.
	LabelCloseTab = "Close tab"
)

// titleBar holds the tabs the bar shows and where its clicks go.
type titleBar struct {
	tabs []Tab
	// selectTab takes the tab that a click picked. It may be nil: the click is dropped.
	selectTab func(Tab)
	// closeTab takes the plain tab whose close button was clicked. Nil: no close buttons.
	closeTab func(Tab)
	// newTab opens a plain terminal. Nil: the bar has no "+" button.
	newTab func()
	// search opens the find bar over the terminal. Nil: the bar has no Search button.
	search func()
	// diff is the diff summary and openReview its click. Nil diff: none.
	diff       *DiffSummary
	openReview func()
	// choice is the index the tab list edits; model is the selected tab's index at the last build.
	choice, model int
}

// setTabs takes in the tabs and tells whether they differ from the last ones.
func (b *titleBar) setTabs(tabs []Tab) bool {
	if slices.Equal(b.tabs, tabs) {
		return false
	}
	b.tabs = tabs
	return true
}

// syncChoice turns a choice made on the tab list (a click, an arrow, Home or End) into a call
// of selectTab, then sets the list to the model's selected tab. The selected tab does nothing.
func (b *titleBar) syncChoice() {
	if b.choice != b.model && b.choice >= 0 && b.choice < len(b.tabs) && b.selectTab != nil &&
		!b.tabs[b.choice].Selected {
		b.selectTab(b.tabs[b.choice])
	}
	b.model = 0
	for i, t := range b.tabs {
		if t.Selected {
			b.model = i
		}
	}
	b.choice = b.model
}

// build adds the bar's parts to the title bar row: the tabs, "+", the drag stretch, the diff
// summary, Search and the files toggle at the right edge. The toggle is pressed while the files show; it
// is disabled while the window is too narrow for them.
func (b *titleBar) build(c *ui.Context, filesOn, filesDisabled bool, toggleFiles func()) {
	b.tabRow(c)
	if b.newTab != nil {
		kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: LabelNewTab, Key: "⌘T", OnClick: b.newTab}).
			MarginStart(theme.Space8)
	}
	ui.Box(c).Label(LabelTitleDrag).Grow(1).MinWidth(0).FillHeight().DragWindow()
	if b.diff != nil {
		ui.Row(c).Shrink(0).MarginEnd(theme.Space8).Children(func() { b.diff.View(c, b.openReview) })
	}
	if b.search != nil {
		kit.IconButton(c, kit.IconButtonOpts{Icon: "search", Label: LabelSearch, Key: "⌘F", OnClick: b.search}).
			MarginEnd(theme.Space8)
	}
	kit.IconButton(c, kit.IconButtonOpts{Icon: "sidebar-right", Label: LabelFilesToggle, Key: "⌘⇧E",
		On: &filesOn, OnClick: toggleFiles, Disabled: filesDisabled})
}

// tabRow shows the tabs in model order, space-2 apart. When they do not fit, each tab shrinks
// toward zero and its title ends in an ellipsis; the row clips what is left.
func (b *titleBar) tabRow(c *ui.Context) {
	b.syncChoice()
	tabs := ui.TabsBase(c.Key("agent tabs"), &b.choice, len(b.tabs))
	tabs.List.Label(LabelTabs).FillHeight().MinWidth(0).Shrink(1).AlignItems(ui.Center).Gap(theme.Space2).Clip().Children(func() {
		for i, t := range b.tabs {
			b.tab(c, tabs.Tab(i), t)
		}
	})
}

// tab draws one tab: the glyph, the title and, on the selected plain terminal, the close button.
func (b *titleBar) tab(c *ui.Context, tab ui.Element, t Tab) {
	p := theme.Current()
	el := tab.Label(t.Title).Height(theme.SizeRowHeight).MaxWidth(theme.SizeTabMaxWidth).MinWidth(0).Shrink(1).
		Padding(0, theme.Space10).Gap(theme.Space8).AlignItems(ui.Center).Radius(theme.Radius8)
	ink := p.Ink50
	switch {
	case t.Selected:
		el = el.Background(p.Selected)
		el = insetRing(el, p.ShadowSelected)
		ink = p.Ink100
	default:
		el = el.Cursor(ui.CursorPointer)
		if el.Hovered() {
			el = el.Background(p.Hover)
		}
	}
	el.TextColor(ink).Children(func() {
		b.glyph(c, t)
		s := theme.StyleRowSmall
		ui.Text(c, t.Title).Font(s.Stack()).FontSize(s.Size).FixedLineHeight(s.Line).FontWeight(s.Weight).
			NoWrap().Ellipsis("…").MinWidth(0).Shrink(1)
		if t.Selected && t.Closable() && b.closeTab != nil {
			closeButton(c, func() { b.closeTab(t) })
		}
	})
}

// glyph is the leading part of a tab: the Claude mark for an orchestrator, the status glyph for
// a worker, the terminal icon for a plain terminal.
func (b *titleBar) glyph(c *ui.Context, t Tab) {
	if t.Plain {
		if s := icons.Line("terminal"); s != nil {
			ui.Icon(c, s).Size(14, 14).Shrink(0)
		}
		return
	}
	kit.StatusGlyph(c, kit.GlyphOpts{State: t.State, Claude: t.Orchestrator, Small: true})
}

// closeButton is the 16px close button of the selected tab.
func closeButton(c *ui.Context, onClick func()) {
	p := theme.Current()
	b := ui.ButtonBase(c).Label(LabelCloseTab).Size(16, 16).Shrink(0).Radius(theme.Radius4).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).OnClick(onClick)
	ink := p.Ink45
	if b.Hovered() || b.Pressed() {
		b = b.Background(p.Press)
		ink = p.Ink100
	}
	b.TextColor(ink).Children(func() {
		if s := icons.Line("close"); s != nil {
			ui.Icon(c, s).Size(10, 10)
		}
	})
}

// insetRing draws the layers of a token shadow that are inset, as MyGo's inset shadow.
func insetRing(el ui.Element, sh theme.Shadow) ui.Element {
	for _, l := range sh {
		if l.Inset {
			el = el.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
	}
	return el
}
