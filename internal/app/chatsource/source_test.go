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

// lastReset is the reset flag of the last change of a fake source.
var lastReset bool

func newFakeSource(t *testing.T) (*Source, *fakeWatch, *int) {
	t.Helper()
	f := &fakeWatch{notify: map[string]func(){}, missing: map[string]bool{}}
	n := 0
	s := &Source{StateDir: t.TempDir(), Watch: f.watch, Changed: func(reset bool) { n++; lastReset = reset }}
	return s, f, &n
}

func TestSourceFollowsTheSavedPath(t *testing.T) {
	s, f, n := newFakeSource(t)
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	s.Sync("t1")
	if s.Path() != "/x/a.jsonl" || *n != 1 || s.Rev() != 1 || !lastReset {
		t.Fatalf("path %q, %d changes, rev %d, reset %v", s.Path(), *n, s.Rev(), lastReset)
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
	if *n != 2 || s.Rev() != 2 || lastReset {
		t.Fatalf("%d changes after a write, reset %v", *n, lastReset)
	}
	// A session file change with the same path changes nothing.
	f.notify[sessPath]()
	if *n != 2 || len(f.stopped) != 0 {
		t.Fatalf("%d changes, stopped %v", *n, f.stopped)
	}
	// /clear: a new path moves the watch.
	setPath(t, s.StateDir, "t1", "/x/b.jsonl")
	f.notify[sessPath]()
	if s.Path() != "/x/b.jsonl" || *n != 3 || !lastReset || !slices.Equal(f.stopped, []string{"/x/a.jsonl"}) || f.notify["/x/b.jsonl"] == nil {
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
	if f.notify["/x/a.jsonl"] == nil || *n != 2 || lastReset {
		t.Fatalf("no watch after the folder appeared: %d changes, reset %v", *n, lastReset)
	}
}

func TestSourceResetsWhenTheFileGetsShorter(t *testing.T) {
	s, f, n := newFakeSource(t)
	tr := filepath.Join(t.TempDir(), "s1.jsonl")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(tr, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("{}\n{}\n")
	setPath(t, s.StateDir, "t1", tr)
	s.Sync("t1")
	steps := []struct {
		do    func()
		reset bool
	}{
		{func() { write("{}\n{}\n{}\n") }, false}, // grows
		{func() { write("{}\n") }, true},          // replaced by a shorter file
		{func() { write("{}\n{}\n") }, false},     // grows again
		{func() { _ = os.Remove(tr) }, true},      // removed
		{func() { write("{}\n") }, false},         // made again
	}
	for i, st := range steps {
		st.do()
		f.notify[tr]()
		if *n != i+2 || lastReset != st.reset {
			t.Fatalf("step %d: %d changes, reset %v, want %v", i, *n, lastReset, st.reset)
		}
	}
}

func TestSourceResetsWhenTheFirstChangeIsShorter(t *testing.T) {
	s, f, n := newFakeSource(t)
	tr := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := os.WriteFile(tr, []byte("{}\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setPath(t, s.StateDir, "t1", tr)
	s.Sync("t1")
	// The size at the start of the watch counts.
	if err := os.WriteFile(tr, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.notify[tr]()
	if *n != 2 || !lastReset {
		t.Fatalf("%d changes, reset %v", *n, lastReset)
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
	reset   bool
}

func newRealSource(t *testing.T) *realSource {
	t.Helper()
	r := &realSource{posts: make(chan func(), 64)}
	r.Source = &Source{StateDir: t.TempDir(), Changed: func(reset bool) { r.changes++; r.reset = reset }}
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
	r.waitFor(t, func() bool { return r.changes >= want }, poke)
}

// waitFor runs poke and the posts until done, or fails.
func (r *realSource) waitFor(t *testing.T, done func() bool, poke func()) {
	t.Helper()
	for range 50 {
		poke()
		deadline := time.After(100 * time.Millisecond)
	drain:
		for {
			select {
			case f := <-r.posts:
				f()
				if done() {
					return
				}
			case <-deadline:
				break drain
			}
		}
	}
	t.Fatalf("not done after %d changes, path %q", r.changes, r.Path())
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
	if r.reset {
		t.Fatal("a write that grows the file is a reset")
	}
	// A shorter file is a reset.
	r.waitChange(t, r.changes+1, func() {
		if err := os.WriteFile(tr, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if !r.reset {
		t.Fatal("a shorter file is no reset")
	}

	// A resume: the hook saves a new path; the watch follows it.
	next := filepath.Join(t.TempDir(), "s2.jsonl")
	r.waitFor(t, func() bool { return r.Path() == next }, func() { setPath(t, r.StateDir, "t1", next) })
	if r.Path() != next || !r.reset {
		t.Fatalf("path %q, want %q; reset %v", r.Path(), next, r.reset)
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

// A change of the transcript reaches the UI thread through Env.PostQuiet and asks for no redraw:
// the chat redraws once it applied a batch (#660, D257).
func TestSourceTranscriptChangeAsksForNoRedraw(t *testing.T) {
	s, f, n := newFakeSource(t)
	var quiet, posts, redraws int
	s.Env = view.Env{
		Run:        view.Runner{Go: func(fn func()) { fn() }, Post: func(fn func()) { posts++; fn() }},
		Quiet:      func(fn func()) { quiet++; fn() },
		Invalidate: func() { redraws++ },
	}
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	s.Sync("t1")
	if *n != 1 || redraws != 0 {
		t.Fatalf("Sync: %d changes, %d redraws; want 1 change and no redraw of its own", *n, redraws)
	}
	posts = 0
	for range 5 {
		f.notify["/x/a.jsonl"]()
	}
	if *n != 6 || quiet != 5 || posts != 0 || redraws != 0 {
		t.Errorf("5 writes: %d changes, %d quiet posts, %d posts (Window.Update), %d redraws; want 6, 5, 0, 0",
			*n, quiet, posts, redraws)
	}
}

// queueEnv is an Env whose work and posts wait in one queue until run: the test is the UI thread.
type queueEnv struct{ q []func() }

func (e *queueEnv) env() view.Env {
	add := func(fn func()) { e.q = append(e.q, fn) }
	return view.Env{Run: view.Runner{Go: add, Post: add}}
}

// run runs the queue, and what it adds, until it is empty or done.
func (e *queueEnv) run(done func() bool) {
	for len(e.q) > 0 && !done() {
		fn := e.q[0]
		e.q = e.q[1:]
		fn()
	}
}

// A watch start lists the folder of the file, which takes milliseconds for a folder of many
// sessions: the watches start off the UI thread (#660). A watch that starts after its terminal
// or its path went is stopped.
func TestSourceStartsTheWatchesOffTheUIThread(t *testing.T) {
	s, f, n := newFakeSource(t)
	q := &queueEnv{}
	s.Env = q.env()
	setPath(t, s.StateDir, "t1", "/x/a.jsonl")
	setPath(t, s.StateDir, "t2", "/x/c.jsonl")
	s.Sync("t1")
	if len(f.notify) != 0 {
		t.Fatalf("Sync started a watch on the UI thread: %v", f.notify)
	}
	q.run(func() bool { return false })
	if f.notify[session.Path(s.StateDir, "t1")] == nil || f.notify["/x/a.jsonl"] == nil || s.Path() != "/x/a.jsonl" || *n != 1 {
		t.Fatalf("after the work: watches %v, path %q, %d changes", f.notify, s.Path(), *n)
	}

	// /clear: the path moves once the new watch started, with the reset, so that no read takes
	// the new file at the old offset.
	setPath(t, s.StateDir, "t1", "/x/b.jsonl")
	f.notify[session.Path(s.StateDir, "t1")]()
	q.run(func() bool { return f.notify["/x/b.jsonl"] != nil })
	if s.Path() != "/x/a.jsonl" || *n != 1 {
		t.Fatalf("while the watch starts: path %q, %d changes; want the old path, no change", s.Path(), *n)
	}
	q.run(func() bool { return false })
	if s.Path() != "/x/b.jsonl" || *n != 2 || !lastReset {
		t.Fatalf("after the start: path %q, %d changes, reset %v", s.Path(), *n, lastReset)
	}

	// The terminal goes before its session watch started.
	s.Sync("t2")
	s.Sync("")
	q.run(func() bool { return false })
	if !slices.Contains(f.stopped, session.Path(s.StateDir, "t2")) || s.Path() != "" || *n != 2 {
		t.Fatalf("a late session watch: stopped %v, path %q, %d changes", f.stopped, s.Path(), *n)
	}

	// The terminal goes before its transcript watch started.
	s.Sync("t2")
	q.run(func() bool { return f.notify["/x/c.jsonl"] != nil })
	s.Sync("")
	q.run(func() bool { return false })
	if !slices.Contains(f.stopped, "/x/c.jsonl") || s.Path() != "" || *n != 2 {
		t.Fatalf("a late transcript watch: stopped %v, path %q, %d changes", f.stopped, s.Path(), *n)
	}
}
