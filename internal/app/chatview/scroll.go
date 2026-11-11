package chatview

// The chat scrolled up (docs/spec/ChatView.md, D266): the prompt of the turn in view pinned at the
// top, and Jump to latest at the bottom. Both come from where the list was in the last frame
// (Visible, AtEnd): the list builds another frame only when that changes, so a chat at rest asks
// for no frame (D55).

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels and texts of the scrolled chat.
const (
	// LabelPinned names the pinned prompt; a click scrolls to the prompt.
	LabelPinned = "pinned prompt"
	// TextYou comes before the text of the pinned prompt.
	TextYou = "You"
	// TextJump is the text of the control that goes to the end; TextNew replaces it when content
	// came after the owner left the end.
	TextJump = "Jump to latest"
	TextNew  = "New messages"
)

// Sizes of the scrolled chat.
const (
	jumpHeight = 28
	// pinRight is the room at the right of the pinned prompt when the switch was not laid out yet.
	pinRight = 160
)

// scrolled is where the list was in the last frame: the row of the prompt to pin, or -1, whether
// Jump to latest shows, and whether content came since the owner left the end.
type scrolled struct {
	pin        int
	jump, news bool
}

// scrollState reads where the list was and keeps the count of shown entries when the owner left
// the end. At the end, or before the list was laid out, nothing is pinned and no jump shows.
func (ch *Chat) scrollState() scrolled {
	s := scrolled{pin: -1}
	first, last := ch.list.Visible()
	if last < first || ch.list.AtEnd() {
		ch.away = false
		return s
	}
	n := ch.model.Shown()
	if !ch.away {
		ch.away, ch.awayAt = true, n
	}
	s.jump, s.news = true, n > ch.awayAt
	s.pin = PinnedRow(ch.model.Rows(), first)
	return s
}

// PinnedRow is the row of the prompt of the turn that row first is in, when that prompt is above
// first, else -1. When first is a prompt, a pending one included (past the rows), it shows itself:
// nothing is pinned.
func PinnedRow(rows []Row, first int) int {
	if first < 0 || first >= len(rows) || rows[first].Kind == RowPrompt {
		return -1
	}
	for i := first - 1; i >= 0; i-- {
		if rows[i].Kind == RowPrompt {
			return i
		}
	}
	return -1
}

// pinned is the prompt of row i as one clipped line across the top of the chat, on the stage with
// a hairline under it: "You" and the text. The text ends before the notch and the switch. A click
// scrolls to the prompt.
func (ch *Chat) pinned(c *ui.Context, i int) {
	pal := theme.Current()
	text := strings.Join(strings.Fields(ch.model.Entry(ch.model.Rows()[i].Entry).Text), " ")
	ui.ButtonBase(c).Label(LabelPinned+" "+text).Absolute().Top(0).Left(0).Right(0).Height(topSpace).
		Row().AlignItems(ui.Center).Gap(theme.Space12).Padding(0, ch.pinnedRight(c), 0, theme.Space16).
		Background(pal.StageBase()).Cursor(ui.CursorPointer).
		OnClick(func() { ch.list.ScrollTo(i, ui.Start) }).Children(func() {
		kit.Styled(ui.Text(c, TextYou), theme.StyleSmall).NoWrap().Shrink(0).TextColor(pal.Ink45)
		kit.Styled(ui.Text(c, text), theme.StyleSmall).SingleLine().Grow(1).MinWidth(0).TextColor(pal.Ink100)
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(pal.Hairline)
	})
}

// pinnedRight is the room at the right of the pinned prompt: up to the switch, and up to the
// notch while it shows over the bar, each with space-12 to spare. A box that is not over the bar
// takes no room.
func (ch *Chat) pinnedRight(c *ui.Context) float32 {
	p := ch.panel.Bounds(c)
	if p.W <= 0 {
		return pinRight
	}
	over := func(r ui.Rect) bool { return r.W > 0 && r.Y < p.Y+topSpace && r.Y+r.H > p.Y && r.X > p.X }
	right := float32(theme.Space16)
	if sw := ch.switchBox.Bounds(c); over(sw) {
		right = max(right, p.X+p.W-sw.X+theme.Space12)
	}
	if ch.Notch != nil {
		if n, ok := ch.Notch(); ok && over(n) {
			right = max(right, p.X+p.W-n.X+theme.Space12)
		}
	}
	return right
}

// jump is the control at the bottom of the chat that goes to the end, where the list follows
// again. When content came since the owner left the end, it says so, with a claude dot.
func (ch *Chat) jump(c *ui.Context, news bool) {
	pal := theme.Current()
	text, label := TextJump, TextJump
	if news {
		text, label = TextNew, TextJump+", "+strings.ToLower(TextNew)
	}
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(theme.Space8).Justify(ui.Center).PassThrough().Children(func() {
		b := ui.ButtonBase(c).Label(label).Row().Height(jumpHeight).AlignItems(ui.Center).Gap(theme.Space8).
			Padding(0, 13, 0, 11).Radius(jumpHeight/2).Background(pal.FloatSolid).Border(1, pal.FloatLine).
			Cursor(ui.CursorPointer).OnClick(func() {
			ch.list.ScrollToEnd()
			ch.away = false
		})
		for _, l := range pal.ShadowFloat {
			if !l.Inset {
				b = b.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			}
		}
		b.Children(func() {
			if news {
				ui.Box(c).Size(dotSize, dotSize).Shrink(0).Radius(dotSize / 2).Background(pal.Claude).AccessHidden(true)
			}
			kit.Styled(ui.Text(c, text), theme.StyleSmall).NoWrap().TextColor(pal.Ink100)
			kit.Styled(ui.Text(c, "↓"), theme.StyleSmall).NoWrap().TextColor(pal.Ink100).AccessHidden(true)
		})
	})
}
