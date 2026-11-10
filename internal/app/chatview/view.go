package chatview

import (
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
	TextChat    = "Chat"
	TextTerm    = "Terminal"
	// TextEmpty shows while the session has no transcript, or no entry in it yet.
	TextEmpty = "No transcript yet"
	// TextDock is the dock's line: the composer comes later (#634).
	TextDock = "Type in the terminal"
	// TextToTerminal is the dock's button: it switches to Terminal.
	TextToTerminal = "Switch to Terminal"
	// LabelPrompt, LabelGroup, LabelOwnerNote, LabelNote and LabelCall name the rows and lines.
	LabelPrompt    = "owner prompt"
	LabelGroup     = "work group"
	LabelOwnerNote = "note for the owner"
	LabelNote      = "worker note"
	LabelCall      = "tool call"
	// TextTask names a background task's line, before its status and summary.
	TextTask = "Background task"
	// TextError marks a call whose result is an error.
	TextError = "error"
)

// Sizes of docs/spec/ChatView.md.
const (
	columnMax  = 760 // the content column, centered
	promptMax  = 560
	groupMax   = 584
	lineHeight = 20 // a line of an open group, and the group row
	nameWidth  = 48 // the tool name column of a call line
	dotSize    = 6
	topSpace   = 44 // room for the switch row above the first row
	rowGap     = theme.Space18
	dockHeight = 44
)

// Switch builds the Chat | Terminal switch of terminal id, with its key chip (⌘E).
func (ch *Chat) Switch(c *ui.Context, id string) {
	choice := 1
	if ch.On(id) {
		choice = 0
	}
	box := ui.Box(c).Shrink(0)
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
// its end, and the dock. toTerminal switches the terminal to Terminal mode.
func (ch *Chat) View(c *ui.Context, toTerminal func()) {
	ch.show()
	pal := theme.Current()
	ch.mdTheme(pal)
	ui.Column(c).Label(LabelChat).Fill().Children(func() {
		rows := ch.model.Rows()
		ui.List(c, &ch.list, len(rows), func(i int) { ch.row(c, rows[i]) }).
			Grow(1).MinHeight(0).Gap(rowGap).Padding(topSpace, theme.Space20, theme.Space20).Justify(ui.End).
			Children(func() {
				if len(rows) == 0 {
					ui.Box(c).Fill().Center().Children(func() {
						kit.Styled(ui.Text(c, TextEmpty), theme.StyleBody).TextColor(pal.Ink42)
					})
				}
			})
		ch.dock(c, pal, toTerminal)
	})
}

// column centers a row's content in the content column.
func column(c *ui.Context, fn func()) {
	ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).MaxWidth(columnMax).Children(fn)
	})
}

func (ch *Chat) row(c *ui.Context, r Row) {
	c = c.Key(r.ID)
	column(c, func() {
		switch r.Kind {
		case RowPrompt:
			ch.prompt(c, ch.model.Entry(r.Entry))
		case RowSaid:
			markdown.View(c, ch.model.Entry(r.Entry).Text, &ch.md)
		case RowGroup:
			ch.group(c, r)
		case RowOwnerNote:
			ch.noteLine(c, ch.model.Entry(r.Entry), true)
		}
	})
}

// prompt is the owner's text, at the right, on a hover fill, in ink-100.
func (ch *Chat) prompt(c *ui.Context, e transcript.Entry) {
	pal := theme.Current()
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		ui.Box(c).Label(LabelPrompt).MaxWidth(promptMax).Padding(theme.Space8, 13).Radius(theme.Radius12).
			Background(pal.Hover).Children(func() {
			kit.Styled(ui.Text(c, e.Text), theme.StyleProse).TextColor(pal.Ink100).Selectable()
		})
	})
}

// group is a work group: a fold row with the counts, the dots of the senders and, while a call
// runs, the working glyph. Open, a card lists the notes, then the calls.
func (ch *Chat) group(c *ui.Context, r Row) {
	pal := theme.Current()
	open := ch.folds[r.ID]
	if open == nil {
		open = new(bool)
		ch.folds[r.ID] = open
	}
	notes, tasks := ch.model.Counts(r)
	text := GroupText(notes, tasks, len(r.Calls))
	p := ui.CollapsibleBase(c, open)
	ink := pal.Ink42
	if p.Trigger.Hovered() {
		ink = pal.Ink62
	}
	p.Trigger.Label(LabelGroup + " " + text).Row().AlignSelf(ui.Start).Height(lineHeight).AlignItems(ui.Center).
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
	if p.Trigger.Clicked() {
		ch.list.HoldPlace()
	}
	p.Panel(func() {
		ui.Column(c).MaxWidth(groupMax).Margin(theme.Space6, 0, 0, 0).Padding(theme.Space8, theme.Space12).
			Radius(theme.Radius9).Border(1, pal.Hairline).Children(func() {
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
// needs the owner (own) has its name in ink-100 and its text in ink-85. A background task shows
// "Background task", its status (blocked when not completed) and its summary.
func (ch *Chat) noteLine(c *ui.Context, e transcript.Entry, own bool) {
	pal := theme.Current()
	name, addr, status, text := "owner", "", "", e.Text
	if n := e.Note; n != nil {
		switch {
		case n.Kind == transcript.NoteTask:
			name, status, text = TextTask, n.Status, "· "+TaskSummary(e.Text)
		case n.Name != "":
			name, addr = n.Name, n.Addr
		case n.Kind == transcript.NoteAgent:
			name = "session"
		}
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
		if status != "" {
			kit.Styled(ui.Text(c, status), theme.StyleMeta).NoWrap().Shrink(0).TextColor(statusInk(pal, status, textInk))
		}
		kit.Styled(ui.Text(c, text), theme.StyleMeta).SingleLine().Grow(1).MinWidth(0).TextColor(textInk)
	})
}

// statusInk is the ink of a background task's status: base when it completed, else blocked.
func statusInk(pal *theme.Palette, status string, base ui.Color) ui.Color {
	if status == "completed" {
		return base
	}
	return pal.Blocked
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

// dock is the line under the transcript: the composer comes in a later slice.
func (ch *Chat) dock(c *ui.Context, pal *theme.Palette, toTerminal func()) {
	ui.Row(c).FillWidth().Height(dockHeight).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Gap(theme.Space12).
		Padding(0, theme.Space20, theme.Space14).Children(func() {
		kit.Styled(ui.Text(c, TextDock), theme.StyleBody).NoWrap().TextColor(pal.Ink42)
		kit.CardButton(c, TextToTerminal, kit.ButtonOutlined, pal.Ink85, toTerminal)
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
