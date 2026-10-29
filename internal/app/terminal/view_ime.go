package terminal

import "github.com/egoist/mygo/ui"

// IME. The grid is a [ui.TextInputClient] with no text of its own: composed
// text is shown at the cursor, and committed text goes to the pane as text.
// Printable keys come only through here, so Option types characters (D11) and
// dead keys work.

// imeClient is the grid's [ui.TextInputClient]. marked is the text the IME is
// composing; the paint shows it at the cursor. view is set by [NewView].
type imeClient struct {
	view   *View
	marked string
}

var _ ui.TextInputClient = (*imeClient)(nil)

// TextForRange: the terminal has no document the IME can read.
func (c *imeClient) TextForRange(ui.TextInputRange) (string, ui.TextInputRange) {
	return "", ui.TextInputRange{}
}

// Selection is a caret at 0.
func (c *imeClient) Selection() ui.TextInputSelection { return ui.TextInputSelection{} }

// MarkedRange covers the composing text, if any.
func (c *imeClient) MarkedRange() (ui.TextInputRange, bool) {
	if c.marked == "" {
		return ui.TextInputRange{}, false
	}
	return ui.TextInputRange{Start: 0, End: ui.UTF16Len(c.marked)}, true
}

// ReplaceText is a commit: it ends the composition and sends the text.
func (c *imeClient) ReplaceText(_ *ui.TextInputRange, text string) {
	c.setMarked("")
	c.view.commit(text)
}

// SetMarkedText sets the composing text. Empty text ends the composition.
func (c *imeClient) SetMarkedText(_ *ui.TextInputRange, text string, _ ui.TextInputRange) {
	c.setMarked(text)
}

// UnmarkText ends a composition by committing it, as the system does when
// focus moves away: the composed text is sent as a commit is.
func (c *imeClient) UnmarkText() {
	if c.marked == "" {
		return
	}
	text := c.marked
	c.setMarked("")
	c.view.commit(text)
}

// BoundsForRange is the cursor cell, relative to the grid. It is false
// without metrics, a surface or a cursor.
func (c *imeClient) BoundsForRange(ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	v := c.view
	if !v.hasMetrics || v.surface == nil || v.surface.Cursor == nil {
		return ui.Rect{}, ui.TextInputRange{}, false
	}
	m := v.metrics
	x, y := cursorCell(*v.surface.Cursor, v.surface.Size)
	r := ui.Rect{
		X: m.Pad + float32(x)*m.CellW, Y: m.Pad + float32(y)*m.CellH,
		W: m.CellW, H: m.CellH,
	}
	return r, ui.TextInputRange{}, true
}

// IndexForPoint: there is no text to point at.
func (c *imeClient) IndexForPoint(ui.Point) (int, bool) { return 0, false }

// setMarked sets the composing text and asks for a frame on a change.
func (c *imeClient) setMarked(text string) {
	if c.marked != text {
		c.marked = text
		c.view.invalidate()
	}
}
