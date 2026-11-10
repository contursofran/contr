package chatsource

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/session"
)

// fakeWatch records the watches and lets a test fire them.
type fakeWatch struct {
	notify  map[string]func()
	stopped []string
	// missing lists paths whose watch fails with fs.ErrNotExist.
	missing map[string]bool
}

func (f *fakeWatch) watch(path string, notify func()) (func(), error) {
	if f.missing[path] {
		return nil, &fs.PathError{Op: "watch", Path: path, Err: fs.ErrNotExist}
	}
	f.notify[path] = notify
	return func() { f.stopped = append(f.stopped, path) }, nil
}

func setPath(t *testing.T, dir, id, path string) {
	t.Helper()
	if err := session.Update(dir, id, func(s *session.Session) (bool, error) {
		return s.SetTranscript("s", path), nil
	}); err != nil {
		t.Fatal(err)
	}
}

func newFakeSource(t *testing.T) (*Source, *fakeWatch, *int) {
	t.Helper()
	f := &fakeWatch{notify: map[string]func(){}, missing: map[string]bool{}}
	n := 0
	s := &Source{StateDir: t.TempDir(), Watch: f.watch, Changed: func() { n++ }}
	return s, f, &n
}

func TestSourceFollowsTheSavedPath(t *testing.T) {
	s, f, n := newFakeSource(t)
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	s.Sync("t1")
	if s.Path() != "/x/a.jsonl" || *n != 1 || s.Rev() != 1 {
		t.Fatalf("path %q, %d changes, rev %d", s.Path(), *n, s.Rev())
	}
	sessPath := session.Path(s.StateDir, "t1")
	if f.notify[sessPath] == nil || f.notify["/x/a.jsonl"] == nil {
		t.Fatalf("watches %v", f.notify)
	}
	// The same terminal again changes nothing.
	s.Sync("t1")
	if *n != 1 {
		t.Fatalf("%d changes after the same Sync", *n)
	}
	// The transcript grows.
	f.notify["/x/a.jsonl"]()
	if *n != 2 || s.Rev() != 2 {
		t.Fatalf("%d changes after a write", *n)
	}
	// A session file change with the same path changes nothing.
	f.notify[sessPath]()
	if *n != 2 || len(f.stopped) != 0 {
		t.Fatalf("%d changes, stopped %v", *n, f.stopped)
	}
	// /clear: a new path moves the watch.
	setPath(t, s.StateDir, "t1", "/x/b.jsonl")
	f.notify[sessPath]()
	if s.Path() != "/x/b.jsonl" || *n != 3 || !slices.Equal(f.stopped, []string{"/x/a.jsonl"}) || f.notify["/x/b.jsonl"] == nil {
		t.Fatalf("path %q, %d changes, stopped %v", s.Path(), *n, f.stopped)
	}
	// A late signal of the old watch is dropped.
	f.notify["/x/a.jsonl"]()
	if *n != 3 {
		t.Fatalf("an old watch counted: %d changes", *n)
	}
}

func TestSourceStopsWhenTheTerminalGoes(t *testing.T) {
	s, f, n := newFakeSource(t)
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	setPath(t, s.StateDir, "t2", "/x/c.jsonl")
	s.Sync("t1")
	s.Sync("t2")
	if s.Path() != "/x/c.jsonl" || s.Terminal() != "t2" || !slices.Contains(f.stopped, "/x/a.jsonl") || !slices.Contains(f.stopped, session.Path(s.StateDir, "t1")) {
		t.Fatalf("path %q, stopped %v", s.Path(), f.stopped)
	}
	old := f.notify["/x/c.jsonl"]
	s.Sync("")
	if s.Path() != "" || s.Terminal() != "" || !slices.Contains(f.stopped, "/x/c.jsonl") || !slices.Contains(f.stopped, session.Path(s.StateDir, "t2")) {
		t.Fatalf("path %q, stopped %v", s.Path(), f.stopped)
	}
	before := *n
	old()
	if *n != before {
		t.Fatal("a stopped watch counted")
	}
}

func TestSourceWithoutAPath(t *testing.T) {
	s, f, n := newFakeSource(t)
	// No session file yet: no path, no transcript watch, no error.
	s.Sync("t1")
	if s.Path() != "" || *n != 0 || len(f.notify) != 1 {
		t.Fatalf("path %q, %d changes, watches %v", s.Path(), *n, f.notify)
	}
	// The first hook run saves the path.
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	f.notify[session.Path(s.StateDir, "t1")]()
	if s.Path() != "/x/a.jsonl" || *n != 1 {
		t.Fatalf("path %q, %d changes", s.Path(), *n)
	}
}

