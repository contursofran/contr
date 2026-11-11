package viewers

// The needs-you section of the board: the same items as the former needs-you row
// under the terminal, in the same order (blocked workers, questions, to-dos),
// one line each. A click opens the questions card (#347).

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the needs-you section.
const (
	LabelNeedsSection = "needs section"
	LabelNeedsHeader  = "needs header"
	LabelNeedsRows    = "needs rows"
	LabelNeedsEmpty   = "Nothing waits for you."
)

// NeedKindLabel is the word on the right of a line.
func NeedKindLabel(t agentstate.NeedTab) string {
	switch {
	case t.Resolved:
		return "done"
	case t.Kind == agentstate.NeedBlocked:
		return "blocked"
	case t.Kind == agentstate.NeedQuestion:
		return "decide"
	case t.Icon == icons.Fyi:
		return "fyi"
	}
	return "do"
}

// NeedsCountLabel is the label of the open count in the head.
func NeedsCountLabel(n int) string { return "needs count " + strconv.Itoa(n) }

// NeedsSection adds the section. It takes the rest of the panel's height and
// its lines scroll inside it. onOpen gets the clicked tab.
func NeedsSection(c *ui.Context, tabs []agentstate.NeedTab, onOpen func(agentstate.NeedTab)) {
	open := 0
	for _, t := range tabs {
		if !t.Resolved {
			open++
		}
	}
	ui.Column(c).Label(LabelNeedsSection).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Label(LabelNeedsHeader).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
			AlignItems(ui.Center).Gap(theme.NeedGap).PaddingX(theme.PanelPadX).Children(func() {
			ui.Text(c, "Needs you").TextColor(theme.Color(theme.TextDim))
			ui.Text(c, strconv.Itoa(open)).Label(NeedsCountLabel(open)).Bold().TextColor(theme.Color(theme.Text))
		})
		ui.Scroll(c).Label(LabelNeedsRows).FillWidth().Grow(1).MinHeight(0).Children(func() {
			if len(tabs) == 0 {
				TextLine(c, LabelNeedsEmpty, LabelNeedsEmpty, theme.Current().Ink48)
				return
			}
			for _, t := range tabs {
				needLine(c, t, onOpen)
			}
		})
	})
}

func needLine(c *ui.Context, t agentstate.NeedTab, onOpen func(agentstate.NeedTab)) {
	text := uint32(theme.Text)
	if t.Resolved {
		text = theme.TextDim
	}
	ui.ButtonBase(c).Label(t.Label() + ", " + NeedKindLabel(t)).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.NeedGap).PaddingX(theme.PanelPadX).Radius(theme.CardRadius).
		Cursor(ui.CursorPointer).OnClick(func() {
		if onOpen != nil {
			onOpen(t)
		}
	}).Children(func() {
		icons.Sized(c, t.Icon, theme.IconSizeSmall)
		// The key keeps at least four characters so the titles line up. A long
		// worker name takes the free width and clips on one line (#207).
		ui.Text(c, t.Key).MinWidth(theme.NeedKeyWidth).SingleLine().Ellipsis("…").TextColor(theme.Color(text))
		title := ui.Text(c, t.Title).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").TextColor(theme.Color(text))
		if t.Resolved {
			title.Strikethrough()
		}
		ui.Text(c, NeedKindLabel(t)).Shrink(0).TextColor(theme.Color(theme.TextDim))
	})
}
