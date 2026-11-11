package chatview

// The suggestion popup of the composer (D273): "/" at the start of the field opens the session's
// slash commands and skills above the card, filtered as the owner types. Up and Down move, Enter
// or Tab puts "/name " in the field, Esc closes. The list comes from a SuggestSource, so the
// path popup of "@" can use the same popup with another source.

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels and texts of the popup.
const (
	// LabelSuggest names the popup panel.
	LabelSuggest = "suggestions"
	// TextNoMatch shows when no item matches what was typed.
	TextNoMatch = "No commands match"
	// TextLoading shows while the list is read.
	TextLoading = "Loading…"
)

// Sizes of the popup.
const (
	suggestWidth = 440
	suggestRows  = 8  // the list scrolls past this many rows
	suggestPadY  = 12 // the room above and below the texts of a row
	suggestGap   = theme.Space6
)

// Suggestion is an item of the popup: the name it inserts and a one-line description.
type Suggestion struct {
	Name, Detail string
}

// SuggestSource gives the items of the popup for the session at addr. Suggestions runs on the UI
// thread and returns the read, which blocks: it runs off the UI thread.
type SuggestSource interface {
	Suggestions(addr string) func() ([]Suggestion, error)
}

// suggest is the popup's state for the composer.
type suggest struct {
	// items and errs hold the read list and its failure for each terminal; loading is set while a
	// read runs.
	items   map[string][]Suggestion
	errs    map[string]string
	loading map[string]bool
	// sel is the selected row; closed is the text Esc closed the popup on.
	sel    int
	closed string
	open   bool
	list   ui.ListState
	// caret is where the field's caret goes as it builds after an insert, 0 for no move.
	caret int
}

// SlashQuery is the text after a "/" that starts text, with ok false when text is not a command
// being typed: it does not start with "/" or holds a space or a line break.
func SlashQuery(text string) (string, bool) {
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \t\n") {
		return "", false
	}
	return text[1:], true
}

// Filter gives the items whose names hold query, without regard to case: names that start with it
// first, each group in the order of items.
func Filter(items []Suggestion, query string) []Suggestion {
	q := strings.ToLower(query)
	var starts, holds []Suggestion
	for _, it := range items {
		n := strings.ToLower(it.Name)
		switch {
		case strings.HasPrefix(n, q):
			starts = append(starts, it)
		case strings.Contains(n, q):
			holds = append(holds, it)
		}
	}
	return append(starts, holds...)
}

// suggestOpen tells whether the popup shows for text in terminal id: a command is typed, a source
// is set and Esc did not close it on this text.
func (ch *Chat) suggestOpen(text string) bool {
	_, ok := SlashQuery(text)
	return ok && ch.Slash != nil && ch.sg.closed != text
}

// suggestions are the items of terminal id that match text. The first call reads the list off
// the UI thread.
func (ch *Chat) suggestions(id, text string) []Suggestion {
	q, _ := SlashQuery(text)
	items, ok := ch.sg.items[id]
	if !ok && !ch.sg.loading[id] && ch.Slash != nil {
		ch.sg.loading[id] = true
		read := ch.Slash.Suggestions(id)
		ch.Env.Do(func() func() {
			items, err := read()
			return func() {
				delete(ch.sg.loading, id)
				ch.sg.items[id] = items
				if err != nil {
					ch.sg.errs[id] = err.Error()
				}
				ch.Env.Redraw()
			}
		})
	}
	return Filter(items, q)
}

// suggestKey takes a key of the field while the popup shows: Up and Down move, Enter and Tab put
// the selected item in the field, Esc closes. It returns false for other keys.
func (ch *Chat) suggestKey(id string, k ui.TextKey) bool {
	text := k.Text()
	if k.Mods != 0 || !ch.suggestOpen(text) {
		return false
	}
	list := ch.suggestions(id, text)
	switch k.Key {
	case ui.KeyUp, ui.KeyDown:
		if len(list) > 0 {
			d := 1
			if k.Key == ui.KeyUp {
				d = len(list) - 1
			}
			ch.sg.sel = (ch.sg.sel + d) % len(list)
			ch.sg.list.ScrollIntoView(ch.sg.sel)
		}
		return true
	case ui.KeyEnter, ui.KeyTab:
		if ch.sg.sel >= len(list) {
			return k.Key == ui.KeyTab // Enter with no match sends the text as typed
		}
		k.SetText(ch.insert(list[ch.sg.sel]))
		return true
	case ui.KeyEscape:
		ch.sg.closed = text
		return true
	}
	return false
}

// insert is the field's text that picks it, "/name ", with the caret set to go after it.
func (ch *Chat) insert(it Suggestion) string {
	text := "/" + it.Name + " "
	ch.sg.sel, ch.sg.caret = 0, utf8.RuneCountInString(text)
	return text
}

// suggestPopup builds the popup above anchor, the card, while it shows for the draft of terminal
// id: a row for each match, name in mono and the description in ink-48, the selected one on the
// hover ground; else one quiet line that says why it is empty. Its texts follow the Chat text
// size (D272).
func (ch *Chat) suggestPopup(c *ui.Context, id string, anchor ui.Element) {
	text := *ch.draft(id)
	ch.sg.open = ch.suggestOpen(text)
	if !ch.sg.open {
		ch.sg.sel = 0
		return
	}
	list := ch.suggestions(id, text)
	if ch.sg.sel >= len(list) {
		ch.sg.sel = 0
	}
	pal := theme.Current()
	name, detail := ch.ts(theme.StyleValue), ch.ts(theme.StyleSmall)
	rowH := max(name.Line, detail.Line) + suggestPadY
	p := ui.PopoverBase(c, anchor, &ch.sg.open, func(panel ui.Element) {
		panel = panel.AttachTo(anchor, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, suggestGap, 0).
			Label(LabelSuggest).Column().Width(suggestWidth).Padding(theme.Space4).Radius(theme.Radius12).
			Background(pal.FloatSolid).Border(1, pal.FloatLine)
		for _, l := range pal.ShadowFloat {
			panel = panel.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
		panel.Children(func() {
			if len(list) == 0 {
				empty := TextNoMatch
				switch {
				case ch.sg.loading[id]:
					empty = TextLoading
				case ch.sg.errs[id] != "":
					empty = ch.sg.errs[id]
				}
				kit.Styled(ui.Text(c, empty), detail).SingleLine().Padding(theme.Space6, theme.Space8).TextColor(pal.Ink48)
				return
			}
			h := float32(min(len(list), suggestRows)) * rowH
			ui.List(c, &ch.sg.list, len(list), func(i int) {
				it := list[i]
				row := ui.ButtonBase(c).Label("/" + it.Name).Row().FillWidth().Height(rowH).AlignItems(ui.Center).Justify(ui.Start).
					Gap(theme.Space8).PaddingX(theme.Space8).Radius(theme.Radius8).FocusRing(false).
					OnClick(func() {
						*ch.draft(id) = ch.insert(it)
						ch.field.Focus()
					})
				if i == ch.sg.sel {
					row = row.Background(pal.HoverStrong)
				}
				row.Children(func() {
					kit.Styled(ui.Text(c, "/"+it.Name), name).NoWrap().Shrink(0).TextColor(pal.Ink100)
					kit.Styled(ui.Text(c, it.Detail), detail).SingleLine().Ellipsis("…").MinWidth(0).TextColor(pal.Ink48)
				})
			}).FillWidth().Height(h)
		})
	})
	if p.Dismissed() {
		ch.sg.closed = text
	}
}
