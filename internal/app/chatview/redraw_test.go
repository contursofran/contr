package chatview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
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

// The first read builds its model on the read goroutine (#660) while the composer may add a
// pending prompt on the UI thread: the new model keeps the pending prompts and their ids, and
// drops one whose prompt the read brought. Run with -race.
func TestFirstReadKeepsThePendingPrompts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(lPrompt+"\n"+lSaid2+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	posts := make(chan func(), 16)
	ch := New(view.Env{Run: view.Runner{Post: func(fn func()) { posts <- fn }}}, dir,
		func(string, func()) (func(), error) { return func() {}, nil })
	if err := session.Update(dir, term, func(s *session.Session) (bool, error) {
		return s.SetTranscript("s", path), nil
	}); err != nil {
		t.Fatal(err)
	}
	ch.Sync(term)
	ch.SetOn(term, true)
	ch.show()
	for ch.src.Path() == "" || ch.model.Len() == 0 {
		// The read runs on its own goroutine while the UI thread adds pending prompts.
		if ch.src.Path() != "" && len(ch.model.Pendings()) == 0 {
			ch.show()
			ch.model.AddPending("start builders for 609") // its prompt is in the file
			ch.model.AddPending("check the board")
		}
		select {
		case fn := <-posts:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("the first read did not land")
		}
	}
	p := ch.model.Pendings()
	if len(ch.model.Rows()) != 2 || len(p) != 1 || p[0].Text != "check the board" || p[0].ID != "pending:2" {
		t.Fatalf("rows %d, pending %+v; want 2 rows and the second pending prompt with its id", len(ch.model.Rows()), p)
	}
	ch.model.DropPending(p[0].ID)
	if id := ch.model.AddPending("next"); len(ch.model.Pendings()) != 1 || id != "pending:3" {
		t.Errorf("after the swap: pending %+v, new id %q; want one, pending:3", ch.model.Pendings(), id)
	}
}

// The live line of a working session asks for a frame each second while the chat shows; in
// Terminal mode it asks for none, also when the transcript changes (#660, D257).
func TestLiveLineAsksForNoFrameInTerminalMode(t *testing.T) {
	r := newRedrawRig(t)
	r.ch.Worker = func(addr string) (agents.Worker, bool) {
		return agents.Worker{Name: "lead", Address: addr, Live: true, Status: "working", Command: "claude"}, addr == term
	}
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		if r.ch.On(term) {
			r.ch.View(c, func() {})
		} else {
			r.ch.Hide()
		}
	}, 1000, 900)
	r.ch.Sync(term)
	r.ch.SetOn(term, true)
	turn := tt.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339Nano)
	r.add(t, strings.Replace(lTurn, "2026-11-10T10:00:00Z", turn, 1))
	r.add(t, lRunning)
	tt.Frame()
	before := builds
	tt.Advance(3 * time.Second)
	if !r.ch.ticks || builds-before < 3 {
		t.Fatalf("Chat mode: %d frames in 3 s (ticks %v); want one a second at least", builds-before, r.ch.ticks)
	}

	r.ch.SetOn(term, false)
	tt.Frame()
	r.posts, r.redraws, before = 0, 0, builds
	r.add(t, lThink)
	tt.Advance(3 * time.Second)
	if builds != before || r.frames() != 0 {
		t.Errorf("Terminal mode, a write and 3 s: %d frames built, %d asked; want 0, 0", builds-before, r.frames())
	}
}
