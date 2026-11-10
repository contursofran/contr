package chatview

// The place of the Chat | Terminal switch (docs/spec/ChatView.md, D253): alone at the top right of
// the terminal panel, a layer of the terminal overlay.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// LabelSwitchLayer names the switch layer of the terminal overlay.
const LabelSwitchLayer = "chat switch"

// Sizes of the switch's place.
const (
	// switchRight is the space between the switch and the right side: the outline inset.
	switchRight = 10
	// switchRightClear keeps the switch clear of the outline column when the column reaches the
	// switch's row: inset 10px, 20px wide, and 10px more.
	switchRightClear = 40
	// switchBottom is the bottom of the switch's row: top space-8 and the switch's 26px.
	switchBottom = theme.Space8 + 26
)

// Corner places the switch at the top right of the terminal panel. Its methods run on the UI
// thread.
type Corner struct {
	// Switch returns the view of the switch, or nil when the selected terminal has none.
	Switch func() func(c *ui.Context)
	// OutlineTop returns the top of the outline column in the terminal, and false while no column
	// shows; may be nil.
	OutlineTop func() (float32, bool)
}

// View builds the switch, absolute at the top right of the terminal, or nothing. The row around
// it lets the pointer through.
func (k *Corner) View(c *ui.Context) {
	if k.Switch == nil {
		return
	}
	sw := k.Switch()
	if sw == nil {
		return
	}
	ui.Row(c).Absolute().Top(theme.Space8).Left(0).Right(k.right()).Justify(ui.End).PassThrough().
		Children(func() { sw(c) })
}

// right is the space between the switch and the right side: the outline inset, or clear of the
// outline column when the column's top is above the bottom of the switch's row (D237, D253).
func (k *Corner) right() float32 {
	if k.OutlineTop == nil {
		return switchRight
	}
	if top, ok := k.OutlineTop(); ok && top < switchBottom {
		return switchRightClear
	}
	return switchRight
}
