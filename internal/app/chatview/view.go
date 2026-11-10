package chatview

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/shortcut"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/transcript"
)

// Labels and texts of the chat view.
const (
	// LabelChat names the chat view.
	LabelChat = "chat"
	// LabelSwitch names the Chat | Terminal switch.
	LabelSwitch = "View of this session"
	// LabelSwitchBack names the solid box under the switch.
	LabelSwitchBack = "View switch"
	TextChat        = "Chat"
	TextTerm        = "Terminal"
	// TextEmpty shows while the session has no transcript, or no entry in it yet.
	TextEmpty = "No transcript yet"
	// TextToTerminal is the button of the needs-you line without a menu: it switches to Terminal.
	TextToTerminal = "Switch to Terminal"
	// LabelPrompt, LabelGroup, LabelOwnerNote, LabelNote and LabelCall name the rows and lines.
	LabelPrompt = "owner prompt"
	// LabelPending names a prompt sent from the composer that the transcript has not brought yet.
	LabelPending   = "pending prompt"
	LabelGroup     = "work group"
	LabelOwnerNote = "note for the owner"
	LabelNote      = "worker note"
	LabelCall      = "tool call"
	// TextError marks a call whose result is an error.
	TextError = "error"
)

// Sizes of docs/spec/ChatView.md.
const (
	columnMax  = 920 // the column of prose, centered
	promptMax  = 560
	groupMax   = 584
	lineHeight = 20 // a line of an open group, and the group row
	nameWidth  = 48 // the tool name column of a call line
	dotSize    = 6
	topSpace   = 44 // room for the switch row above the first row
	rowGap     = theme.Space18
	// pendingOpacity dims a pending prompt.
	pendingOpacity = 0.55
)

// Switch builds the Chat | Terminal switch of terminal id, with its key chip (⌘E).
func (ch *Chat) Switch(c *ui.Context, id string) {
	choice := 1
	if ch.On(id) {
		choice = 0
	}
	// Solid: the float-solid fill and the float-line border keep the terminal text under it from
	// showing through; the chosen segment keeps its selected fill.
	pal := theme.Current()
	box := ui.Box(c).Label(LabelSwitchBack).Shrink(0).Radius(theme.SegmentRadius).Background(pal.FloatSolid).Border(1, pal.FloatLine)
	box.Children(func() {
		kit.Switch(c, LabelSwitch, &choice, TextChat, TextTerm).Tooltip(LabelSwitch + " " + shortcut.Of(shortcut.Chat).Glyphs())
		// The switch has no icon: the chip is centered on it, inside it (KeyHint, D248).
		kit.KeyHint(c, shortcut.Of(shortcut.Chat).HintIf(ch.Hints), box, ui.Element{})
	})
	if on := choice == 0; on != ch.On(id) {
		ch.SetOn(id, on)
	}
}

// View builds the chat of the terminal that Sync follows: the transcript in a list that follows
// its end, with the pending prompts at its end, the live line and the composer. toTerminal
// switches the terminal to Terminal mode.
func (ch *Chat) View(c *ui.Context, toTerminal func()) {
	ch.show()
	ch.toTerm = toTerminal
	ch.ticks = false
	pal := theme.Current()
	ch.mdTheme(pal)
	id := ch.src.Terminal()
	st := ch.state(id)
	ui.Column(c).Label(LabelChat).Fill().Bind(&ch.panel).Children(func() {
		rows, pending := ch.model.Rows(), ch.model.Pendings()
		ui.List(c, &ch.list, len(rows)+len(pending), func(i int) {
			if i < len(rows) {
				ch.row(c, rows[i], i+1 < len(rows) && rows[i+1].Kind == RowSaid)
				return
			}
			p := pending[i-len(rows)]
			column(c.Key(p.ID), func() { ch.prompt(c, p.Text, true) })
		}).
			Grow(1).MinHeight(0).Gap(rowGap).Padding(topSpace, theme.Space20, theme.Space12).Justify(ui.End).
			Children(func() {
				if len(rows)+len(pending) == 0 {
					ui.Box(c).Fill().Center().Children(func() {
						kit.Styled(ui.Text(c, TextEmpty), theme.StyleBody).TextColor(pal.Ink42)
					})
				}
			})
		ui.Column(c).FillWidth().Shrink(0).Gap(theme.Space12).Padding(0, theme.Space20, theme.Space14).Children(func() {
			ch.live(c, st, toTerminal)
			ch.composer(c, id, st)
		})
	})
}

// column centers a row's content in the content column.
func column(c *ui.Context, fn func()) {
	ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).MaxWidth(columnMax).Children(fn)
	})
}

