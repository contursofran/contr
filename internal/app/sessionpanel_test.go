package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/session"
)

var sessionNow = time.Unix(1_800_000_000, 0)

// newSessionTester shows a panel for terminal "t1" with the session s
// written under a temp state dir (nil writes no file).
func newSessionTester(t *testing.T, s *session.Session) (*SessionPanel, *ui.Tester, *[]string) {
	t.Helper()
	dir := t.TempDir()
	if s != nil {
		if err := session.Write(dir, "t1", s); err != nil {
			t.Fatal(err)
		}
	}
	var opened []string
	p := &SessionPanel{StateDir: dir, Terminal: func() string { return "t1" },
		Env:   view.Env{OpenURL: func(u string) { opened = append(opened, u) }},
		Watch: func(string, func()) (func(), error) { return func() {}, nil },
		Now:   func() time.Time { return sessionNow }}
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Width(280).Children(func() { p.View(c) })
	}, 300, 600)
	tt.Frame()
	return p, tt, &opened
}

func ago(d time.Duration) uint64 { return uint64(sessionNow.Add(-d).Unix()) }

func TestSessionTodoRowStates(t *testing.T) {
	_, tt, _ := newSessionTester(t, &session.Session{Todos: []session.Todo{
		{Text: "Merge #453", Status: session.Completed},
		{Text: "Design the panel", Active: "Designing the panel", Status: session.InProgress, You: true},
		{Text: "Merge #460", Status: session.Pending},
	}})
	for _, label := range []string{LabelTodos, "Merge #453, done",
		"Designing the panel, in progress", "Merge #460, pending", "working"} {
		box(t, tt, label)
	}
	if !tt.HasText("1 of 3") {
		t.Errorf("no count 1 of 3: %q", tt.Texts())
	}
	// In progress shows the active text, not the text.
	if !tt.HasText("Designing the panel") || tt.HasText("Design the panel") {
		t.Errorf("in progress text: %q", tt.Texts())
	}
	// Only the todo from the owner's prompt has the tag.
	if n := count(tt.Texts(), LabelYouTag); n != 1 {
		t.Errorf("%d you tags, want 1: %q", n, tt.Texts())
	}
	if !absent(tt, LabelArtifacts) {
		t.Error("the empty Artifacts section shows")
	}
}

func count(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

func TestSessionTodoTextClips(t *testing.T) {
	long := "a todo text that is much too long to fit in the files panel at any width"
	_, tt, _ := newSessionTester(t, &session.Session{Todos: []session.Todo{{Text: long, Status: session.Pending}}})
	row := box(t, tt, long+", pending")
	if row.W > 280 {
		t.Errorf("row %v is wider than the panel", row)
	}
}

func TestSessionArtifactRows(t *testing.T) {
	_, tt, opened := newSessionTester(t, &session.Session{Artifacts: []session.Artifact{
		{Slug: "canvas", Title: "contr · from-zero canvas", URL: "https://claude.ai/artifact/aaa", UpdatedAt: ago(10 * time.Second)},
		{Slug: "design", Title: "contr design system", URL: "https://claude.ai/artifact/bbb", UpdatedAt: ago(38 * time.Minute)},
		{Slug: "native", Title: "Native · final", URL: "https://claude.ai/artifact/ccc", UpdatedAt: ago(50 * time.Hour)},
	}})
	for _, s := range []string{"now", "38m", "2d", "3", "contr design system"} {
		if !tt.HasText(s) {
			t.Errorf("no text %q: %q", s, tt.Texts())
		}
	}
	if !absent(tt, LabelTodos) {
		t.Error("the empty Todos section shows")
	}
	tt.Click("Open contr design system")
	if !slices.Equal(*opened, []string{"https://claude.ai/artifact/bbb"}) {
		t.Errorf("opened %q", *opened)
	}
}

func TestSessionArtifactsDedupeByClaudeID(t *testing.T) {
	got := DedupeArtifacts([]session.Artifact{
		{Slug: "https://claude.ai/artifact/abc", UpdatedAt: 3},
		{Slug: "abc", Title: "Board", UpdatedAt: 2},
		{Slug: "other", Title: "Other", URL: "https://claude.ai/artifact/xyz/", UpdatedAt: 1},
		{Slug: "xyz", Title: "Same as other"},
	})
	if len(got) != 2 {
		t.Fatalf("%d rows, want 2: %+v", len(got), got)
	}
	if got[0].Title != "Board" || artifactURL(got[0]) != "https://claude.ai/artifact/abc" || got[0].UpdatedAt != 3 {
		t.Errorf("first row %+v", got[0])
	}
	if got[1].Title != "Other" {
		t.Errorf("second row %+v", got[1])
	}
}

func TestSessionArtifactAge(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "now", 5 * time.Minute: "5m", 3 * time.Hour: "3h", 49 * time.Hour: "2d", -time.Hour: ""} {
		if got := ArtifactAge(sessionNow, ago(d)); got != want {
			t.Errorf("age of %v = %q, want %q", d, got, want)
		}
	}
	if ArtifactAge(sessionNow, 0) != "" {
		t.Error("zero time has an age")
	}
}

