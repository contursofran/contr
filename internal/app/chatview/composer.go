package chatview

// The composer (docs/spec/ChatView.md, D252): a field under the transcript that types the owner's
// text into the session's own terminal and presses Enter, as the other sends of the app do.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels and texts of the composer.
const (
	// LabelComposer names the field; its placeholder is "Message <name>…".
	LabelComposer = "Message"
	// LabelCard names the card: the top line, the field, the button and the foot (D271).
	LabelCard = "composer"
	// LabelSend names the round button while it sends: with text, or dimmed while idle and empty.
	LabelSend = "Send"
	// LabelStop names the round button while the agent works and the field is empty.
	LabelStop = "Stop"
	// TipSend and TipStop are the tooltips of the round button: its label and its key.
	TipSend = "Send  ↩"
	TipStop = "Stop  esc"
	// TextAnswerFirst shows when Enter comes while the session waits on a menu: the text is kept,
	// so a stray Enter never picks a choice (D51). The notch answers the menu (D260).
	TextAnswerFirst = "Answer in the notch first (⌘J), or switch to Terminal."
)

// Sizes of the composer card (D271).
const (
	composerLines = 8  // the field grows to this many lines, then scrolls
	actionSize    = 28 // the round button
	cardPadY      = theme.Space6
	cardPadLeft   = theme.Space14
	cardPadRight  = theme.Space8 // the button sits closer to the edge than the text
	// cardInset is the room the card takes from the column's width: its border and paddings.
	cardInset = 2 + cardPadLeft + cardPadRight
	// fieldGap is the room between the top line and the field.
	fieldGap = theme.Space2
)

// Action is what the round button of the composer does.
type Action int

// The actions of the round button.
const (
	// ActionIdle is Send, dimmed: nothing to send and nothing to stop.
	ActionIdle Action = iota
	// ActionSend sends the text, also while the agent works: the message queues (D265).
	ActionSend
	// ActionStop interrupts the agent: it works and the field is empty.
	ActionStop
)

// ActionOf is the round button's action: Send with text, Stop while the agent works and the
// field is empty (when it can be stopped), else Send dimmed.
func ActionOf(text string, working, canStop bool) Action {
	switch {
	case strings.TrimSpace(text) != "":
		return ActionSend
	case working && canStop:
		return ActionStop
	}
	return ActionIdle
}

// Placeholder is the field's text while it is empty: "Message lead…".
func Placeholder(name string) string {
	if name == "" {
		return LabelComposer + "…"
	}
	return LabelComposer + " " + name + "…"
}

// draft is the text being typed for terminal id.
func (ch *Chat) draft(id string) *string {
	d := ch.drafts[id]
	if d == nil {
		d = new(string)
		ch.drafts[id] = d
	}
	return d
}

// composer builds the card of terminal id (D271): one surface with, on top, the team row or the
// live line when there is one; the field that grows with its text and, in line with its last line,
// the round Send or Stop button; under it the model and context. The last failure shows under the
// card. The field takes the focus as it appears: when the chat shows.
func (ch *Chat) composer(c *ui.Context, id string, st liveState) {
	pal := theme.Current()
	draft := ch.draft(id)
	column(c, func() {
		ui.Column(c).Label(LabelCard).FillWidth().Padding(cardPadY, cardPadRight, cardPadY, cardPadLeft).
			Radius(theme.Radius12).Border(1, pal.InputLine).Background(pal.Hover).Children(func() {
			// While the session orchestrates, the team row takes the live line's place and carries
			// its text, so the card does not change height when work starts and stops.
			top := false
			if t, ok := ch.teamOf(id); ok {
				ch.team(c, t, st)
				top = true
			} else {
				top = ch.live(c, st)
			}
			// Attachment chips go here, between the top line and the field.
			field := ui.Row(c).FillWidth().AlignItems(ui.End).Gap(theme.Space8)
			if top {
				field = field.Margin(fieldGap, 0, 0, 0)
			}
			field.Children(func() {
				in := ui.TextAreaBase(c.Key("composer:"+id), draft).Label(LabelComposer).Placeholder(Placeholder(ch.name(id))).
					PlaceholderColor(pal.Ink48).TextColor(pal.Ink100).FontSize(theme.StyleProse.Size).LineHeight(theme.StyleProse.Line/theme.StyleProse.Size).
					Lines(1, composerLines).Grow(1).MinWidth(0).PaddingY((actionSize - theme.StyleProse.Line) / 2).AutoFocus().Bind(&ch.field).
					OnTextKey(func(k ui.TextKey) bool { return ch.textKey(id, st, k) })
				if in.Changed() {
					delete(ch.errs, id)
				}
				ch.action(c, id, st, ActionOf(*draft, st.kind == liveWorking, ch.Interrupt != nil))
			})
			if ch.Meta != nil {
				if m := ch.Meta(id); m != "" {
					kit.Styled(ui.Text(c, m), theme.StyleMeta).SingleLine().MinWidth(0).TextColor(pal.Ink48)
				}
			}
		})
		if e := ch.errs[id]; e != "" {
			kit.Styled(ui.Text(c, e), theme.StyleSmall).Wrap().
				Padding(theme.Space6, cardPadLeft, 0).TextColor(pal.Ink64)
		}
	})
}

