package chatview

import (
	"errors"
	"io/fs"
	"log"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/chatsource"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/transcript"
)

// Chat is the chat view of the selected terminal and the Chat | Terminal mode of each terminal.
// It follows the transcript through a chatsource.Source and reads it only while the chat shows:
// the read runs off the UI thread and its batch is applied on it. Nothing polls (D55): a read
// comes from the source's change signal or from the switch. Methods run on the UI thread.
type Chat struct {
	// Env runs the reads and redraws.
	Env view.Env
	// Worker gives the agent at an hc address, for the state of the workers that sent notes.
	// May be nil.
	Worker func(addr string) (agents.Worker, bool)
	// Hints shows the key chip of the switch while ⌘ is held (D245).
	Hints bool

	src    *chatsource.Source
	model  Model
	reader *transcript.Reader
	// gen drops the batch of a read that a reset made old.
	gen uint64
	// reading is set while a read runs; stale asks for a read once it ends or the chat shows.
	reading, stale bool
	// shown is set while the chat of the source's terminal shows.
	shown bool
	// chat holds the terminals in Chat mode; Terminal is the default (absent).
	chat map[string]bool
	list ui.ListState
	md   markdown.Options
	// folds holds the open work groups by row id.
	folds map[string]*bool
}

// New returns a chat view that reads the session files under stateDir (package session).
// watch replaces the file watch in tests; nil uses the real one.
func New(env view.Env, stateDir string, watch func(path string, notify func()) (func(), error)) *Chat {
	ch := &Chat{Env: env, reader: transcript.NewReader(0), chat: map[string]bool{}, folds: map[string]*bool{}}
	ch.src = &chatsource.Source{StateDir: stateDir, Env: env, Watch: watch, Changed: ch.changed}
	ch.list.FollowEnd = true
	ch.list.Key = func(i int) any { return ch.model.rows[i].ID }
	return ch
}

// Sync follows terminal id; "" stops. Call it with the selected terminal.
func (ch *Chat) Sync(id string) {
	if id != ch.src.Terminal() {
		ch.shown = false
	}
	ch.src.Sync(id)
}

// Close stops the watches.
func (ch *Chat) Close() { ch.src.Close() }

// HasPath tells that terminal id has a transcript path: a Claude Code session that a hook of the
// kit saw (D243).
func (ch *Chat) HasPath(id string) bool {
	return id != "" && id == ch.src.Terminal() && ch.src.Path() != ""
}

// On tells that terminal id is in Chat mode.
func (ch *Chat) On(id string) bool { return ch.chat[id] }

// SetOn puts terminal id in Chat mode (on) or Terminal mode. The mode is kept for each terminal
// for the run of the app.
func (ch *Chat) SetOn(id string, on bool) {
	if id == "" {
		return
	}
	if on {
		ch.chat[id] = true
	} else {
		delete(ch.chat, id)
	}
	ch.Env.Redraw()
}

// Toggle switches the mode of terminal id.
func (ch *Chat) Toggle(id string) { ch.SetOn(id, !ch.On(id)) }

// Hide tells that the chat does not show: changes of the file wait until it shows again.
func (ch *Chat) Hide() { ch.shown = false }

// changed is the source's signal: the path moved or the file changed. The source asks for no
// redraw (#660, D257): a change redraws only after its batch is applied, so a transcript that
// grows while the chat does not show asks for no frame. A reset redraws: the switch may show or
// go (HasPath), and the chat is empty again.
func (ch *Chat) changed(reset bool) {
	if reset {
		ch.gen++
		ch.reading = false
		ch.reader = transcript.NewReader(0)
		ch.model.Reset()
		ch.folds = map[string]*bool{}
		ch.Env.Redraw()
	}
	ch.stale = true
	ch.pull()
}

// show marks the chat as shown and reads what changed while it was not.
func (ch *Chat) show() {
	if !ch.shown {
		ch.shown = true
		ch.pull()
	}
}

// pull reads what was added to the file, off the UI thread, when the chat shows and no read
// runs. A missing file has no text yet (D216); another error is logged.
func (ch *Chat) pull() {
	path := ch.src.Path()
	if !ch.shown || ch.reading || !ch.stale || path == "" {
		return
	}
	ch.stale, ch.reading = false, true
	r, gen, first := ch.reader, ch.gen, ch.model.Len() == 0
	ch.Env.Do(func() func() {
		b, err := r.ReadFile(path)
		// The first read gives the whole file: its model is made here, off the UI thread (#660).
		// Only this read changes the model until it lands, and a reset drops it (gen).
		var m *Model
		if first {
			m = &Model{}
			m.Apply(b)
		}
		return func() {
			if gen != ch.gen {
				return
			}
			ch.reading = false
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Printf("chat: read transcript: %v", err)
			}
			if m != nil {
				ch.model = *m
			} else {
				ch.model.Apply(b)
			}
			ch.Env.Redraw()
			ch.pull()
		}
	})
}
