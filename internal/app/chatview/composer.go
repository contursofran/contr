package chatview

// The composer (docs/spec/ChatView.md, D252): a field under the transcript that types the owner's
// text into the session's own terminal and presses Enter, as the other sends of the app do.

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels and texts of the composer.
const (
	// LabelComposer names the field; its placeholder is "Message <name>…".
	LabelComposer = "Message"
	// LabelCard names the card: the field and the button (D271, D276).
	LabelCard = "composer"
	// LabelFooter names the line under the card: the status at its left, the model at its right
	// (D276).
	LabelFooter = "composer footer"
	// TextKeys is the footer's left text while the agent does not work.
	TextKeys = "⏎ send · ⇧⏎ new line"
	// LabelSend names the round button while the field has text.
	LabelSend = "Send"
	// LabelStop names the round button while the agent works and the field is empty.
	LabelStop = "Stop"
	// TipSend and TipStop are the tooltips of the round button: its label and its key.
	TipSend = "Send  ↩"
	TipStop = "Stop  esc"
	// TextAnswerFirst shows when Enter comes while the session waits on a menu: the text is kept,
	// so a stray Enter never picks a choice (D51). The notch answers the menu (D260).
	TextAnswerFirst = "Answer in the notch first (⌘J), or switch to Terminal."
	// LabelAttached names the chip of an image pasted in the composer: "attached Image 1".
	LabelAttached = "attached"
)

// The keys that the composer presses in the terminal (D269).
const (
	// keyPasteImage is Ctrl+V: Claude Code reads the clipboard's image and writes "[Image #N]".
	keyPasteImage = 0x16
	keyEnter      = '\r'
)

// Sizes of the composer card (D271, D276).
const (
	composerLines = 8  // the field grows to this many lines, then scrolls
	actionSize    = 30 // the round button
	cardPadY      = theme.Space8
	cardPadLeft   = theme.Space8
	cardPadRight  = theme.Space8
	footerHeight  = 18            // the line under the card
	footerInset   = theme.Space16 // its side inset
	// fieldGap is the room between the image chips and the field.
	fieldGap = theme.Space2
)

// Action is what the round button of the composer does.
type Action int

// The actions of the round button.
const (
	// ActionIdle is no button: nothing to send and nothing to stop (D276).
	ActionIdle Action = iota
	// ActionSend sends the text, also while the agent works: the message queues (D265).
	ActionSend
	// ActionStop interrupts the agent: it works and the field is empty.
	ActionStop
)

// ActionOf is the round button's action: Send with text, Stop while the agent works and the
// field is empty (when it can be stopped), else no button.
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

// composerText is the field's text: 14px on 22px lines (D276).
var composerText = theme.TextStyle{Size: 14, Line: 22, Weight: 400}

