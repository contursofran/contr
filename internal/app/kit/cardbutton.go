package kit

// The button that cards and panels share: the button styles and CardButton.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// ClickFn is a click handler.
type ClickFn func()

// ButtonStyle says how a card button is drawn.
type ButtonStyle int

// The button styles.
const (
	// ButtonFilled is white, with bold dark text: the owner acts.
	ButtonFilled ButtonStyle = iota
	// ButtonOutlined is outlined grey, with the text colour.
	ButtonOutlined
	// ButtonPlain has no border; its text is underlined on hover.
	ButtonPlain
)

// OptionStyle is the style of an option button: the recommended option is
// filled, the others are outlined.
func OptionStyle(recommended bool) ButtonStyle {
	if recommended {
		return ButtonFilled
	}
	return ButtonOutlined
}

// CardButton is a card button. With click nil the board takes no actions:
// the button is dim and does nothing. The button's accessible name is its
// label. colour is the text colour of a live plain button.
func CardButton(c *ui.Context, label string, style ButtonStyle, colour ui.Color, click ClickFn) {
	live := click != nil
	el := ui.ButtonBase(c).Label(label).Height(theme.AnswerButtonHeight).Shrink(0).
		PaddingX(theme.AnswerButtonPadX).AlignItems(ui.Center).Radius(theme.AnswerButtonRadius)
	p := theme.Current()
	text := p.Ink48
	switch {
	case style == ButtonFilled && live:
		el = el.Background(p.Ink100)
		text = p.Ground // the inverse of ink-100 in both themes
	case style == ButtonPlain:
		if live {
			text = colour
		}
	default:
		el = el.Border(1, p.InputLine)
		if live {
			text = p.Ink88
		}
	}
	if live {
		el = el.Cursor(ui.CursorPointer).OnClick(click)
	}
	el = el.TextColor(text)
	el.Children(func() {
		t := ui.Text(c, label)
		if style == ButtonFilled && live {
			t = t.Bold()
		}
		if style == ButtonPlain && live && el.Hovered() {
			t.Underline()
		}
	})
}

// KeyStyle says how a [KeyButton] is drawn (D255).
type KeyStyle int

// The key button styles.
const (
	// KeyMain is ink-100 with ground text: the one fill of an empty window, on the stage.
	KeyMain KeyStyle = iota
	// KeyPlain is ink-64 text and its key, no fill or border; the label underlines on hover.
	KeyPlain
	// KeyRow is ink-88 text and its key, no fill or border; on hover the fill of a plain row.
	KeyRow
)

// KeyButton is a first-run action with its key at the right (D255), such as "Open a project ⌘O",
// in the given style. The accessible name is the label alone, so a click by label finds it.
// The label shortens with an ellipsis when the button is narrow; the key stays.
func KeyButton(c *ui.Context, label, key string, style KeyStyle, click ClickFn) ui.Element {
	p := theme.Current()
	text, keyInk := p.Ink64, p.Ink48
	el := ui.ButtonBase(c).Label(label).Height(theme.KeyButtonHeight).MinWidth(0).
		Padding(0, theme.KeyButtonPadR, 0, theme.KeyButtonPadL).AlignItems(ui.Center).Gap(theme.Space8).
		Radius(theme.Radius8)
	main := style == KeyMain
	switch {
	case main:
		el = el.Background(p.Ink100)
		text, keyInk = p.Ground, p.Ground.Alpha(0.6)
	case style == KeyRow:
		text = p.Ink88
		// ButtonBase has its own look; the explicit background keeps it flat, as a plain row.
		if el.Hovered() {
			el = el.Background(p.Hover)
		} else {
			el = el.Background(ui.Transparent)
		}
	}
	if click != nil {
		el = el.Cursor(ui.CursorPointer).OnClick(click)
	}
	return el.TextColor(text).Children(func() {
		t := Styled(ui.Text(c, label), theme.StyleSmall).SingleLine().MinWidth(0).Shrink(1).TextColor(text)
		if main {
			t.FontWeight(theme.StyleLabel.Weight)
		} else if style == KeyPlain && el.Hovered() {
			t.Underline()
		}
		if key != "" {
			Styled(ui.Text(c, key), theme.StyleKbd).NoWrap().Shrink(0).TextColor(keyInk)
		}
	})
}
