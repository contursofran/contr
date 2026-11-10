package chatview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/session"
)

// redrawRig is a chat with an Env that counts what asks for a frame: Window.Update (Post) and
// Invalidate. Quiet posts count apart: they ask for none.
type redrawRig struct {
	ch                     *Chat
	path                   string
	notify                 map[string]func()
	posts, redraws, quiets int
}

func newRedrawRig(t *testing.T) *redrawRig {
	t.Helper()
	r := &redrawRig{path: filepath.Join(t.TempDir(), "s.jsonl"), notify: map[string]func(){}}
	env := view.Env{
		Run:        view.Runner{Go: func(fn func()) { fn() }, Post: func(fn func()) { r.posts++; fn() }},
		Invalidate: func() { r.redraws++ },
		Quiet:      func(fn func()) { r.quiets++; fn() },
	}
	dir := t.TempDir()
	r.ch = New(env, dir, func(p string, n func()) (func(), error) { r.notify[p] = n; return func() {}, nil })
	if err := session.Update(dir, term, func(s *session.Session) (bool, error) {
		return s.SetTranscript("s", r.path), nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

// add appends a line to the transcript and fires its watch.
func (r *redrawRig) add(t *testing.T, line string) {
	t.Helper()
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	r.notify[r.path]()
}

func (r *redrawRig) frames() int { return r.posts + r.redraws }

// A working session writes its transcript many times a second. While the terminal shows, a
// change asks for no frame and reads nothing; in Chat mode each change reads once and asks for
// a frame after its batch is applied (#660, D257).
func TestTranscriptChangeRedrawsOnlyWhileTheChatShows(t *testing.T) {
	r := newRedrawRig(t)
	r.ch.Sync(term)
	if r.redraws == 0 {
		t.Fatal("a new path asks for no frame: the switch would not show")
	}
	r.posts, r.redraws = 0, 0
	for range 10 {
		r.add(t, lPrompt)
	}
	if r.frames() != 0 || r.quiets != 10 || r.ch.model.Len() != 0 {
		t.Fatalf("Terminal mode, 10 writes: %d frames asked, %d quiet posts, %d entries read; want 0, 10, 0",
			r.frames(), r.quiets, r.ch.model.Len())
	}

	r.ch.SetOn(term, true)
	r.ch.show()
	if r.ch.model.Len() != 10 {
		t.Fatalf("the chat did not read what changed while hidden: %d entries", r.ch.model.Len())
	}
	r.posts, r.redraws = 0, 0
	r.add(t, lSaid2)
	if r.ch.model.Len() != 11 || r.redraws != 1 || r.posts != 1 {
		t.Errorf("Chat mode, 1 write: %d entries, %d redraws, %d posts; want 11, 1, 1", r.ch.model.Len(), r.redraws, r.posts)
	}

	r.ch.Hide()
	r.posts, r.redraws = 0, 0
	r.add(t, lSaid2)
	if r.frames() != 0 || r.ch.model.Len() != 11 {
		t.Errorf("hidden again, 1 write: %d frames asked, %d entries; want 0, 11", r.frames(), r.ch.model.Len())
	}
}