// composer builds the card of terminal id (D271, D276). Above the card, outside it, the team row
// while the session orchestrates. The card holds the field that grows with its text and, in line with its last
// line, the round Send or Stop button. Under the card, outside it, the footer: the status at the
// left, the model and context at the right. The last failure shows under the footer. The field
// takes the focus as it appears: when the chat shows.
func (ch *Chat) composer(c *ui.Context, id string, st liveState) {
	pal := theme.Current()
	draft := ch.draft(id)
	ct := ch.ts(composerText) // the field follows the Chat text size (D272)
	column(c, func() {
		// While the session orchestrates, the team row sits above the card, outside it (D276).
		if t, ok := ch.teamOf(id); ok {
			ch.team(c, t)
		}
		card := ui.Column(c).Label(LabelCard).FillWidth().Padding(cardPadY, cardPadRight, cardPadY, cardPadLeft).
			Radius(theme.Radius12).Border(1, pal.InputLine).Background(pal.Hover).Children(func() {
			// The chips of the images pasted since the last send, above the field
			// (D269).
			if n := ch.attached[id]; n > 0 {
				ui.Row(c).FillWidth().Wrap().Gap(theme.Space6).Padding(theme.Space6, 0, theme.Space2, 0).Children(func() {
					for i := range n {
						ch.imageChip(c, "Image "+strconv.Itoa(i+1), LabelAttached)
					}
				})
			}
			svc := c.Services()
			field := ui.Row(c).FillWidth().AlignItems(ui.End).Gap(theme.Space8)
			if ch.attached[id] > 0 {
				field = field.Margin(fieldGap, 0, 0, 0)
			}
			field.Children(func() {
				hint := Placeholder(ch.name(id))
				if ch.next = ""; *draft == "" && ch.NextPrompt != nil {
					// Claude Code's suggested next prompt takes the place of the placeholder (D281).
					if ch.next = ch.NextPrompt(id); ch.next != "" {
						hint = ch.next
					}
				}
				in := ui.TextAreaBase(c.Key("composer:"+id), draft).Label(LabelComposer).Placeholder(hint).
					PlaceholderColor(pal.Ink48).TextColor(pal.Ink100).FontSize(ct.Size).LineHeight(ct.Line/ct.Size).
					Lines(1, composerLines).Grow(1).MinWidth(0).Padding((actionSize-ct.Line)/2, 0, (actionSize-ct.Line)/2, theme.Space6).AutoFocus().Bind(&ch.field).
					OnTextKey(func(k ui.TextKey) bool { return ch.textKey(id, st, k) }).
					OnPaste(func(text string) bool { return ch.paste(id, svc, text) })
				if n := ch.sg.caret; n > 0 {
					// After an insert of the popup, the caret goes to the end of it (D273).
					in.SetTextSelection(n, n)
					ch.sg.caret = 0
				}
				if in.Changed() {
					delete(ch.errs, id)
				}
				a := ActionOf(*draft, st.kind == liveWorking, ch.Interrupt != nil)
				if ch.attached[id] > 0 {
					// Pasted images are something to send (D269).
					a = ActionSend
				}
				if a == ActionIdle {
					// No button: the field keeps the height of one.
					ui.Box(c).Size(0, actionSize).Shrink(0)
				} else {
					ch.action(c, id, st, a)
				}
			})
		})
		ch.suggestPopup(c, id, card)
		if paths := card.DroppedFiles(); len(paths) > 0 {
			ch.drop(draft, paths)
			ch.field.Focus()
		}
		ch.footer(c, id, st)
		if e := ch.errs[id]; e != "" {
			kit.Styled(ui.Text(c, e), ch.ts(theme.StyleSmall)).Wrap().
				Padding(theme.Space6, footerInset, 0).TextColor(pal.Ink64)
		}
	})
}

// footer is the line under the card (D276), mono meta in ink-48: at the left the working glyph and
// "Working 7s · Bash · gh pr diff 633" while the agent works, which asks for a redraw each second
// (D55), else the keys; at the right the model and context, which never clips: the left text
// clips first.
func (ch *Chat) footer(c *ui.Context, id string, st liveState) {
	pal := theme.Current()
	ui.Row(c).Label(LabelFooter).FillWidth().Height(footerHeight).Padding(0, footerInset).AlignItems(ui.Center).
		Gap(theme.Space8).Children(func() {
		left := ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(theme.Space6)
		if st.kind == liveWorking {
			text := TextWorking
			what, start, ok := ch.model.Activity()
			if ok {
				text += " " + ElapsedText(c.Now().Sub(start))
				ch.ticks = true
				c.After(time.Second)
			}
			if what != "" && what != TextWorking {
				text += " · " + what
			}
			left.Label(LabelLive + " " + text).Children(func() {
				kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true})
				kit.Styled(ui.Text(c, text), ch.ts(theme.StyleMeta)).SingleLine().Ellipsis("…").MinWidth(0).TextColor(pal.Ink48)
			})
		} else {
			left.Children(func() {
				kit.Styled(ui.Text(c, TextKeys), ch.ts(theme.StyleMeta)).SingleLine().MinWidth(0).TextColor(pal.Ink48)
			})
		}
		if ch.Meta != nil {
			if m := ch.Meta(id); m != "" {
				kit.Styled(ui.Text(c, m), ch.ts(theme.StyleMeta)).NoWrap().Shrink(0).TextColor(pal.Ink48)
			}
		}
	})
}

