// Ported from the Rust contr module title_bar.

package app

// The title bar: the agent tabs of herdr's current workspace, "+", the split button and the
// panel toggle. The strip follows herdr's tree; a click becomes a [TabCommand] for the frame to
// send. Its methods run on the UI thread.

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
)

// Labels of the title bar parts. A tab's label is "<herdr label>, <status>".
const (
	LabelTabs      = "tabs"
	LabelTabNew    = "New tab"
	LabelTabActive = "active tab underline"
	LabelSplit     = "Split pane"
	LabelTitleDrag = "title drag"
)

// titleBar is the strip the bar shows and where its clicks go.
type titleBar struct {
	strip herdr.TabStrip
	// send takes a herdr command. It may be nil: the command is dropped.
	send func(TabCommand)
}

// setStrip takes in a strip and tells whether it differs from the last one. The frame builds the
// bar again for any herdr change; this tells whether the bar's own content changed.
func (b *titleBar) setStrip(s herdr.TabStrip) bool {
	if b.strip.WorkspaceID == s.WorkspaceID && b.strip.SplitPaneID == s.SplitPaneID &&
		slices.Equal(b.strip.Tabs, s.Tabs) {
		return false
	}
	b.strip = s
	return true
}

func (b *titleBar) emit(cmd TabCommand) {
	if b.send != nil {
		b.send(cmd)
	}
}

// clickTab: an inactive tab asks herdr to focus it, and the active tab does nothing.
func (b *titleBar) clickTab(i int) {
	if i < 0 || i >= len(b.strip.Tabs) {
		return
	}
	if tab := b.strip.Tabs[i]; !tab.Active {
		b.emit(TabCommand{TabFocus, tab.ID})
	}
}

// clickNew asks for a new focused tab in the strip's workspace.
func (b *titleBar) clickNew() {
	if b.strip.WorkspaceID != "" {
		b.emit(TabCommand{TabCreate, b.strip.WorkspaceID})
	}
}

// clickSplit splits the strip's pane to the right.
func (b *titleBar) clickSplit() {
	if b.strip.SplitPaneID != "" {
		b.emit(TabCommand{TabSplitRight, b.strip.SplitPaneID})
	}
}

// build adds the bar's parts to the title bar row: the tabs, "+", the drag stretch, then the
// split button (when there is a pane to split) and the panel toggle at the right edge.
func (b *titleBar) build(c *ui.Context, panelOpen bool, togglePanel func()) {
	b.tabs(c)
	if b.strip.WorkspaceID != "" {
		b.newButton(c)
	}
	ui.Box(c).Label(LabelTitleDrag).Grow(1).MinWidth(0).FillHeight().DragWindow()
	if b.strip.SplitPaneID != "" {
		b.splitButton(c)
	}
	panelToggle(c, panelOpen, togglePanel)
}

// tabs shows the tabs of the current workspace, in herdr's order. When they do not fit, each tab
// shrinks to TabMinWidth and its label ends in an ellipsis; the row clips what is left
// (Rust D62).
func (b *titleBar) tabs(c *ui.Context) {
	ui.Row(c).Label(LabelTabs).FillHeight().MinWidth(0).Shrink(1).Clip().Children(func() {
		for i, item := range b.strip.Tabs {
			b.tab(c, i, item)
		}
	})
}

// tab shows the status icon, then herdr's label. The active tab has the terminal's background
// and a white underline. Clicking an inactive tab asks herdr to focus it.
func (b *titleBar) tab(c *ui.Context, i int, item herdr.TabItem) {
	status := icons.FromAgentState(item.Status)
	el := ui.ButtonBase(c).Label(item.Label + ", " + status.Label()).FillHeight().
		MinWidth(theme.TabMinWidth).MaxWidth(theme.TabMaxWidth).Shrink(1).
		PaddingX(theme.TabPaddingX).Gap(theme.TabGap).AlignItems(ui.Center)
	color := uint32(theme.Text2)
	if item.Active {
		color = theme.Text
		el = el.Background(theme.Color(theme.BgTerminal))
	} else {
		el = el.Cursor(ui.CursorPointer).OnClick(func() { b.clickTab(i) })
		if el.Hovered() {
			color = theme.Text
		}
	}
	el.TextColor(theme.Color(color)).Children(func() {
		ui.Box(c).Shrink(0).Children(func() { icons.Sized(c, status, theme.IconSizeSmall) })
		ui.Text(c, item.Label).SingleLine().MinWidth(0).Shrink(1)
		if item.Active {
			ui.Box(c).Label(LabelTabActive).Absolute().Bottom(0).Left(0).Right(0).
				Height(theme.TabUnderline).Background(theme.Color(theme.Text))
		}
	})
}

// newButton is "+": a new focused tab in the workspace.
func (b *titleBar) newButton(c *ui.Context) {
	el := ui.ButtonBase(c).Label(LabelTabNew).Width(theme.TitleButtonSize).FillHeight().Shrink(0).
		Cursor(ui.CursorPointer).Center().OnClick(b.clickNew)
	color := uint32(theme.TextDim)
	if el.Hovered() {
		color = theme.Text
	}
	el.TextColor(theme.Color(color)).Children(func() { ui.Text(c, "+") })
}

// splitButton is an outline box with a vertical line in the middle. It splits the strip's pane
// to the right (Rust D20).
func (b *titleBar) splitButton(c *ui.Context) {
	el := ui.ButtonBase(c).Label(LabelSplit).Size(theme.TitleButtonSize, theme.TitleButtonSize).Shrink(0).
		Cursor(ui.CursorPointer).OnClick(b.clickSplit)
	color := uint32(theme.TextDim)
	if el.Hovered() {
		color = theme.Text
	}
	el.Children(func() {
		ui.Row(c).Size(theme.TitleGlyphWidth, theme.TitleGlyphHeight).
			Border(1, theme.Color(color)).Radius(theme.SidebarToggleRadius).Center().Children(func() {
			ui.Box(c).Width(1).FillHeight().Background(theme.Color(color))
		})
	})
}
