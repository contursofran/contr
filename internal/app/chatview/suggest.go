package chatview

// The suggestion popup of the composer (D273, D279): "/" at the start of the field opens the
// session's slash commands and skills above the card, and "@" at the caret (at the start or after
// a space) opens the files and folders of its working folder, filtered as the owner types. Up and
// Down move, Enter or Tab puts "/name " or "@path " in the field, Esc closes. A folder goes in as
// "@dir/" and the popup stays open for its content.

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
	// TextNoPath shows when no file or folder matches what was typed after "@".
	TextNoPath = "No files match"
	// TextLoading shows while the list is read.
	TextLoading = "Loading…"
)

// Sizes of the popup.
const (
	suggestWidth = 440
	suggestRows  = 8  // the list scrolls past this many rows
	suggestPadY  = 12 // the room above and below the texts of a row
	suggestGap   = theme.Space6
	// maxPaths is the most paths the popup shows for "@".
	maxPaths = 50
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

// PathSource gives the files and folders of the working folder of the session at addr for "@".
// Names are slash-separated and relative to that folder; a folder ends in "/". dir is the folder
// typed so far ("" or ending in "/"), for a source that reads one folder at a time. Paths runs on
// the UI thread and returns the read, which runs off it.
type PathSource interface {
	Paths(addr, dir string) func() ([]Suggestion, error)
}

// Query is what is typed at the caret for the popup: Mark is '/' or '@', Text is what follows it
// up to the caret, and Start and Caret are rune offsets of the mark and the caret.
type Query struct {
	Mark         rune
	Text         string
	Start, Caret int
}

// QueryAt finds the query at caret, a rune offset in text: a "/" command (see SlashQuery) with the
// caret at its end, or an "@" at the start or after a space with no space between it and the
// caret. ok is false when no query is typed there.
func QueryAt(text string, caret int) (Query, bool) {
	r := []rune(text)
	caret = min(max(caret, 0), len(r))
	if q, ok := SlashQuery(text); ok && caret == len(r) {
		return Query{Mark: '/', Text: q, Caret: caret}, true
	}
	i := caret
	for i > 0 && !strings.ContainsRune(" \t\n", r[i-1]) {
		i--
	}
	if i == caret || r[i] != '@' {
		return Query{}, false
	}
	return Query{Mark: '@', Text: string(r[i+1 : caret]), Start: i, Caret: caret}, true
}

// FilterPaths gives at most maxPaths items whose names hold query, without regard to case: the
// paths with a part that starts with it first, each group in the order of items.
func FilterPaths(items []Suggestion, query string) []Suggestion {
	q := strings.ToLower(query)
	var starts, holds []Suggestion
	for _, it := range items {
		n := strings.ToLower(it.Name)
		switch {
		case strings.HasPrefix(n, q) || strings.Contains(n, "/"+q):
			starts = append(starts, it)
		case strings.Contains(n, q):
			holds = append(holds, it)
		}
	}
	out := append(starts, holds...)
	return out[:min(len(out), maxPaths)]
}

// pathDir is the folder part of a path query: "" or the text up to its last "/".
func pathDir(q string) string {
	return q[:strings.LastIndex(q, "/")+1]
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
	// caret is where the field's caret goes as it builds after an insert, 0 for no move; at is
	// where the caret was as the field last built.
	caret, at int
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

// query is the query at the caret of text, with ok false when the popup does not show for it:
// nothing is typed, its source is not set or Esc closed it on this text.
func (ch *Chat) query(text string) (Query, bool) {
	q, ok := QueryAt(text, ch.sg.at)
	if !ok || ch.sg.closed == text || (q.Mark == '/' && ch.Slash == nil) || (q.Mark == '@' && ch.Path == nil) {
		return Query{}, false
	}
	return q, true
}

// listKey is the key of the list of q in the popup's maps: one for "/", one for each folder of "@".
func listKey(id string, q Query) string {
	if q.Mark == '@' {
		return "@" + id + "\x00" + pathDir(q.Text)
	}
	return "/" + id
}

// suggestions are the items of terminal id that match q. The first call for a list reads it off
// the UI thread: once for "/", once for each folder typed after "@".
func (ch *Chat) suggestions(id string, q Query) []Suggestion {
	key := listKey(id, q)
	items, ok := ch.sg.items[key]
	if !ok && !ch.sg.loading[key] {
		ch.sg.loading[key] = true
		var read func() ([]Suggestion, error)
		if q.Mark == '@' {
			read = ch.Path.Paths(id, pathDir(q.Text))
		} else {
			read = ch.Slash.Suggestions(id)
		}
		ch.Env.Do(func() func() {
			items, err := read()
			return func() {
				delete(ch.sg.loading, key)
				ch.sg.items[key] = items
				if err != nil {
					ch.sg.errs[key] = err.Error()
				}
				ch.Env.Redraw()
			}
		})
	}
	if q.Mark == '@' {
		return FilterPaths(items, q.Text)
	}
	return Filter(items, q.Text)
}

// suggestKey takes a key of the field while the popup shows: Up and Down move, Enter and Tab put
// the selected item in the field, Esc closes. It returns false for other keys.
func (ch *Chat) suggestKey(id string, k ui.TextKey) bool {
	text := k.Text()
	q, ok := ch.query(text)
	if k.Mods != 0 || !ok {
		return false
	}
	list := ch.suggestions(id, q)
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
		k.SetText(ch.insert(text, q, list[ch.sg.sel]))
		return true
	case ui.KeyEscape:
		ch.sg.closed = text
		return true
	}
	return false
}

// insert is text with the query q replaced by the mark and the name of it, "/name " or "@path ",
// with the caret set to go after it. A folder, "@dir/", gets no space, so the popup stays open
// for its content.
func (ch *Chat) insert(text string, q Query, it Suggestion) string {
	r := []rune(text)
	put := string(q.Mark) + it.Name
	if !strings.HasSuffix(it.Name, "/") {
		put += " "
	}
	head := string(r[:q.Start]) + put
	ch.sg.sel, ch.sg.caret = 0, utf8.RuneCountInString(head)
	ch.sg.at = ch.sg.caret
	return head + string(r[q.Caret:])
}

// suggestPopup builds the popup above anchor, the card, while it shows for the draft of terminal
// id: a row for each match, name in mono and the description in ink-48, the selected one on the
// hover ground; else one quiet line that says why it is empty. Its texts follow the Chat text
// size (D272).
func (ch *Chat) suggestPopup(c *ui.Context, id string, anchor ui.Element) {
	text := *ch.draft(id)
	q, open := ch.query(text)
	ch.sg.open = open
	if !ch.sg.open {
		ch.sg.sel = 0
		return
	}
	list := ch.suggestions(id, q)
	key := listKey(id, q)
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
				if q.Mark == '@' {
					empty = TextNoPath
				}
				switch {
				case ch.sg.loading[key]:
					empty = TextLoading
				case ch.sg.errs[key] != "":
					empty = ch.sg.errs[key]
				}
				kit.Styled(ui.Text(c, empty), detail).SingleLine().Padding(theme.Space6, theme.Space8).TextColor(pal.Ink48)
				return
			}
			h := float32(min(len(list), suggestRows)) * rowH
			ui.List(c, &ch.sg.list, len(list), func(i int) {
				it := list[i]
				shown := string(q.Mark) + it.Name
				row := ui.ButtonBase(c).Label(shown).Row().FillWidth().Height(rowH).AlignItems(ui.Center).Justify(ui.Start).
					Gap(theme.Space8).PaddingX(theme.Space8).Radius(theme.Radius8).FocusRing(false).
					OnClick(func() {
						*ch.draft(id) = ch.insert(text, q, it)
						ch.field.Focus()
					})
				if i == ch.sg.sel {
					row = row.Background(pal.HoverStrong)
				}
				row.Children(func() {
					kit.Styled(ui.Text(c, shown), name).NoWrap().Shrink(0).TextColor(pal.Ink100)
					kit.Styled(ui.Text(c, it.Detail), detail).SingleLine().Ellipsis("…").MinWidth(0).TextColor(pal.Ink48)
				})
			}).FillWidth().Height(h)
		})
	})
	if p.Dismissed() {
		ch.sg.closed = text
	}
}
