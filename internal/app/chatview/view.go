package chatview

import (
	"time"

	"github.com/egoist/mygo/ui"

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
	// LabelPrompt, LabelGroup, LabelOwnerNote, LabelNote and LabelCall name the rows and lines.
	LabelPrompt = "owner prompt"
	// LabelPending names a prompt sent from the composer that the transcript has not brought yet.
	LabelPending   = "pending prompt"
	LabelGroup     = "work group"
	LabelOwnerNote = "note for the owner"
	LabelNote      = "worker note"
	LabelCall      = "tool call"
	// LabelFade names the gradient over the bottom of the transcript, above the dock.
	LabelFade = "transcript fade"
	// TextTask names a background task's line, before its status and summary.
	TextTask = "Background task"
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
	// Solid: the float-solid fill and the float-line line keep the terminal text under it from
	// showing through; the chosen segment keeps its selected fill.
	pal := theme.Current()
	box := ui.Box(c).Label(LabelSwitchBack).Bind(&ch.switchBox).Shrink(0).Radius(theme.SegmentRadius).Background(pal.FloatSolid)
	box.Children(func() {
		kit.Switch(c, LabelSwitch, &choice, TextChat, TextTerm).Tooltip(LabelSwitch + " " + shortcut.Of(shortcut.Chat).Glyphs())
		// The switch has no icon: the chip is centered on it, inside it (KeyHint, D248).
		kit.KeyHint(c, shortcut.Of(shortcut.Chat).HintIf(ch.Hints), box, ui.Element{})
		// The line is drawn over the box and takes no room, so the switch keeps its size and place.
		ui.Box(c).Absolute().Fill().Radius(theme.SegmentRadius).Border(1, pal.FloatLine).PassThrough()
	})
	if on := choice == 0; on != ch.On(id) {
		ch.SetOn(id, on)
	}
}

// View builds the chat of the terminal that Sync follows: the transcript in a list that follows
// its end, with the pending prompts at its end, the live line or the team row, and the composer.
func (ch *Chat) View(c *ui.Context) {
	ch.show()
	ch.svc = c.Services()
	ch.ticks = false
	pal := theme.Current()
	ch.mdTheme(pal)
	id := ch.src.Terminal()
	st := ch.state(id)
	// The end of a turn settles the pending prompts (D265).
	if ch.wasWorking && st.kind == liveIdle {
		ch.model.TurnEnded()
	}
	ch.wasWorking = st.kind == liveWorking
	ui.Column(c).Label(LabelChat).Fill().Bind(&ch.panel).Draw(func(_ *ui.Painter, r ui.Rect) { ch.keepRect(&ch.panelRect, r) }).Children(func() {
		rows, pending := ch.model.Rows(), ch.model.Pendings()
		sc := ch.scrollState()
		// The list has room under its last entry as tall as the fade over its bottom edge, so the
		// fade covers no entry and the dock cuts none (D259).
		// The switch row is room above the list, not in it: the rows in view (Visible) are those
		// under it, and the pinned prompt takes its place while the chat is scrolled up (D266).
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(topSpace, 0, 0, 0).
			Draw(func(_ *ui.Painter, r ui.Rect) { ch.keepRect(&ch.listRect, r) }).Children(func() {
			ui.List(c, &ch.list, len(rows)+len(pending), func(i int) {
				if i < len(rows) {
					ch.row(c, rows[i])
					return
				}
				p := pending[i-len(rows)]
				column(c.Key(p.ID), func() {
					ch.prompt(c, p.Text, true)
					ch.images(c, nil, p.Images)
				})
			}).
				Grow(1).MinHeight(0).Gap(rowGap).Padding(0, theme.Space20, fadeHeight).Justify(ui.End).
				Children(func() {
					if len(rows)+len(pending) == 0 {
						ui.Box(c).Fill().Center().Children(func() {
							kit.Styled(ui.Text(c, TextEmpty), ch.ts(theme.StyleBody)).TextColor(pal.Ink48)
						})
					}
				})
			// The fade ends on the stage as it shows (StageBase, D250): the translucent stage
			// itself, drawn again over the panel, would show as a band.
			stage := pal.StageBase()
			ui.Box(c).Label(LabelFade).Absolute().Left(0).Right(0).Bottom(0).Height(fadeHeight).PassThrough().
				LinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: stage.Alpha(0), At: 0}, {Color: stage, At: 1}}})
			if sc.pin >= 0 {
				ch.pinned(c, sc.pin)
			}
			if sc.jump {
				ch.jump(c, sc.news)
			}
		})
		// The dock is the composer card, with the team row or the live line on top (D271).
		ui.Column(c).FillWidth().Shrink(0).Padding(0, theme.Space20, theme.Space14).Children(func() {
			ch.composer(c, id, st)
		})
		ch.bigImage(c)
	})
}

// column centers a row's content in the content column.
func column(c *ui.Context, fn func()) {
	ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).MaxWidth(columnMax).Children(fn)
	})
}

// row builds row r. The time shows once per turn, under its prompt (D266).
func (ch *Chat) row(c *ui.Context, r Row) {
	c = c.Key(r.ID)
	switch r.Kind {
	case RowPrompt:
		if r.Sent() {
			column(c, func() { ch.prompt(c, r.Text, false) })
			return
		}
		e := ch.model.Entry(r.Entry)
		column(c, func() {
			ch.prompt(c, e.Text, false)
			ch.images(c, e.Pics, e.Images)
			ch.sentAt(c, e.Time)
		})
	case RowSaid:
		e := ch.model.Entry(r.Entry)
		ch.said(c, r.ID, e.Text, e.Time)
	case RowGroup:
		ch.group(c, r)
	case RowOwnerNote:
		column(c, func() { ch.noteLine(c, ch.model.Entry(r.Entry), true) })
	}
}