// row builds row r. moreSaid tells that a text of Claude comes next: the time of an answer shows
// once, under its last text.
func (ch *Chat) row(c *ui.Context, r Row, moreSaid bool) {
	c = c.Key(r.ID)
	switch r.Kind {
	case RowPrompt:
		e := ch.model.Entry(r.Entry)
		column(c, func() {
			ch.prompt(c, e.Text, false)
			sentAt(c, e.Time, ui.End)
		})
	case RowSaid:
		e := ch.model.Entry(r.Entry)
		ch.said(c, r.ID, e.Text)
		if !moreSaid {
			column(c, func() { sentAt(c, e.Time, ui.Start) })
		}
	case RowGroup:
		ch.group(c, r)
	case RowOwnerNote:
		column(c, func() { ch.noteLine(c, ch.model.Entry(r.Entry), true) })
	}
}

// sentAt is the line with the time an entry was sent, at the start or the end of the column. A
// time that is missing or does not parse shows nothing. It is a fixed text: nothing ticks (D55).
func sentAt(c *ui.Context, at string, align ui.Align) {
	t, err := time.Parse(time.RFC3339Nano, at)
	if at == "" || err != nil {
		return
	}
	kit.Styled(ui.Text(c, TimeText(t, c.Now())), theme.StyleMeta).NoWrap().AlignSelf(align).
		Margin(theme.Space4, 0, 0, 0).TextColor(theme.Current().Ink45)
}

// TimeText is when an entry was sent, in the time zone of now: "14:32" today, "Yesterday 14:32",
// else "9 Nov 14:32".
func TimeText(t, now time.Time) string {
	t = t.In(now.Location())
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today) && t.Before(today.AddDate(0, 0, 1)):
		return t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)) && t.Before(today):
		return "Yesterday " + t.Format("15:04")
	}
	return t.Format("2 Jan 15:04")
}

// prompt is the owner's text, at the right, on a hover fill, in ink-100. A pending prompt is
// dimmed.
func (ch *Chat) prompt(c *ui.Context, text string, pending bool) {
	pal := theme.Current()
	label, opacity := LabelPrompt, float32(1)
	if pending {
		label, opacity = LabelPending, pendingOpacity
	}
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		ui.Box(c).Label(label).MaxWidth(promptMax).Padding(theme.Space8, 13).Radius(theme.Radius12).
			Background(pal.Hover).Opacity(opacity).Children(func() {
			kit.Styled(ui.Text(c, text), theme.StyleProse).TextColor(pal.Ink100).Selectable()
		})
	})
}

// group is a work group: a fold row with the counts, the dots of the senders and, while a call
// runs, the working glyph. Open, a card lists the notes, then the calls.
func (ch *Chat) group(c *ui.Context, r Row) {
	open := ch.folds[r.ID]
	if open == nil {
		open = new(bool)
		ch.folds[r.ID] = open
	}
	text := GroupText(len(r.Notes), len(r.Calls))
	ui.Column(c).FillWidth().Children(func() { ch.groupBody(c, r, open, text) })
}

// groupBody is the fold row in the column and, open, the card in a wide box.
func (ch *Chat) groupBody(c *ui.Context, r Row, open *bool, text string) {
	pal := theme.Current()
	p := ui.CollapsibleBase(c, open)
	ink := pal.Ink42
	if p.Trigger.Hovered() {
		ink = pal.Ink62
	}
	// The trigger is placed where CollapsibleBase makes it: a margin puts it on the column's left.
	ch.groupRow(c, p, r, open, text, ink)
	if p.Trigger.Clicked() {
		ch.list.HoldPlace()
	}
	p.Panel(func() {
		ch.wide(c, func() {
			card := ui.Column(c).AlignSelf(ui.Start).MinWidth(min(groupMax, ch.contentWidth(c))).
				Margin(theme.Space6, 0, 0, 0).Padding(theme.Space8, theme.Space12).
				Radius(theme.Radius9).Border(1, pal.Hairline)
			if w := ch.contentWidth(c); w > 0 {
				card.MaxWidth(w)
			}
			card.Children(func() {
				for _, i := range r.Notes {
					ch.noteLine(c, ch.model.Entry(i), false)
				}
				if len(r.Notes) > 0 && len(r.Calls) > 0 {
					ui.Box(c).FillWidth().Height(1).Margin(theme.Space6, 0).Background(pal.Hairline)
				}
				for _, i := range r.Calls {
					ch.callLine(c, ch.model.Entry(i))
				}
			})
		})
	})
}

// groupRow is the fold row of a work group.
func (ch *Chat) groupRow(c *ui.Context, p ui.CollapsibleParts, r Row, open *bool, text string, ink ui.Color) {
	p.Trigger.Label(LabelGroup+" "+text).Row().AlignSelf(ui.Start).Margin(0, 0, 0, ch.columnLeft(c)).Height(lineHeight).AlignItems(ui.Center).
		Gap(theme.Space8).Cursor(ui.CursorPointer).Children(func() {
		if ch.model.Running(r) {
			kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true})
		}
		kit.Styled(ui.Text(c, text), theme.StyleValue).NoWrap().TextColor(ink)
		for _, addr := range ch.model.Senders(r) {
			ch.dot(c, addr)
		}
		chev := "›"
		if *open {
			chev = "⌄"
		}
		kit.Styled(ui.Text(c, chev), theme.StyleValue).TextColor(ink).AccessHidden(true)
	})
}

