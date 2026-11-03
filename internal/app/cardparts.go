package app

// Parts that the answer card and the worker panel share: the button styles,
// the head line and the card text.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
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

// headLine is the head line: a status icon (items only), the title in bold
// (cut with an ellipsis, struck through when resolved), and meta in dim text
// on the right.
func headLine(c *ui.Context, icon *icons.Status, title string, resolved bool, meta string) {
	ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).Gap(theme.NeedGap).Children(func() {
		if icon != nil {
			ui.Box(c).Shrink(0).Children(func() { icons.Sized(c, *icon, theme.IconSizeSmall) })
		}
		t := ui.Text(c, title).Bold().SingleLine().Grow(1).MinWidth(0).TextColor(theme.Color(theme.Text))
		if resolved {
			t.Strikethrough()
		}
		if meta != "" {
			ui.Text(c, meta).Shrink(0).TextColor(theme.Color(theme.TextDim))
		}
	})
}

// cardText is the text of the card, in Text2, at most three lines. A longer
// text is clipped. The text takes the room that the actions line does not,
// so the actions stay at the bottom.
func cardText(c *ui.Context, text *string) {
	ui.Box(c).Grow(1).MinHeight(0).Clip().Children(func() {
		s := ""
		if text != nil {
			s = *text
		}
		ui.Column(c).MaxHeight(theme.AnswerTextHeight).Clip().Children(func() {
			ui.Text(c, s).TextColor(theme.Color(theme.Text2)).Wrap()
		})
	})
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

// stopper is a timer that can be stopped. [time.Timer] fits it.
type stopper interface{ Stop() bool }
