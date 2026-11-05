package app

// The button that cards and panels share: the button styles and cardButton.

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

// cardButton is a card button. With click nil the board takes no actions:
// the button is dim and does nothing. The button's accessible name is its
// label. colour is the text colour of a live plain button.
func cardButton(c *ui.Context, label string, style ButtonStyle, colour uint32, click ClickFn) {
	live := click != nil
	el := ui.ButtonBase(c).Label(label).Height(theme.AnswerButtonHeight).Shrink(0).
		PaddingX(theme.AnswerButtonPadX).AlignItems(ui.Center).Radius(theme.AnswerButtonRadius)
	text := uint32(theme.TextDim)
	switch {
	case style == ButtonFilled && live:
		el = el.Background(theme.Color(theme.YouBg))
		text = theme.YouText
	case style == ButtonPlain:
		if live {
			text = colour
		}
	default:
		el = el.Border(1, theme.Color(theme.CardBorder))
		if live {
			text = theme.Text
		}
	}
	if live {
		el = el.Cursor(ui.CursorPointer).OnClick(click)
	}
	el = el.TextColor(theme.Color(text))
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