// dot is the state of the worker at addr: blocked in the blocked hue, working in ink-85, else
// ink-40.
func (ch *Chat) dot(c *ui.Context, addr string) {
	pal := theme.Current()
	color, state := pal.Ink40, "idle"
	if ch.Worker != nil {
		if w, ok := ch.Worker(addr); ok {
			state = w.Status
			switch w.Status {
			case "blocked":
				color = pal.Blocked
			case "working":
				color = pal.Ink85
			}
			ch.dotBox(c, w.DisplayName()+" "+state, color)
			return
		}
	}
	ch.dotBox(c, addr+" "+state, color)
}

func (ch *Chat) dotBox(c *ui.Context, label string, color ui.Color) {
	ui.Box(c).Label(label).Size(dotSize, dotSize).Shrink(0).Radius(dotSize / 2).Background(color)
}

// noteLine is one kit note: the sender's dot and name, and its text on one line. A note that
// needs the owner (own) has its name in ink-100 and its text in ink-85.
func (ch *Chat) noteLine(c *ui.Context, e transcript.Entry, own bool) {
	pal := theme.Current()
	name, addr := "owner", ""
	if e.Note != nil && e.Note.Name != "" {
		name, addr = e.Note.Name, e.Note.Addr
	}
	label, nameInk, textInk := LabelNote, pal.Ink85, pal.Ink42
	if own {
		label, nameInk, textInk = LabelOwnerNote, pal.Ink100, pal.Ink85
	}
	ui.Row(c).Label(label + " " + name).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
		if own {
			ch.dotBox(c, name+" needs you", pal.Blocked)
		} else if addr != "" {
			ch.dot(c, addr)
		}
		kit.Styled(ui.Text(c, name), theme.StyleMeta).FontWeight(500).NoWrap().Shrink(0).TextColor(nameInk)
		kit.Styled(ui.Text(c, e.Text), theme.StyleMeta).SingleLine().Grow(1).MinWidth(0).TextColor(textInk)
	})
}

// callLine is one tool call: the working glyph while it runs, its name, its summary, an error
// mark and how long it took.
func (ch *Chat) callLine(c *ui.Context, e transcript.Entry) {
	pal := theme.Current()
	call := e.Tool
	name := ToolName(call.Name)
	ui.Row(c).Label(LabelCall + " " + name).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
		if !call.Done {
			kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true})
		}
		kit.Styled(ui.Text(c, name), theme.StyleMeta).NoWrap().MinWidth(nameWidth).Shrink(0).TextColor(pal.Ink62)
		kit.Styled(ui.Text(c, call.Summary), theme.StyleMeta).SingleLine().Grow(1).MinWidth(0).TextColor(pal.Ink42)
		if call.Error {
			kit.Styled(ui.Text(c, TextError), theme.StyleMeta).NoWrap().Shrink(0).TextColor(pal.Blocked)
		}
		if d, ok := Duration(e); ok {
			kit.Styled(ui.Text(c, DurationText(d)), theme.StyleMeta).NoWrap().Shrink(0).TextColor(pal.Ink38)
		}
	})
}

// mdTheme styles Claude's Markdown with the tokens of the current theme: prose in ink-85, strong
// text and links in ink-95, code in mono.
func (ch *Chat) mdTheme(pal *theme.Palette) {
	if ch.md.Theme != nil && ch.md.Theme.Text == pal.Ink85 {
		return
	}
	th := &markdown.Theme{
		FontSize: theme.StyleProse.Size, Font: theme.FontFamilySans, CodeFont: theme.FontFamily,
		LineHeight: theme.StyleProse.Line / theme.StyleProse.Size,
		Text:       pal.Ink85, Muted: pal.Ink45, Link: pal.Ink95, Strong: pal.Ink95,
		Code: pal.Ink90, CodeBackground: pal.Hover,
		QuoteBar: pal.Hairline, QuoteText: pal.Ink62, Rule: pal.Hairline,
		CodeBlockBackground: pal.Hover, CodeBlockBorder: pal.Hairline,
		TableBorder: pal.Hairline, TableHeader: pal.Hover, Mark: pal.Selected, Checkbox: pal.Ink45,
	}
	for i := range th.Headings {
		th.Headings[i] = markdown.HeadingStyle{Size: 1.15 - 0.03*float32(i), Weight: 500, Color: pal.Ink95, Top: 1, Bottom: 0.4}
	}
	ch.md.Theme = th
}
