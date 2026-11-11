package chatview

import (
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

// fakeSlash is a SuggestSource with a fixed list; reads counts the reads.
type fakeSlash struct {
	items []Suggestion
	err   error
	reads int
}

func (f *fakeSlash) Suggestions(addr string) func() ([]Suggestion, error) {
	return func() ([]Suggestion, error) { f.reads++; return f.items, f.err }
}

func slashSession(t *testing.T) (*sess, *fakeSlash) {
	s := newSession(t, "idle")
	f := &fakeSlash{items: []Suggestion{{"clear", "Clear the history"}, {"compact", "Keep a summary"},
		{"contr:orchestrate", "Lead workers"}, {"review", "Review a PR"}}}
	s.ch.Slash = f
	return s, f
}

func TestSlashQuery(t *testing.T) {
	for text, want := range map[string]bool{"/": true, "/co": true, "co": false, "/co x": false, "/a\nb": false, "": false} {
		if _, ok := SlashQuery(text); ok != want {
			t.Errorf("SlashQuery(%q) = %v, want %v", text, ok, want)
		}
	}
}

func TestFilterPutsPrefixMatchesFirst(t *testing.T) {
	items := []Suggestion{{Name: "contr:orchestrate"}, {Name: "Orchestra"}, {Name: "clear"}}
	var names []string
	for _, it := range Filter(items, "orch") {
		names = append(names, it.Name)
	}
	if want := []string{"Orchestra", "contr:orchestrate"}; !reflect.DeepEqual(names, want) {
		t.Errorf("Filter = %v, want %v", names, want)
	}
	if len(Filter(items, "")) != 3 {
		t.Error("an empty query keeps all items")
	}
}

func TestSlashOpensFiltersAndTabInserts(t *testing.T) {
	s, f := slashSession(t)
	if s.has(LabelSuggest) {
		t.Fatal("the popup shows with no slash")
	}
	s.typeIn("/co")
	if !s.has(LabelSuggest) || !s.has("/compact") || !s.has("/contr:orchestrate") || s.has("/clear") {
		t.Fatal("the popup does not show the matches of /co")
	}
	s.key(0, ui.KeyDown)
	s.key(0, ui.KeyDown)
	s.key(0, ui.KeyUp)
	s.key(0, ui.KeyTab)
	if s.draft() != "/contr:orchestrate " || s.has(LabelSuggest) {
		t.Fatalf("after Tab: draft %q, popup %v", s.draft(), s.has(LabelSuggest))
	}
	if len(s.sent) != 0 {
		t.Errorf("Tab sent %q", s.sent)
	}
	*s.ch.draft(term) = ""
	s.tt.Frame()
	s.typeIn("/r")
	if f.reads != 1 {
		t.Errorf("reads = %d, want the list kept after one read", f.reads)
	}
}

func TestSlashEnterInsertsAndDoesNotSend(t *testing.T) {
	s, _ := slashSession(t)
	s.typeIn("/rev")
	s.key(0, ui.KeyEnter)
	if s.draft() != "/review " || len(s.sent) != 0 {
		t.Fatalf("draft %q, sent %q", s.draft(), s.sent)
	}
	// With the popup closed, Enter sends again.
	s.typeIn("12")
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 1 || s.sent[0] != "/review 12" {
		t.Fatalf("sent %q", s.sent)
	}
}

func TestSlashEscapeClosesUntilTheTextChanges(t *testing.T) {
	s, _ := slashSession(t)
	s.ch.Interrupt = func(string) error { t.Error("Esc of the popup interrupts"); return nil }
	s.typeIn("/c")
	s.key(0, ui.KeyEscape)
	if s.has(LabelSuggest) {
		t.Fatal("Esc does not close the popup")
	}
	s.typeIn("l")
	if !s.has(LabelSuggest) {
		t.Fatal("typing does not open the popup again")
	}
}

func TestSlashEmptyStates(t *testing.T) {
	s, f := slashSession(t)
	s.typeIn("/zzz")
	if !s.has(TextNoMatch) {
		t.Error("no match shows no empty text")
	}
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 1 || s.sent[0] != "/zzz" {
		t.Errorf("Enter with no match sent %q, want the text as typed", s.sent)
	}

	s, f = slashSession(t)
	f.items, f.err = nil, errors.New("read failed")
	s.typeIn("/")
	if !s.has("read failed") {
		t.Error("a failed read shows no error")
	}
}

func TestSlashWithoutSourceOpensNothing(t *testing.T) {
	s := newSession(t, "idle")
	s.typeIn("/c")
	if s.has(LabelSuggest) {
		t.Error("the popup shows with no source")
	}
}

func TestSlashClickInsertsAndFocusesTheField(t *testing.T) {
	s, _ := slashSession(t)
	s.typeIn("/")
	if err := s.tt.Click("/review"); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if s.draft() != "/review " || !s.tt.Focused(LabelComposer) || s.has(LabelSuggest) {
		t.Fatalf("after a click: draft %q, focused %v", s.draft(), s.tt.Focused(LabelComposer))
	}
}
