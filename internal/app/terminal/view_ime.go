package terminal

import "github.com/egoist/mygo/ui"

// IME. M5.7a holds a client that does nothing; M5.7b fills it.

// imeClient is the grid's [ui.TextInputClient]. marked is the text the IME is
// composing; the paint shows it at the cursor.
type imeClient struct {
	marked string
}

var _ ui.TextInputClient = (*imeClient)(nil)

func (c *imeClient) TextForRange(ui.TextInputRange) (string, ui.TextInputRange) {
	return "", ui.TextInputRange{}
}
func (c *imeClient) Selection() ui.TextInputSelection { return ui.TextInputSelection{} }
func (c *imeClient) MarkedRange() (ui.TextInputRange, bool) {
	return ui.TextInputRange{}, false
}
func (c *imeClient) ReplaceText(*ui.TextInputRange, string)                      {}
func (c *imeClient) SetMarkedText(*ui.TextInputRange, string, ui.TextInputRange) {}
func (c *imeClient) UnmarkText()                                                 {}
func (c *imeClient) BoundsForRange(ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	return ui.Rect{}, ui.TextInputRange{}, false
}
func (c *imeClient) IndexForPoint(ui.Point) (int, bool) { return 0, false }
