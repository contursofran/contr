// Ported from the Rust contr modules board_panel (ActionClick, clear_done,
// text_line) and board_card (selected_card).

package app

// The small parts that the work and needs-you sections share, and that the
// board panel reuses.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the parts the sections share.
const (
	LabelClearDone = "clear done"
	LabelClearWork = "work clear done"
	LabelClearNeed = "needs clear done"
)

// ActionClick is the click of a control that sends an action. A nil
// ActionClick means the board takes no actions: the control is dim and
// does nothing.
type ActionClick func()

// ClearDone adds the "clear done" control of a section head. label names it
// for the accessibility tree and tests. It is lit and clickable when onClick
// is set, else dim and inert.
func ClearDone(c *ui.Context, label string, onClick ActionClick) {
	if onClick == nil {
		ui.Text(c, LabelClearDone).Label(label).Shrink(0).TextColor(theme.Color(theme.TextDim2))
		return
	}
	b := ui.ButtonBase(c).Label(label).Shrink(0).Cursor(ui.CursorPointer).OnClick(onClick)
	colour := uint32(theme.Text2)
	if b.Hovered() {
		colour = theme.Text
	}
	b.TextColor(theme.Color(colour)).Children(func() {
		ui.Text(c, LabelClearDone)
	})
}

// TextLine adds one line of text across the panel: BoardLineHeight tall,
// padded, and cut with an ellipsis. label names the line.
func TextLine(c *ui.Context, label, text string, colour uint32) {
	ui.Row(c).Label(label).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.PanelPadX).Children(func() {
		ui.Text(c, text).TextColor(theme.Color(colour)).SingleLine().Ellipsis("…").MinWidth(0)
	})
}

// selected draws el as a picked row: the selected-card background and
// border, as the agents list draws its selected row.
func selected(el ui.Element) ui.Element {
	return el.Background(theme.Color(theme.BgCard)).Border(1, theme.Color(theme.CardBorder)).Radius(theme.CardRadius)
}
