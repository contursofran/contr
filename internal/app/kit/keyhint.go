package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// LabelKeyHint starts the name of each key chip, for tests and the e2e driver (KeyHintLabel).
// Screen readers do not see the chip: the element's label and tooltip name its key.
const LabelKeyHint = "key hint"

// KeyHintLabel names the chip of key: "key hint ⌥↓". Each chip on screen has its own key, so
// a test finds each chip's box by its name.
func KeyHintLabel(key string) string { return LabelKeyHint + " " + key }

// SizeKeyHint is the height of a key chip and its least width: the kbd line and a 1px border
// and padding at each side.
const SizeKeyHint = 18

// keyHintBorder is the border of a key chip at each side.
const keyHintBorder = 1

// KeyHint draws the key chip of a shortcut while ⌘ is held (docs/spec/KeyHints.md, D248): kbd
// text in ink-100 on glass-float with a float-line border. Build it as the last child of box,
// the element it names. One rule places it: its center sits on the center of icon, the icon or
// glyph of the element, and it stays fully inside box and the window. A chip wider than the icon
// near an edge moves in along that edge. The places come from the previous frame's boxes; with
// none (the first frame of the element) the chip is centered on box. It is out of the layout, so
// nothing moves, and it takes no pointer. An empty key draws nothing.
func KeyHint(c *ui.Context, key string, box, icon ui.Element) {
	if key == "" {
		return
	}
	p := theme.Current()
	w := keyHintWidth(c, key)
	winW, winH := c.Size()
	dx, dy := keyHintOffset(box.LayoutBounds(), icon.LayoutBounds(), w, SizeKeyHint, winW, winH)
	chip := ui.Box(c).Label(KeyHintLabel(key)).AccessHidden(true).IgnoreInput(true).
		Attach(ui.AnchorCenter, ui.AnchorCenter).ZIndex(1).Shrink(0).
		Width(w).Height(SizeKeyHint).Padding(0, theme.Space4).
		AlignItems(ui.Center).Justify(ui.Center).Radius(theme.Radius4).Border(keyHintBorder, p.FloatLine)
	if dx != 0 || dy != 0 {
		chip = chip.Translate(dx, dy)
	}
	GlassFloat(chip, p).Children(func() {
		Styled(ui.Text(c, key), theme.StyleKbd).NoWrap().TextColor(p.Ink100)
	})
}

// keyHintWidth is the width of the chip of key: the kbd text, the padding and the border, and
// at least SizeKeyHint. The chip takes this width, so its place does not wait for a layout.
func keyHintWidth(c *ui.Context, key string) float32 {
	s := theme.StyleKbd
	tw, _ := c.MeasureText(0, ui.Span{Text: key, Font: s.Stack(), Size: s.Size, Weight: s.Weight})
	return max(SizeKeyHint, tw+2*theme.Space4+2*keyHintBorder)
}

// keyHintOffset is the move of a chip of w×h from the center of box to the center of icon,
// held so the chip stays inside box and inside the window of winW×winH. All boxes are in window
// coordinates. An empty box gives no move; an empty icon puts the chip on the center of box.
func keyHintOffset(box, icon ui.Rect, w, h, winW, winH float32) (dx, dy float32) {
	if box.W <= 0 || box.H <= 0 {
		return 0, 0
	}
	bx, by := box.X+box.W/2, box.Y+box.H/2
	cx, cy := bx, by
	if icon.W > 0 && icon.H > 0 {
		cx, cy = icon.X+icon.W/2, icon.Y+icon.H/2
	}
	cx = clampCenter(cx, w, box.X, box.X+box.W)
	cy = clampCenter(cy, h, box.Y, box.Y+box.H)
	if winW > 0 {
		cx = clampCenter(cx, w, 0, winW)
	}
	if winH > 0 {
		cy = clampCenter(cy, h, 0, winH)
	}
	return cx - bx, cy - by
}

// clampCenter moves the center c of a span of size n so the span lies in lo..hi. A span longer
// than lo..hi is centered on it.
func clampCenter(c, n, lo, hi float32) float32 {
	if hi-lo <= n {
		return (lo + hi) / 2
	}
	return min(max(c, lo+n/2), hi-n/2)
}