// action is the round button of the card: Send in ink-100 with a ground arrow, the one strong
// thing of the card; Stop as a quiet 1px ring with a 9px square. A click gives the focus back to
// the field.
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
	}
	focus := b.FocusVisible()
	b.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if a == ActionStop {
			const s = 9
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

// textKey is a key of the field that types no text. Tab or Right in an empty field takes the
// suggested next prompt (D281). Enter sends; ⇧Enter goes on to the field,
// which breaks the line. While the suggestion popup shows, it takes its keys first (D273). Esc interrupts the agent while it works and does nothing else: it closes
// and switches nothing.
func (ch *Chat) textKey(id string, st liveState, k ui.TextKey) bool {
	if ch.suggestKey(id, k) {
		return true
	}
	if (k.Key == ui.KeyTab || k.Key == ui.KeyRight) && k.Mods == 0 && ch.next != "" && k.Text() == "" {
		// Tab or Right takes the suggested next prompt into the empty field (D281).
		k.SetText(ch.next)
		ch.sg.caret = utf8.RuneCountInString(ch.next)
		ch.next = ""
		return true
	}
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
// field. A blank text with no pasted images, a send that runs or no Send does nothing. While the session waits on a
// menu, the text stays and the error says to answer the menu first.
func (ch *Chat) submit(id string, st liveState, text string, clear func()) {
	if strings.TrimSpace(text) == "" && ch.attached[id] == 0 || ch.sending || ch.Send == nil {
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

// send types text into terminal id off the UI thread, with the images pasted since the last send.
// The text shows at once as a pending prompt; a failure drops it, puts the text back in an empty
// field, keeps the images and shows the error. Images alone press Enter: Claude Code's field holds
// their marks (D269).
func (ch *Chat) send(id, text string) {
	ch.sending = true
	delete(ch.errs, id)
	images := ch.attached[id]
	delete(ch.attached, id)
	pending := ch.model.AddPendingImages(text, images)
	ch.Env.Redraw()
	ch.Env.Do(func() func() {
		var err error
		if strings.TrimSpace(text) == "" {
			err = ch.Keys(id, []byte{keyEnter})
		} else {
			err = ch.Send(id, text)
		}
		return func() {
			ch.sending = false
			if err != nil {
				ch.attached[id] += images
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

// paste takes a paste of the composer of terminal id that brings no text while the clipboard
// holds an image: it presses Ctrl+V in the terminal off the UI thread, and Claude Code attaches the
// image itself. The composer then shows a chip for it until the send; a failure shows under the
// field. A paste with text, or with no image, goes on to the field (D269).
func (ch *Chat) paste(id string, svc ui.Services, text string) bool {
	if text != "" || ch.Keys == nil || !slices.Contains(svc.ClipboardFormats(), transfer.PNG) {
		return false
	}
	if ch.sending {
		return true
	}
	ch.sending = true
	delete(ch.errs, id)
	ch.Env.Do(func() func() {
		err := ch.Keys(id, []byte{keyPasteImage})
		return func() {
			ch.sending = false
			if err != nil {
				ch.errs[id] = err.Error()
			} else {
				ch.attached[id]++
			}
			ch.Env.Redraw()
		}
	})
	return true
}

// drop adds the paths of files dropped on the composer to the draft, each escaped as a terminal
// pastes a dropped file. The send pastes them into the session, where Claude Code takes the path
// of an image as an image (D269).
func (ch *Chat) drop(draft *string, paths []string) {
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		parts = append(parts, ShellEscape(p))
	}
	text := strings.Join(parts, " ")
	if d := *draft; d != "" && !strings.HasSuffix(d, " ") && !strings.HasSuffix(d, "\n") {
		text = " " + text
	}
	*draft += text
	ch.Env.Redraw()
}

// ShellEscape puts a backslash before each byte of path that a shell reads as more than itself, as
// a terminal does with the path of a dropped file.
func ShellEscape(path string) string {
	var b strings.Builder
	for _, r := range path {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+,:@%=~", r) || r > 0x7f) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
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
