package chatview

// The composer (docs/spec/ChatView.md, D252): a field under the transcript that types the owner's
// text into the session's own terminal and presses Enter, as the other sends of the app do.

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/transfer"
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
	// LabelAttached names the chip of an image pasted in the composer: "attached Image 1".
	LabelAttached = "attached"
)

// The keys that the composer presses in the terminal (D269).
const (
	// keyPasteImage is Ctrl+V: Claude Code reads the clipboard's image and writes "[Image #N]".
	keyPasteImage = 0x16
	keyEnter      = '\r'
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
		box := ui.Row(c).FillWidth().MinHeight(composerMinHeight).AlignItems(ui.Center).Gap(theme.Space12).
			Padding(0, 14).Radius(theme.Radius12).Border(1, pal.InputLine).Background(pal.Hover).Children(func() {
			for i := range ch.attached[id] {
				imageChip(c, "Image "+strconv.Itoa(i+1), LabelAttached)
			}
			svc := c.Services()
			in := ui.TextAreaBase(c.Key("composer:"+id), draft).Label(LabelComposer).Placeholder(Placeholder(ch.name(id))).
				PlaceholderColor(pal.Ink48).TextColor(pal.Ink100).FontSize(theme.StyleProse.Size).
				Lines(1, composerLines).Grow(1).MinWidth(0).PaddingY(theme.Space12).AutoFocus().Bind(&ch.field).
				OnTextKey(func(k ui.TextKey) bool { return ch.textKey(id, st, k) }).
				OnPaste(func(text string) bool { return ch.paste(id, svc, text) })
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
		if paths := box.DroppedFiles(); len(paths) > 0 {
			ch.drop(draft, paths)
			ch.field.Focus()
		}
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
		images := ch.attached[id]
		if strings.TrimSpace(text) == "" && images == 0 || ch.sending || ch.Send == nil {
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