func TestSourceKeepsThePathOfABadFile(t *testing.T) {
	s, f, n := newFakeSource(t)
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	s.Sync("t1")
	sessPath := session.Path(s.StateDir, "t1")
	if err := os.WriteFile(sessPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.notify[sessPath]()
	if s.Path() != "/x/a.jsonl" || *n != 1 || len(f.stopped) != 0 {
		t.Fatalf("path %q, %d changes, stopped %v", s.Path(), *n, f.stopped)
	}
}

func TestSourceMissingFolderIsNotAnError(t *testing.T) {
	s, f, n := newFakeSource(t)
	f.missing["/x/a.jsonl"] = true
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	s.Sync("t1")
	if s.Path() != "/x/a.jsonl" || *n != 1 || f.notify["/x/a.jsonl"] != nil {
		t.Fatalf("path %q, %d changes", s.Path(), *n)
	}
	// The folder appears; the next hook run starts the watch, which counts
	// as a change: the file may have text now.
	delete(f.missing, "/x/a.jsonl")
	f.notify[session.Path(s.StateDir, "t1")]()
	if f.notify["/x/a.jsonl"] == nil || *n != 2 {
		t.Fatalf("no watch after the folder appeared: %d changes", *n)
	}
}

func TestSourceNoTerminalOrStateDir(t *testing.T) {
	s, f, _ := newFakeSource(t)
	s.Sync("")
	s.StateDir = ""
	s.Sync("t1")
	if len(f.notify) != 0 || s.Path() != "" {
		t.Fatalf("watches %v", f.notify)
	}
}

// realSource is a Source with the mygo watch. Its posts run on the test
// goroutine through pump.
type realSource struct {
	*Source
	posts   chan func()
	changes int
}

func newRealSource(t *testing.T) *realSource {
	t.Helper()
	r := &realSource{posts: make(chan func(), 64)}
	r.Source = &Source{StateDir: t.TempDir(), Changed: func() { r.changes++ }}
	r.Env = view.Env{Run: view.Runner{
		Go:   func(f func()) { f() },
		Post: func(f func()) { r.posts <- f },
	}}
	t.Cleanup(r.Close)
	return r
}

// waitChange runs poke and the posts until changes passes want, or fails.
func (r *realSource) waitChange(t *testing.T, want int, poke func()) {
	t.Helper()
	for range 50 {
		poke()
		deadline := time.After(100 * time.Millisecond)
	drain:
		for {
			select {
			case f := <-r.posts:
				f()
				if r.changes >= want {
					return
				}
			case <-deadline:
				break drain
			}
		}
	}
	t.Fatalf("%d changes, want %d", r.changes, want)
}

func appendLine(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{}\n")
	_ = f.Close()
}

func TestSourceWatchesTheFileGrow(t *testing.T) {
	r := newRealSource(t)
	tr := filepath.Join(t.TempDir(), "s1.jsonl")
	appendLine(t, tr)
	setPath(t, r.StateDir, "t1", tr)
	r.Sync("t1")
	r.waitChange(t, 1, func() {})
	if r.Path() != tr {
		t.Fatalf("path %q", r.Path())
	}
	r.waitChange(t, 2, func() { appendLine(t, tr) })

	// A resume: the hook saves a new path; the watch follows it.
	next := filepath.Join(t.TempDir(), "s2.jsonl")
	r.waitChange(t, 3, func() { setPath(t, r.StateDir, "t1", next) })
	if r.Path() != next {
		t.Fatalf("path %q, want %q", r.Path(), next)
	}
	// The new file does not exist yet: its first write is a change.
	base := r.changes
	r.waitChange(t, base+1, func() { appendLine(t, next) })
}

func TestSourceMissingFileThenMade(t *testing.T) {
	r := newRealSource(t)
	tr := filepath.Join(t.TempDir(), "s1.jsonl")
	setPath(t, r.StateDir, "t1", tr)
	r.Sync("t1")
	r.waitChange(t, 1, func() {})
	if r.Path() != tr {
		t.Fatalf("path %q", r.Path())
	}
	r.waitChange(t, 2, func() { appendLine(t, tr) })
}
