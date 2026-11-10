package chatview

// The team row (docs/spec/ChatView.md, D259): while the session orchestrates, one low row above
// the composer says how: the mascot, "Orchestrating · 4 agents", a short count in words, and the
// live text of the session at its right end.

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// LabelTeam names the team row.
const LabelTeam = "team row"

// Sizes of the team row and the dock.
const (
	teamGap  = theme.Space14 // between the head, the count and the live text
	countGap = theme.Space6  // between the parts of the count and their dots
	glyph    = 14            // the small status glyph and mascot
	// fadeHeight is the gradient over the bottom of the transcript, above the dock; the list
	// has as much room under its last entry, so the fade covers no entry.
	fadeHeight = 32
)

// Team is the team of an orchestrating session: its live agents (the pill's number), the open
// items of its sidebar row (D240: its own menu, those of its workers, its questions and to-dos),
// and its live workers by state. A blocked worker counts in Agents and in NeedsYou (its menu is an
// item), in no other part (D260).
type Team struct {
	Agents, NeedsYou, Error, Working, Idle int
}

// TeamHead is the head of the row: "Orchestrating", "Orchestrating · 1 agent",
// "Orchestrating · 4 agents".
func TeamHead(agents int) string {
	switch agents {
	case 0:
		return "Orchestrating"
	case 1:
		return "Orchestrating · 1 agent"
	}
	return "Orchestrating · " + strconv.Itoa(agents) + " agents"
}

// NeedsText is the count of the items for the owner, as the notch has it (D258): "1 needs you",
// "2 need you".
func NeedsText(n int) string {
	if n == 1 {
		return "1 needs you"
	}
	return strconv.Itoa(n) + " need you"
}

// TeamParts are the parts of the count, in order: needs you, error, working, idle. A part with no
// agent is left out.
func TeamParts(t Team) []string {
	var out []string
	if t.NeedsYou > 0 {
		out = append(out, NeedsText(t.NeedsYou))
	}
	for _, p := range []struct {
		n    int
		word string
	}{{t.Error, "error"}, {t.Working, "working"}, {t.Idle, "idle"}} {
		if p.n > 0 {
			out = append(out, strconv.Itoa(p.n)+" "+p.word)
		}
	}
	return out
}

// TeamText is the whole row in words: "Orchestrating · 4 agents · 1 needs you · 2 working".
func TeamText(t Team) string {
	return strings.Join(append([]string{TeamHead(t.Agents)}, TeamParts(t)...), " · ")
}

// fitParts is how many parts of the count fit in room: each part after the first takes a dot
// and two gaps before it. A room of 0 or less fits none.
func fitParts(widths []float32, dot, room float32) int {
	used := float32(0)
	for i, w := range widths {
		if i > 0 {
			w += dot + 2*countGap
		}
		if used+w > room {
			return i
		}
		used += w
	}
	return len(widths)
}

// teamOf is the team of the session at id; ok is false when it does not orchestrate or the frame
// gives no team.
func (ch *Chat) teamOf(id string) (Team, bool) {
	if ch.Team == nil {
		return Team{}, false
	}
	return ch.Team(id)
}

// team builds the team row of t. st is the session's live state: while it works, its live text
// sits at the right end. In a narrow column the live text shortens first, then the count drops its
// last parts; the head stays.
func (ch *Chat) team(c *ui.Context, t Team, st liveState) {
	pal := theme.Current()
	head := TeamHead(t.Agents)
	parts := TeamParts(t)
	what, start, timed := ch.model.Activity()
	working := st.kind == liveWorking
	elapsed := ""
	if working && timed {
		elapsed = ElapsedText(c.Now().Sub(start))
	}
	shown := len(parts)
	if w := min(ch.contentWidth(c), columnMax); w > 0 {
		room := w - glyph - theme.Space8 - measure(c, head, theme.StyleLabel) - teamGap
		if working {
			room -= teamGap + glyph // the live text keeps its glyph and time; its words go first
			if elapsed != "" {
				room -= theme.Space8 + measure(c, elapsed, theme.StyleValue)
			}
		}
		widths := make([]float32, len(parts))
		for i, p := range parts {
			widths[i] = measure(c, p, theme.StyleValue)
		}
		shown = fitParts(widths, measure(c, "·", theme.StyleValue), room)
	}
	column(c, func() {
		ui.Row(c).Label(LabelTeam).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(teamGap).Children(func() {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
				// The mascot in its orchestrating color, as the pill has it: still.
				kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWaiting, Claude: true, Small: true}).AccessHidden(true)
				kit.Styled(ui.Text(c, head), theme.StyleLabel).NoWrap().TextColor(pal.Ink85)
			})
			if shown > 0 {
				ch.teamCount(c, pal, parts[:shown], t)
			}
			if working {
				ui.Row(c).Label(LabelLive + " " + what).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
					ch.working(c, pal, what, elapsed)
				})
			}
		})
	})
}

// teamCount is the count of t in words, its parts apart by dots. "N needs you" is the one red of
// the row and a button that opens the notch's card (as ⌘J). "N error" is in ink-100: not red, as
// it is no item of the notch, and it takes no click (D260). The others are in ink-45.
func (ch *Chat) teamCount(c *ui.Context, pal *theme.Palette, parts []string, t Team) {
	needs, errAt := t.NeedsYou > 0, -1
	if t.Error > 0 {
		errAt = 0
		if needs {
			errAt = 1
		}
	}
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(countGap).Children(func() {
		for i, p := range parts {
			if i > 0 {
				kit.Styled(ui.Text(c, "·"), theme.StyleValue).NoWrap().TextColor(pal.Ink45).AccessHidden(true)
			}
			if i == 0 && needs {
				b := ui.ButtonBase(c).Label(p).Shrink(0).OnClick(func() {
					if ch.OpenNeeds != nil {
						ch.OpenNeeds()
					}
				})
				if ch.OpenNeeds != nil {
					b = b.Cursor(ui.CursorPointer)
				}
				b.Children(func() {
					kit.Styled(ui.Text(c, p), theme.StyleValue).NoWrap().TextColor(pal.BlockedText)
				})
				continue
			}
			ink := pal.Ink45
			if i == errAt {
				ink = pal.Ink100
			}
			kit.Styled(ui.Text(c, p), theme.StyleValue).NoWrap().TextColor(ink)
		}
	})
}

// working is the live text of a working session: the working glyph, what it does now in ink-62
// and the time since the turn started in ink-38. A time asks for a redraw in a second (D55).
func (ch *Chat) working(c *ui.Context, pal *theme.Palette, what, elapsed string) {
	kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true})
	kit.Styled(ui.Text(c, what), theme.StyleValue).SingleLine().MinWidth(0).TextColor(pal.Ink62)
	if elapsed != "" {
		kit.Styled(ui.Text(c, elapsed), theme.StyleValue).NoWrap().Shrink(0).TextColor(pal.Ink38)
		ch.ticks = true
		c.After(time.Second)
	}
}

// measure is the width of s in style st on one line.
func measure(c *ui.Context, s string, st theme.TextStyle) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: s, Font: st.Stack(), Size: st.Size, Weight: st.Weight})
	return w
}
