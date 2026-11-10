package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// Sizes of the icon inside an IconButton. The drawing is 16px; it shows at 14.
const iconDraw = 14

// SizeIconButtonSmall is the box of an IconButton in a panel footer.
const SizeIconButtonSmall = 24

// IconButtonOpts describes an IconButton (docs/spec/IconButton.md).
type IconButtonOpts struct {
	// Icon is the name of a line icon (icons.LineNames).
	Icon string
	// Label names the button for assistive technology and for the tooltip.
	// It is required.
	Label string
	// Key is the window shortcut, shown after the label in the tooltip.
	Key string
	// Hint is the key chip that shows over the bottom right corner while ⌘ is held (KeyHint).
	// Empty or Disabled: no chip.
	Hint string
	// Small makes a 24px box, for panel footers; the default is 28px.
	Small bool
	// On makes a toggle button: it is pressed while *On is true. The model
	// owns the state; a click only reports through OnClick (MyGo's Changed).
	On      *bool
	OnClick func()
	// Disabled dims the icon and removes the hover and the tooltip key.
	Disabled bool
	// Dot draws a small dot at the top right corner, in ink-100: the button
	// has something that waits for the owner (D126). Name it in Label too,
	// for the screen reader.
	Dot bool
	// Menu, when set, makes the button open a menu drawn in the window
	// (MyGo's PopoverMenuButton) instead of calling OnClick.
	Menu func(m *ui.Menu)
}

// SizeIconButtonDot is the width of the dot of an IconButton.
const SizeIconButtonDot = 6

// TipText is the tooltip of a button: the label, and the key after two
// spaces. A disabled button shows no key.
func TipText(o IconButtonOpts) string {
	if o.Key == "" || o.Disabled {
		return o.Label
	}
	return o.Label + "  " + o.Key
}

// IconLook returns the fill and the icon color of an IconButton for the
// palette p. hover includes the pointer held down. A zero fill means none.
func IconLook(p *theme.Palette, hover, pressed, disabled bool) (fill, ink ui.Color) {
	switch {
	case disabled:
		return ui.Color{}, p.Ink22
	case hover:
		return p.HoverStrong, p.Ink100
	case pressed:
		return ui.Color{}, p.Ink85
	}
	return ui.Color{}, p.Ink50
}

// IconButton builds a square button with one line icon. It is a ButtonBase,
// or a ToggleBase when o.On is set, so it keeps the keyboard, the focus ring
// (drawn here: 1px, 2px outside, the focus-ring token) and the
// screen-reader label of MyGo's buttons.
func IconButton(c *ui.Context, o IconButtonOpts) ui.Element {
	size := float32(theme.SizeIconButton)
	if o.Small {
		size = SizeIconButtonSmall
	}
	var b ui.Element
	if o.On != nil {
		b = ui.ToggleBase(c, o.On)
		if o.OnClick != nil {
			b = b.OnChange(o.OnClick)
		}
	} else {
		b = ui.ButtonBase(c)
		if o.OnClick != nil {
			b = b.OnClick(o.OnClick)
		}
	}
	hover := b.Hovered() || b.Pressed()
	fill, ink := IconLook(theme.Current(), hover, o.On != nil && *o.On, o.Disabled)
	b = b.Label(o.Label).Size(size, size).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.Radius8).TextColor(ink).Disabled(o.Disabled)
	b = b.Tooltip(TipText(o)).FocusRing(false)
	if o.Menu != nil {
		b = b.PopoverMenuButton(o.Menu)
	}
	if !o.Disabled {
		b = b.Cursor(ui.CursorPointer)
		if fill != (ui.Color{}) {
			b = b.Background(fill)
		}
	}
	focus := b.FocusVisible()
	b = b.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if focus {
			// 1px ring, 2px outside the box (docs/spec/IconButton.md).
			const gap, width = 2, 1
			p.Stroke(ui.Rect{X: r.X - gap - width, Y: r.Y - gap - width, W: r.W + 2*(gap+width), H: r.H + 2*(gap+width)},
				theme.Current().FocusRing, theme.Radius8+gap+width, width)
		}
		if o.Dot {
			d := float32(SizeIconButtonDot)
			p.Fill(ui.Rect{X: r.X + r.W - d - 4, Y: r.Y + 4, W: d, H: d}, theme.Current().Ink100, d/2)
		}
	})
	return b.Children(func() {
		if s := icons.Line(o.Icon); s != nil {
			ui.Icon(c, s).Size(iconDraw, iconDraw)
		}
		if !o.Disabled {
			KeyHint(c, o.Hint, ui.AnchorBottomRight, ui.AnchorCenter)
		}
	})
}
