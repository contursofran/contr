// Ported from the Rust contr module title_bar.

package app

// The title bar: one tab for each hc terminal of the open project (#138, D50), the Review
// button and the panel toggle. A click on a tab selects its agent. Its methods run on the UI
// thread.

import (
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the title bar parts. A worker tab's label is "<label>, <status>"; an orchestrator
// tab's is "orchestrator <name>, <status>"; the badge's is "needs <N>".
const (
	LabelTabs      = "tabs"
	LabelTabActive = "active tab underline"
	LabelTitleDrag = "title drag"
	// LabelReviewToggle is the title bar's Review button.
	LabelReviewToggle = "Review"
	// LabelNewTab is the "+" button after the tabs: it opens a plain terminal (D82).
	LabelNewTab = "New terminal"
	// orchestratorMark comes before an orchestrator tab's name.
	orchestratorMark = "◆ "
)

// LabelNeedsBadge is the accessible label of the needs badge on an orchestrator tab.
func LabelNeedsBadge(n int) string { return "needs " + strconv.Itoa(n) }

// titleBar holds the tabs the bar shows and where its clicks go.
type titleBar struct {
	tabs []AgentTab
	// selectTab takes the tab that a click picked. It may be nil: the click is dropped.
	selectTab func(AgentTab)
	// toggleReview shows or hides the Review view. Nil: the bar has no Review button.
	toggleReview func()
	reviewOpen   bool
	// collapsed shows only the icon and the id of each tab (D59): the PR pane takes the room.
	collapsed bool
	// newTab opens a plain terminal. Nil: the bar has no "+" button.
	newTab func()
	// choice is the index the tab list edits; model is the selected tab's index at the last build.
	choice, model int
}

// setTabs takes in the tabs and tells whether they differ from the last ones.
func (b *titleBar) setTabs(tabs []AgentTab) bool {
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

// build adds the bar's parts to the title bar row: the tabs, the drag stretch, the Review
// button and the panel toggle at the right edge.
func (b *titleBar) build(c *ui.Context, panelOpen bool, togglePanel func()) {
	b.tabRow(c)
	if b.newTab != nil {
		b.newTabButton(c)
	}
	ui.Box(c).Label(LabelTitleDrag).Grow(1).MinWidth(0).FillHeight().DragWindow()
	if b.toggleReview != nil {
		b.reviewButton(c)
	}
	panelToggle(c, panelOpen, togglePanel)
}

// newTabButton is the "+" after the tabs. It stays visible with no tabs.
func (b *titleBar) newTabButton(c *ui.Context) {
	el := ui.ButtonBase(c).Label(LabelNewTab).FillHeight().PaddingX(theme.TabPaddingX).Shrink(0).
		Cursor(ui.CursorPointer).Center().OnClick(b.newTab)
	color := uint32(theme.TextDim)
	if el.Hovered() {
		color = theme.Text
	}
	el.TextColor(theme.Color(color)).Children(func() { ui.Text(c, "+") })
}

// reviewButton is the "Review" toggle: it swaps the terminal area for the Review view. It is
// white while the view shows (D37).
func (b *titleBar) reviewButton(c *ui.Context) {
	el := ui.ButtonBase(c).Label(LabelReviewToggle).FillHeight().PaddingX(theme.TabPaddingX).Shrink(0).
		Cursor(ui.CursorPointer).Center().OnClick(b.toggleReview)
	color := uint32(theme.TextDim)
	if b.reviewOpen || el.Hovered() {
		color = theme.Text
	}
	el.TextColor(theme.Color(color)).Children(func() { ui.Text(c, "Review") })
}

// tabRow shows the tabs of the open project in model order. When they do not fit, each tab
// shrinks to TabMinWidth and its label ends in an ellipsis; the row clips what is left
// (Rust D62).
func (b *titleBar) tabRow(c *ui.Context) {
	b.syncChoice()
	tabs := ui.TabsBase(c.Key("agent tabs"), &b.choice, len(b.tabs))
	tabs.List.Label(LabelTabs).FillHeight().MinWidth(0).Shrink(1).Clip().Children(func() {
		for i, t := range b.tabs {
			b.tab(c, tabs.Tab(i), t)
		}
	})
}

// tab shows the status icon, the label and, on the first orchestrator, the needs badge. The
// selected tab has the terminal's background and a white underline. An orchestrator tab has
// the terminal's background too, with its mark before the name.
func (b *titleBar) tab(c *ui.Context, tab ui.Element, t AgentTab) {
	name := t.Label
	label := name + ", " + t.Icon.Label()
	if t.Orchestrator {
		name = orchestratorMark + name
		label = "orchestrator " + t.Label + ", " + t.Icon.Label()
	}
	if b.collapsed {
		name = t.ID
		if t.Orchestrator {
			name = orchestratorMark + t.ID
		}
	}
	minW := float32(theme.TabMinWidth)
	if b.collapsed {
		minW = 0
	}
	el := tab.Label(label).FillHeight().
		MinWidth(minW).MaxWidth(theme.TabMaxWidth).Shrink(1).
		PaddingX(theme.TabPaddingX).Gap(theme.TabGap).AlignItems(ui.Center)
	color := uint32(theme.Text2)
	if t.Selected || t.Orchestrator {
		el = el.Background(theme.Color(theme.BgTerminal))
	}
	if t.Selected {
		color = theme.Text
	} else {
		el = el.Cursor(ui.CursorPointer)
		if el.Hovered() {
			color = theme.Text
		}
	}
	el.TextColor(theme.Color(color)).Children(func() {
		ui.Box(c).Shrink(0).Children(func() { icons.Sized(c, t.Icon, theme.IconSizeSmall) })
		ui.Text(c, name).SingleLine().MinWidth(0).Shrink(1)
		if t.Needs > 0 {
			// White: the badge is what needs the owner (DESIGN rule).
			ui.Box(c).Label(LabelNeedsBadge(t.Needs)).Shrink(0).PaddingX(theme.BadgePadX).
				Radius(theme.BadgeRadius).Background(theme.Color(theme.Text)).
				TextColor(theme.Color(theme.BgTerminal)).Children(func() {
				ui.Text(c, strconv.Itoa(t.Needs))
			})
		}
		if t.Selected {
			ui.Box(c).Label(LabelTabActive).Absolute().Bottom(0).Left(0).Right(0).
				Height(theme.TabUnderline).Background(theme.Color(theme.Text))
		}
	})
}
