package chatview

// The important rows of the chat (D270): the one place that says which rows get a tick in the
// outline, and of which kind.

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/transcript"
)

// MarkKind is the kind of an important row.
type MarkKind int

// The kinds of marks.
const (
	// MarkYou is a prompt of the owner.
	MarkYou MarkKind = iota
	// MarkDecision, MarkWarning, MarkDone and MarkUnverified are remarks in Claude's text.
	MarkDecision
	MarkWarning
	MarkDone
	MarkUnverified
	// MarkNeedsYou is a note for the owner outside the fold.
	MarkNeedsYou
	// MarkPinned is a call of pin_for_owner, MarkAsked one of ask_owner.
	MarkPinned
	MarkAsked
)

var markWords = [...]string{"You", "Decision", "Warning", "Done", "Not verified", "Needs you", "Pinned", "Question"}

// Word is the kind as the outline card says it.
func (k MarkKind) Word() string { return markWords[k] }

// Mark is one important thing of the chat, in thread order.
type Mark struct {
	Kind MarkKind
	// Row is the index of its row in Rows: a call's row is its work group.
	Row int
	// ID is stable while the transcript grows: the row id, and the place of a remark in its text.
	ID string
	// Text is the first words, on one line; "" for a pin or a question, whose text is in the
	// owner store under Item.
	Text string
	// Item is the owner item id of a pin or a question ("T3", "D2"), from the call's result; ""
	// while the result has not come.
	Item string
	// At is the time of its entry; zero when it has none (a sent prompt, D265).
	At time.Time
	// Waits is set for a decision remark that no prompt of the owner follows.
	Waits bool
}

// markTextMax is the most runes of a mark's text: more than two lines of the card show.
const markTextMax = 160

// Marks are the important rows of m, in thread order (D270).
func Marks(m *Model) []Mark {
	var out []Mark
	for i, r := range m.rows {
		switch r.Kind {
		case RowPrompt:
			text := r.Text
			var at time.Time
			if !r.Sent() {
				e := m.entries[r.Entry]
				text, at = e.Text, entryTime(e)
			}
			out = append(out, Mark{Kind: MarkYou, Row: i, ID: r.ID, Text: firstWords(text), At: at})
		case RowSaid:
			e := m.entries[r.Entry]
			for k, rm := range remarksIn(e.Text) {
				kind, ok := remarkMarks[rm.Kind]
				if !ok {
					continue
				}
				out = append(out, Mark{Kind: kind, Row: i, ID: r.ID + "/" + strconv.Itoa(k), Text: firstWords(rm.Body), At: entryTime(e)})
			}
		case RowOwnerNote:
			e := m.entries[r.Entry]
			text := e.Text
			if e.Note != nil && e.Note.Kind == transcript.NoteTask {
				text = TextTask + " " + strings.ToLower(e.Note.Status) + ": " + TaskSummary(e.Text)
			}
			out = append(out, Mark{Kind: MarkNeedsYou, Row: i, ID: r.ID, Text: firstWords(text), At: entryTime(e)})
		case RowGroup:
			for _, ci := range r.Calls {
				e := m.entries[ci]
				kind, ok := ownerCall(e.Tool)
				if !ok {
					continue
				}
				item := ""
				if e.Tool.Done {
					item = strings.TrimSpace(e.Tool.Excerpt)
				}
				out = append(out, Mark{Kind: kind, Row: i, ID: callID(e.Tool.ID), Item: item, At: entryTime(e)})
			}
		}
	}
	// A decision waits until the owner writes after it.
	waits := true
	for j := len(out) - 1; j >= 0; j-- {
		switch out[j].Kind {
		case MarkYou:
			waits = false
		case MarkDecision:
			out[j].Waits = waits
		}
	}
	return out
}

// remarkMarks are the remarks that get a tick: a note does not.
var remarkMarks = map[RemarkKind]MarkKind{
	RemarkDecision: MarkDecision, RemarkWarning: MarkWarning, RemarkDone: MarkDone, RemarkUnverified: MarkUnverified,
}

// ownerCall tells a call of pin_for_owner or ask_owner that did not fail.
func ownerCall(c *transcript.Call) (MarkKind, bool) {
	if c == nil || c.Error {
		return 0, false
	}
	switch {
	case c.Name == "pin_for_owner" || strings.HasSuffix(c.Name, "__pin_for_owner"):
		return MarkPinned, true
	case c.Name == "ask_owner" || strings.HasSuffix(c.Name, "__ask_owner"):
		return MarkAsked, true
	}
	return 0, false
}

// entryTime is the time of e, zero when it has none or it does not parse.
func entryTime(e transcript.Entry) time.Time {
	t, err := time.Parse(time.RFC3339Nano, e.Time)
	if err != nil {
		return time.Time{}
	}
	return t
}

// firstWords is text on one line, cut at markTextMax runes.
func firstWords(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(s) <= markTextMax {
		return s
	}
	return string([]rune(s)[:markTextMax]) + "…"
}

// remarksIn are the remarks of a Markdown text: the quotes at the top level whose first line is
// a tag. Quotes in fenced code are not remarks.
func remarksIn(text string) []Remark {
	var out []Remark
	var quote []string
	end := func() {
		if len(quote) > 0 {
			if r, ok := RemarkOf(quoteText(strings.Join(quote, "\n"))); ok {
				out = append(out, r)
			}
			quote = nil
		}
	}
	fence := ""
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimLeft(l, " ")
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		}
		if len(l)-len(t) < 4 && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")) {
			end()
			fence = t[:3]
			continue
		}
		if len(l)-len(t) < 4 && strings.HasPrefix(t, ">") {
			quote = append(quote, l)
			continue
		}
		end()
	}
	end()
	return out
}

// Marks are the important rows of the chat that shows (D270).
func (ch *Chat) Marks() []Mark { return Marks(&ch.model) }

// ScrollToRow scrolls the chat so that row i is at the top of the list.
func (ch *Chat) ScrollToRow(i int) {
	ch.list.ScrollTo(i, ui.Start)
	ch.Env.Redraw()
}

// Area is the room of the transcript list in the chat, from the last frame drawn: the space
// above it (the switch row) and the space below it (the dock). ok is false before the chat was
// drawn.
func (ch *Chat) Area() (top, bottom float32, ok bool) {
	p, l := ch.panelRect, ch.listRect
	if p.H <= 0 || l.H <= 0 {
		return 0, 0, false
	}
	return l.Y - p.Y + topSpace, p.Y + p.H - (l.Y + l.H), true
}

// keepRect keeps r, a box as drawn, in *at, and asks for one more frame when it changed: the
// outline, built before the layout, follows the list's room (D270).
func (ch *Chat) keepRect(at *ui.Rect, r ui.Rect) {
	if *at == r {
		return
	}
	*at = r
	ch.Env.Redraw()
}