// sentAt is the line with the time a prompt was sent, at the end of the column. A time that is
// missing or does not parse shows nothing. It is a fixed text: nothing ticks (D55).
func (ch *Chat) sentAt(c *ui.Context, at string) {
	t, err := time.Parse(time.RFC3339Nano, at)
	if at == "" || err != nil {
		return
	}
	kit.Styled(ui.Text(c, TimeText(t, c.Now())), ch.ts(theme.StyleMeta)).NoWrap().AlignSelf(ui.End).
		Margin(theme.Space4, 0, 0, 0).TextColor(theme.Current().Ink48)
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
// dimmed. A prompt of images alone has no bubble.
func (ch *Chat) prompt(c *ui.Context, text string, pending bool) {
	if text == "" {
		return
	}
	pal := theme.Current()
	label, opacity := LabelPrompt, float32(1)
	if pending {
		label, opacity = LabelPending, pendingOpacity
	}
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		ui.Box(c).Label(label).MaxWidth(promptMax).Padding(theme.Space8, 13).Radius(theme.Radius12).
			Background(pal.Hover).Opacity(opacity).Children(func() {
			kit.Styled(ui.Text(c, text), ch.ts(theme.StyleProse)).TextColor(pal.Ink100).Selectable()
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
	notes, tasks := ch.model.Counts(r)
	text := GroupText(notes, tasks, len(r.Calls))
	ui.Column(c).FillWidth().Children(func() { ch.groupBody(c, r, open, text) })
}

// groupBody is the fold row in the column and, open, the card in a wide box.
func (ch *Chat) groupBody(c *ui.Context, r Row, open *bool, text string) {
	pal := theme.Current()
	p := ui.CollapsibleBase(c, open)
	ink := pal.Ink48
	if p.Trigger.Hovered() {
		ink = pal.Ink64
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
		kit.Styled(ui.Text(c, text), ch.ts(theme.StyleValue)).NoWrap().TextColor(ink)
		for _, addr := range ch.model.Senders(r) {
			ch.dot(c, addr)
		}
		chev := "›"
		if *open {
			chev = "⌄"
		}
		kit.Styled(ui.Text(c, chev), ch.ts(theme.StyleValue)).TextColor(ink).AccessHidden(true)
	})
}

// dot is the state of the worker at addr: blocked in ink-100, working in ink-88, else ink-30. A
// blocked worker gets no red: the team row's "N needs you" is the chat's one red for it (D259).
func (ch *Chat) dot(c *ui.Context, addr string) {
	pal := theme.Current()
	color, state := pal.Ink30, "idle"
	if ch.Worker != nil {
		if w, ok := ch.Worker(addr); ok {
			state = w.Status
			switch w.Status {
			case "blocked":
				color = pal.Ink100
			case "working":
				color = pal.Ink88
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

// noteLine is one kit note: the sender's dot and name, and its text on one line. A background
// task shows "Background task", its status and its summary. A failed task outside the fold (own)
// has no dot, its name and status in ink-100 and its text in ink-88: no red (D259).
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
	label, nameInk, statusInk, textInk := LabelNote, pal.Ink88, pal.Ink48, pal.Ink48
	if own {
		label, nameInk, statusInk, textInk = LabelOwnerNote, pal.Ink100, pal.Ink100, pal.Ink88
	}
	ui.Row(c).Label(label + " " + name).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
		if addr != "" {
			ch.dot(c, addr)
		}
		kit.Styled(ui.Text(c, name), ch.ts(theme.StyleMeta)).FontWeight(500).NoWrap().Shrink(0).TextColor(nameInk)
		if status != "" {
			kit.Styled(ui.Text(c, status), ch.ts(theme.StyleMeta)).NoWrap().Shrink(0).TextColor(statusInk)
		}
		kit.Styled(ui.Text(c, text), ch.ts(theme.StyleMeta)).SingleLine().Grow(1).MinWidth(0).TextColor(textInk)
	})
}

// callLine is one tool call: the working glyph while it runs, its name, its summary, an error
// mark and how long it took.
func (ch *Chat) callLine(c *ui.Context, e transcript.Entry) {
	pal := theme.Current()
	call := e.Tool
	name := ToolName(call)
	ui.Row(c).Label(LabelCall + " " + name).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
		if !call.Done {
			kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true})
		}
		kit.Styled(ui.Text(c, name), ch.ts(theme.StyleMeta)).NoWrap().MinWidth(nameWidth).Shrink(0).TextColor(pal.Ink64)
		kit.Styled(ui.Text(c, call.Summary), ch.ts(theme.StyleMeta)).SingleLine().Grow(1).MinWidth(0).TextColor(pal.Ink48)
		if call.Error {
			kit.Styled(ui.Text(c, TextError), ch.ts(theme.StyleMeta)).NoWrap().Shrink(0).TextColor(pal.Blocked)
		}
		if d, ok := Duration(e); ok {
			kit.Styled(ui.Text(c, DurationText(d)), ch.ts(theme.StyleMeta)).NoWrap().Shrink(0).TextColor(pal.Ink48)
		}
	})
}
