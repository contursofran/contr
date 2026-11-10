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
	// Send types text into the terminal at addr and presses Enter. It blocks: it runs off the UI
	// thread. Nil: the composer sends nothing.
	Send func(addr, text string) error
	// Interrupt presses the stop key of the terminal at addr, as Esc does in the terminal. It
	// blocks. Nil: Esc and Stop do nothing.
	Interrupt func(addr string) error
	// Read and SendKeys read and answer the menu of a session that needs the owner
	// (questions.Menu). They block. Nil Read: the needs-you line shows no choices.
	Read     func(addr string, lines int) (string, error)
	SendKeys func(addr string, keys []byte) error
	// Meta is the model and the context of the terminal at addr, "opus high · 212k", for the
	// right of the composer. May be nil.
	Meta func(addr string) string

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

	// The composer: the text being typed and the last failure, for each terminal.
	drafts map[string]*string
	errs   map[string]string
	// sending is set while a send or an interrupt runs.
	sending bool
	// panel is the chat's box, for the width of wide blocks (wide.go).
	panel ui.Handle
	// parts holds the parts of each text of Claude by row id, and partsOf the text they came from.
	parts   map[string][]Part
	partsOf map[string]string
	// field is the composer's field, which takes the focus back after a click on a button.
	field ui.Handle
	// toTerm is the toTerminal of the last View.
	toTerm func()
	// menu is the menu of the session that needs the owner (live.go).
	menu menuState
}

// New returns a chat view that reads the session files under stateDir (package session).
// watch replaces the file watch in tests; nil uses the real one.
func New(env view.Env, stateDir string, watch func(path string, notify func()) (func(), error)) *Chat {
	ch := &Chat{Env: env, reader: transcript.NewReader(0), chat: map[string]bool{}, folds: map[string]*bool{},
		drafts: map[string]*string{}, errs: map[string]string{}, parts: map[string][]Part{}, partsOf: map[string]string{}}
	ch.src = &chatsource.Source{StateDir: stateDir, Env: env, Watch: watch, Changed: ch.changed}
	ch.list.FollowEnd = true
	ch.list.Key = func(i int) any {
		if i < len(ch.model.rows) {
			return ch.model.rows[i].ID
		}
		return ch.model.pending[i-len(ch.model.rows)].ID
	}
	return ch
}

// Sync follows terminal id; "" stops. Call it with the selected terminal.
func (ch *Chat) Sync(id string) {
	if id != ch.src.Terminal() {
		ch.shown = false
		ch.menu = menuState{}
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

// changed is the source's signal: the path moved or the file changed.
func (ch *Chat) changed(reset bool) {
	if reset {
		ch.gen++
		ch.reading = false
		ch.reader = transcript.NewReader(0)
		ch.model.Reset()
		ch.folds = map[string]*bool{}
		ch.parts, ch.partsOf = map[string][]Part{}, map[string]string{}
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
	r, gen := ch.reader, ch.gen
	ch.Env.Do(func() func() {
		b, err := r.ReadFile(path)
		return func() {
			if gen != ch.gen {
				return
			}
			ch.reading = false
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Printf("chat: read transcript: %v", err)
			}
			ch.model.Apply(b)
			ch.Env.Redraw()
			ch.pull()
		}
	})
}
