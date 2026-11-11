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
	// LabelStop names the Stop button, which shows while the agent works.
	LabelStop = "Stop"
	// TextAnswerFirst shows when Enter comes while the session waits on a menu: the text is kept,
	// so a stray Enter never picks a choice (D51). The notch answers the menu (D260).
	TextAnswerFirst = "Answer in the notch first (⌘J), or switch to Terminal."
)

// Sizes of the composer.
const (
	composerMinHeight = 44
	composerLines     = 8 // the field grows to this many lines, then scrolls
)

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

// composer builds the field of terminal id with, at its right, the Stop button while the agent
// works and the model and context; the last failure shows under it. The field takes the focus as
// it appears: when the chat shows.
func (ch *Chat) composer(c *ui.Context, id string, st liveState) {
	pal := theme.Current()
	draft := ch.draft(id)
	column(c, func() {
		ui.Row(c).FillWidth().MinHeight(composerMinHeight).AlignItems(ui.Center).Gap(theme.Space12).
			Padding(0, 14).Radius(theme.Radius12).Border(1, pal.InputLine).Background(pal.Hover).Children(func() {
			in := ui.TextAreaBase(c.Key("composer:"+id), draft).Label(LabelComposer).Placeholder(Placeholder(ch.name(id))).
				PlaceholderColor(pal.Ink48).TextColor(pal.Ink100).FontSize(theme.StyleProse.Size).
				Lines(1, composerLines).Grow(1).MinWidth(0).PaddingY(theme.Space12).AutoFocus().Bind(&ch.field).
				OnTextKey(func(k ui.TextKey) bool { return ch.textKey(id, st, k) })
			if in.Changed() {
				delete(ch.errs, id)
			}
			if st.kind == liveWorking && ch.Interrupt != nil {
				kit.CardButton(c, LabelStop, kit.ButtonOutlined, pal.Ink88, func() { ch.stop(id); ch.field.Focus() })
			}
			if ch.Meta != nil {
				if m := ch.Meta(id); m != "" {
					kit.Styled(ui.Text(c, m), theme.StyleValue).NoWrap().Shrink(0).TextColor(pal.Ink48)
				}
			}
		})
		if e := ch.errs[id]; e != "" {
			kit.Styled(ui.Text(c, e), theme.StyleSmall).Wrap().
				Padding(theme.Space6, 14, 0).TextColor(pal.Ink64)
		}
	})
}

// textKey is a key of the field that types no text. Enter sends; ⇧Enter goes on to the field,
// which breaks the line. Esc interrupts the agent while it works and does nothing else: it closes
// and switches nothing.
func (ch *Chat) textKey(id string, st liveState, k ui.TextKey) bool {
	switch {
	case k.Key == ui.KeyEnter && k.Mods == 0:
		text := k.Text()
		if strings.TrimSpace(text) == "" || ch.sending || ch.Send == nil {
			return true
		}
		if st.kind == liveNeedsYou {
			ch.errs[id] = TextAnswerFirst
			ch.Env.Redraw()
			return true
		}
		k.SetText("")
		ch.send(id, text)
		return true
	case k.Key == ui.KeyEscape && k.Mods == 0:
		if st.kind == liveWorking {
			ch.stop(id)
		}
		return true
	}
	return false
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
