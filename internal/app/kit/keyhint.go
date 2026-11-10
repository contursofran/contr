package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// LabelKeyHint names a key chip for tests and the e2e driver. Screen readers do not see the
// chip: the element's label and tooltip name its key.
const LabelKeyHint = "key hint"

// SizeKeyHint is the height of a key chip and its least width: the kbd line and a 1px border
// and padding at each side.
const SizeKeyHint = 18

// KeyHint draws the key chip of a shortcut while ⌘ is held (docs/spec/KeyHints.md): kbd text in
// ink-100 on glass-float with a float-line border. Build it as the last child of the element it
// names. It is out of the layout, so nothing moves: its self point sits on the at point of the
// parent's box (Attach). It takes no pointer. An empty key draws nothing.
func KeyHint(c *ui.Context, key string, at, self ui.Anchor) {
	if key == "" {
		return
	}
	p := theme.Current()
	chip := ui.Box(c).Label(LabelKeyHint).AccessHidden(true).IgnoreInput(true).
		Attach(at, self).ZIndex(1).Shrink(0).
		Height(SizeKeyHint).MinWidth(SizeKeyHint).Padding(0, theme.Space4).
		AlignItems(ui.Center).Justify(ui.Center).Radius(theme.Radius4).Border(1, p.FloatLine)
	GlassFloat(chip, p).Children(func() {
		Styled(ui.Text(c, key), theme.StyleKbd).NoWrap().TextColor(p.Ink100)
	})
}
