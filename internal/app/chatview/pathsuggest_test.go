package chatview

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
)

// fakePaths is a PathSource with a fixed list; dirs holds the folders read.
type fakePaths struct {
	items []Suggestion
	dirs  []string
}

func (f *fakePaths) Paths(addr, dir string) func() ([]Suggestion, error) {
	return func() ([]Suggestion, error) { f.dirs = append(f.dirs, dir); return f.items, nil }
}

func pathSession(t *testing.T) (*sess, *fakePaths) {
	s := newSession(t, "idle")
	f := &fakePaths{items: []Suggestion{{Name: "cmd/"}, {Name: "cmd/main.go"}, {Name: "docs/"},
		{Name: "docs/spec/"}, {Name: "docs/spec/ChatView.md"}, {Name: "README.md"}}}
	s.ch.Path = f
	return s, f
}

func TestQueryAtCaret(t *testing.T) {
	for _, c := range []struct {
		text  string
		caret int
		ok    bool
		q     Query
	}{
		{"@", 1, true, Query{Mark: '@', Text: "", Start: 0, Caret: 1}},
		{"see @do", 7, true, Query{Mark: '@', Text: "do", Start: 4, Caret: 7}},
		{"see @docs/sp and x", 12, true, Query{Mark: '@', Text: "docs/sp", Start: 4, Caret: 12}},
		{"see @do x", 9, false, Query{}},
		{"a@b", 3, false, Query{}},
		{"plain", 5, false, Query{}},
		{"/co", 3, true, Query{Mark: '/', Text: "co", Caret: 3}},
		{"/co", 1, false, Query{}},
	} {
		q, ok := QueryAt(c.text, c.caret)
		if ok != c.ok || q != c.q {
			t.Errorf("QueryAt(%q, %d) = %+v, %v; want %+v, %v", c.text, c.caret, q, ok, c.q, c.ok)
		}
	}
}

func TestFilterPathsOrderAndCap(t *testing.T) {
	items := []Suggestion{{Name: "internal/review/tree.go"}, {Name: "docs/spec/ChatView.md"}, {Name: "spec.md"}, {Name: "x/inspect.go"}}
	var names []string
	for _, it := range FilterPaths(items, "spec") {
		names = append(names, it.Name)
	}
	if want := []string{"docs/spec/ChatView.md", "spec.md", "x/inspect.go"}; !reflect.DeepEqual(names, want) {
		t.Errorf("FilterPaths = %v, want %v", names, want)
	}
	var many []Suggestion
	for i := range maxPaths + 10 {
		many = append(many, Suggestion{Name: "f" + strconv.Itoa(i)})
	}
	if n := len(FilterPaths(many, "")); n != maxPaths {
		t.Errorf("FilterPaths kept %d, want %d", n, maxPaths)
	}
}

func TestAtInsertsAFile(t *testing.T) {
	s, _ := pathSession(t)
	s.typeIn("see @main")
	if !s.has(LabelSuggest) || !s.has("@cmd/main.go") || s.has("@README.md") {
		t.Fatal("the popup does not show the match of @main")
	}
	s.key(0, ui.KeyTab)
	if s.draft() != "see @cmd/main.go " || s.has(LabelSuggest) {
		t.Fatalf("after Tab: draft %q, popup %v", s.draft(), s.has(LabelSuggest))
	}
}

func TestAtInsertsAFolderAndStaysOpen(t *testing.T) {
	s, f := pathSession(t)
	s.typeIn("@sp")
	s.key(0, ui.KeyEnter)
	if s.draft() != "@docs/spec/" || len(s.sent) != 0 {
		t.Fatalf("after Enter: draft %q, sent %q", s.draft(), s.sent)
	}
	if !s.has(LabelSuggest) || !s.has("@docs/spec/ChatView.md") {
		t.Fatal("the popup does not stay open for the folder")
	}
	if want := []string{"", "docs/spec/"}; !reflect.DeepEqual(f.dirs, want) {
		t.Errorf("folders read = %q, want %q", f.dirs, want)
	}
	s.key(0, ui.KeyEscape)
	if s.has(LabelSuggest) {
		t.Error("Esc does not close the popup")
	}
}

func TestInsertKeepsTheTextAfterTheCaret(t *testing.T) {
	ch := &Chat{}
	text := "look at @do then"
	q, ok := QueryAt(text, 11)
	if !ok {
		t.Fatal("no query")
	}
	if got := ch.insert(text, q, Suggestion{Name: "docs/a.md"}); got != "look at @docs/a.md  then" || ch.sg.caret != 19 {
		t.Errorf("insert = %q, caret %d", got, ch.sg.caret)
	}
}

func TestAtWithNoMatchSaysSo(t *testing.T) {
	s, _ := pathSession(t)
	s.typeIn("@zzz")
	if !s.has(LabelSuggest) || !s.has(TextNoPath) {
		t.Error("the popup does not say that no file matches")
	}
}