// action is the round button of the card: Send in ink-100 with a ground arrow, the one strong
// thing of the card; Stop as a quiet outline with a square; Send dimmed as an outline with a dim
// arrow, which takes no click. A click gives the focus back to the field.
func (ch *Chat) action(c *ui.Context, id string, st liveState, a Action) {
	pal := theme.Current()
	label, tip := LabelSend, TipSend
	if a == ActionStop {
		label, tip = LabelStop, TipStop
	}
	b := ui.ButtonBase(c).Label(label).Size(actionSize, actionSize).Shrink(0).Radius(actionSize / 2).
		FocusRing(false).Tooltip(tip)
	mark := pal.Ground
	switch a {
	case ActionSend:
		b = b.Background(pal.Ink100).Cursor(ui.CursorPointer).OnClick(func() {
			ch.submit(id, st, *ch.draft(id), func() { *ch.draft(id) = "" })
			ch.field.Focus()
		})
	case ActionStop:
		mark = pal.Ink88
		b = b.Border(1, pal.InputLine).Cursor(ui.CursorPointer).OnClick(func() { ch.stop(id); ch.field.Focus() })
		if b.Hovered() {
			b = b.Background(pal.HoverStrong)
		}
	default:
		mark = pal.Ink30
		b = b.Border(1, pal.InputLine).Disabled(true)
	}
	focus := b.FocusVisible()
	b.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if a == ActionStop {
			const s = 8
			p.Fill(ui.Rect{X: cx - s/2, Y: cy - s/2, W: s, H: s}, mark, 1.5)
		} else {
			arrow := &ui.Path{}
			arrow.MoveTo(cx, cy+5).LineTo(cx, cy-5)
			arrow.MoveTo(cx-4.5, cy-0.5).LineTo(cx, cy-5).LineTo(cx+4.5, cy-0.5)
			p.StrokePath(arrow, 1.6, mark)
		}
		if focus {
			// 1px ring, 2px outside the button, as IconButton has it.
			const gap, width = 2, 1
			p.Stroke(ui.Rect{X: r.X - gap - width, Y: r.Y - gap - width, W: r.W + 2*(gap+width), H: r.H + 2*(gap+width)},
				pal.FocusRing, r.W/2+gap+width, width)
		}
	})
}

// textKey is a key of the field that types no text. Enter sends; ⇧Enter goes on to the field,
// which breaks the line. Esc interrupts the agent while it works and does nothing else: it closes
// and switches nothing.
func (ch *Chat) textKey(id string, st liveState, k ui.TextKey) bool {
	switch {
	case k.Key == ui.KeyEnter && k.Mods == 0:
		ch.submit(id, st, k.Text(), func() { k.SetText("") })
		return true
	case k.Key == ui.KeyEscape && k.Mods == 0:
		if st.kind == liveWorking {
			ch.stop(id)
		}
		return true
	}
	return false
}

// submit sends text from the field of terminal id, for Enter and for Send: clear empties the
// field. A blank text, a send that runs or no Send does nothing. While the session waits on a
// menu, the text stays and the error says to answer the menu first.
func (ch *Chat) submit(id string, st liveState, text string, clear func()) {
	if strings.TrimSpace(text) == "" || ch.sending || ch.Send == nil {
		return
	}
	if st.kind == liveNeedsYou {
		ch.errs[id] = TextAnswerFirst
		ch.Env.Redraw()
		return
	}
	clear()
	ch.send(id, text)
}

// send types text into terminal id off the UI thread. The text shows at once as a pending prompt;
// a failure drops it, puts the text back in an empty field and shows the error.
func (ch *Chat) send(id, text string) {
	ch.sending = true
	delete(ch.errs, id)
	pending := ch.model.AddPending(text)
	ch.Env.Redraw()
	ch.Env.Do(func() func() {
		err := ch.Send(id, text)
		return func() {
			ch.sending = false
			if err != nil {
				ch.model.DropPending(pending)
				if d := ch.draft(id); *d == "" {
					*d = text
				}
				ch.errs[id] = err.Error()
			}
			ch.Env.Redraw()
		}
	})
}

// stop interrupts the agent of terminal id off the UI thread; a failure shows under the field.
func (ch *Chat) stop(id string) {
	if ch.Interrupt == nil || ch.sending {
		return
	}
	ch.sending = true
	ch.Env.Do(func() func() {
		err := ch.Interrupt(id)
		return func() {
			ch.sending = false
			if err != nil {
				ch.errs[id] = err.Error()
			}
			ch.Env.Redraw()
		}
	})
}

// name is the name of the session at terminal id, "" when the agent is not known.
func (ch *Chat) name(id string) string {
	if ch.Worker != nil {
		if w, ok := ch.Worker(id); ok {
			return w.DisplayName()
		}
	}
	return ""
}