func TestSessionMissingFileShowsNothing(t *testing.T) {
	_, tt, _ := newSessionTester(t, nil)
	if !absent(tt, LabelTodos) || !absent(tt, LabelArtifacts) {
		t.Errorf("sections show with no file: %q", tt.Texts())
	}
}

func TestSessionBadFileShowsNothing(t *testing.T) {
	dir := t.TempDir()
	path := session.Path(dir, "t1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &SessionPanel{StateDir: dir, Terminal: func() string { return "t1" },
		Watch: func(string, func()) (func(), error) { return nil, errors.New("no watch") }}
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c); p.View(c) }, 300, 400)
	tt.Frame()
	if !absent(tt, LabelTodos) || !absent(tt, LabelArtifacts) {
		t.Errorf("sections show for a bad file: %q", tt.Texts())
	}
}

// A watch event reads the file again; a new terminal reads its own file.
func TestSessionFollowsTerminalAndWatch(t *testing.T) {
	dir := t.TempDir()
	id := "t1"
	var notify func()
	stops := 0
	p := &SessionPanel{StateDir: dir, Terminal: func() string { return id },
		Watch: func(_ string, n func()) (func(), error) { notify = n; return func() { stops++ }, nil }}
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c); p.View(c) }, 300, 400)
	tt.Frame()
	if !absent(tt, LabelTodos) {
		t.Fatal("todos before the file exists")
	}
	if err := session.Write(dir, "t1", &session.Session{Todos: []session.Todo{{Text: "one", Status: session.Pending}}}); err != nil {
		t.Fatal(err)
	}
	notify()
	tt.Frame()
	box(t, tt, "one, pending")
	if err := session.Write(dir, "t2", &session.Session{Todos: []session.Todo{{Text: "two", Status: session.Pending}}}); err != nil {
		t.Fatal(err)
	}
	id = "t2"
	tt.Frame()
	box(t, tt, "two, pending")
	if stops != 1 || !absent(tt, "one, pending") {
		t.Errorf("stops %d, texts %q", stops, tt.Texts())
	}
	id = ""
	tt.Frame()
	if !absent(tt, LabelTodos) {
		t.Error("todos with no terminal")
	}
}

func TestFilesPanelShowsSessionAboveTheTree(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	dir := t.TempDir()
	if err := session.Write(dir, "t1", &session.Session{Todos: []session.Todo{{Text: "one", Status: session.Pending}}}); err != nil {
		t.Fatal(err)
	}
	f.Session.StateDir = dir
	f.Session.Terminal = func() string { return "t1" }
	f.Session.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	tt.Frame()
	todos, head := box(t, tt, LabelTodos), box(t, tt, LabelPanelFiles+" name")
	if todos.Y >= head.Y {
		t.Errorf("todos at %v, files head at %v: want the todos above", todos, head)
	}
}

// A watch event of the terminal shown before starts no read.
func TestSessionOldWatchEventReadsNothing(t *testing.T) {
	var h apptest.HeldEnv
	id := "t1"
	var notify func()
	p := &SessionPanel{StateDir: t.TempDir(), Terminal: func() string { return id }, Env: h.Env(),
		Watch: func(_ string, n func()) (func(), error) { notify = n; return func() {}, nil }}
	p.sync()
	h.RunAll()
	old := notify
	id = "t2"
	p.sync()
	h.RunAll()
	old()
	for _, f := range h.Done {
		f()
	}
	h.Done = nil
	if len(h.Work) != 0 {
		t.Fatalf("%d reads started by an old watch event", len(h.Work))
	}
	notify()
	h.Done[0]()
	if len(h.Work) != 1 {
		t.Fatalf("%d reads started by a watch event, want 1", len(h.Work))
	}
}
