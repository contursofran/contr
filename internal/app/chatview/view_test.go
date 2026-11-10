package chatview

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/session"
)

const (
	term  = "w1:t1"
	other = "w1:t2"
)

// rig is a chat of terminal id in a window, with a fake file watch. The zero Env runs the reads
// at once.
type rig struct {
	t      *testing.T
	ch     *Chat
	tt     *ui.Tester
	dir    string
	path   string
	id     string
	notify map[string]func()
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir(), id: term, notify: map[string]func(){}}
	r.path = filepath.Join(t.TempDir(), "s.jsonl")
	r.ch = New(view.Env{}, r.dir, func(path string, notify func()) (func(), error) {
		r.notify[path] = notify
		return func() {}, nil
	})
	r.ch.Worker = func(addr string) (agents.Worker, bool) {
		return agents.Worker{Name: "b-631", Address: addr, Status: "working"}, addr == "w2:t1"
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			r.ch.Switch(c, r.id)
			if r.ch.On(r.id) {
				ui.Box(c).Grow(1).MinHeight(0).Children(func() {
					r.ch.View(c, func() { r.ch.SetOn(r.id, false) })
				})
			} else {
				r.ch.Hide()
			}
		})
	}, 1000, 900)
	return r
}

// savePath saves the transcript path in the session file of terminal id.
func (r *rig) savePath(id string) {
	r.t.Helper()
	if err := session.Update(r.dir, id, func(s *session.Session) (bool, error) {
		return s.SetTranscript("s", r.path), nil
	}); err != nil {
		r.t.Fatal(err)
	}
}

// write sets the transcript to lines and fires its watch.
func (r *rig) write(lines ...string) {
	r.t.Helper()
	apptest.WriteFakeFile(r.t, r.path, strings.Join(lines, "\n")+"\n")
	if n := r.notify[r.path]; n != nil {
		n()
	}
	r.tt.Frame()
}

// has tells that an element with label s shows.
func (r *rig) has(s string) bool {
	_, ok := r.tt.Find(s)
	return ok
}

// select points the chat at terminal id, as the frame does on a new selection.
func (r *rig) selectTerm(id string) {
	r.id = id
	r.ch.Sync(id)
	r.tt.Frame()
}

func (r *rig) chat() {
	r.t.Helper()
	if err := r.tt.Click(TextChat); err != nil {
		r.t.Fatal(err)
	}
	r.tt.Frame()
}

func TestSwitchKeepsTheModeOfEachTerminal(t *testing.T) {
	r := newRig(t)
	r.selectTerm(term)
	if r.ch.On(term) {
		t.Fatal("Terminal is the default")
	}
	r.chat()
	if !r.ch.On(term) {
		t.Fatal("a click on Chat does not switch")
	}
	r.selectTerm(other)
	if r.ch.On(other) {
		t.Error("the mode of one terminal moved to another")
	}
	r.selectTerm(term)
	if !r.ch.On(term) || !r.tt.HasText(TextEmpty) {
		t.Errorf("the mode is not kept: on %v, texts %v", r.ch.On(term), r.tt.Texts())
	}
	if err := r.tt.Click(TextTerm); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.ch.On(term) {
		t.Error("a click on Terminal does not switch back")
	}
	r.ch.Toggle(term)
	r.tt.Frame()
	if !r.ch.On(term) {
		t.Error("Toggle does not switch")
	}
}

func TestNoTranscriptYet(t *testing.T) {
	r := newRig(t)
	r.selectTerm(term)
	r.chat()
	if !r.tt.HasText(TextEmpty) || !r.has(LabelComposer) || r.ch.HasPath(term) {
		t.Fatalf("no path: texts %v", r.tt.Texts())
	}
	// A path whose file does not exist yet.
	r.savePath(term)
	r.selectTerm("")
	r.selectTerm(term)
	if !r.ch.HasPath(term) || !r.tt.HasText(TextEmpty) {
		t.Errorf("missing file: path %v, texts %v", r.ch.HasPath(term), r.tt.Texts())
	}
}

func TestChatShowsTheSession(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt, lHook, lMeta, lThink, lSaid, lCall1, lNote1, lCall2, lRes1, lNote2, lSaid2, lSide)
	for _, want := range []string{"start builders for 609", "#633", "open", "1 worker note · 2 tool calls", "b-632",
		"ended its turn without a report. Last words: waiting", "b-631 is done."} {
		if !r.tt.HasText(want) {
			t.Errorf("no %q in %v", want, r.tt.Texts())
		}
	}
	for _, hidden := range []string{"check the board", "<user-prompt-submit-hook>check the board</user-prompt-submit-hook>", "Caveat: local commands", "hm", "subagent text"} {
		if r.tt.HasText(hidden) {
			t.Errorf("hidden text %q shows", hidden)
		}
	}
	if apptest.Absent(r.tt, LabelPrompt) || apptest.Absent(r.tt, LabelOwnerNote+" b-632") {
		t.Error("no prompt bubble or owner note")
	}
	if !apptest.Absent(r.tt, LabelCall+" Bash") {
		t.Error("a folded group shows its lines")
	}
	if apptest.Absent(r.tt, "b-631 working") {
		t.Error("no dot for the sender of a note")
	}
	if apptest.Absent(r.tt, "working") {
		t.Error("a call without a result shows no working glyph")
	}
}

func TestOpenGroupListsItsLines(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt, lCall1, lNote1, lCall2, lRes1, lRes2Err)
	if err := r.tt.Click(LabelGroup + " 1 worker note · 2 tool calls"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	for _, want := range []string{"Bash", "git log --oneline -3", "2.5s", "contr", "send", TextError, "b-631", "b-631: done, PR #633, check ok"} {
		if !r.tt.HasText(want) {
			t.Errorf("open group: no %q in %v", want, r.tt.Texts())
		}
	}
	if apptest.Absent(r.tt, LabelCall+" Bash") || apptest.Absent(r.tt, LabelNote+" b-631") {
		t.Error("no call or note line")
	}
	if !apptest.Absent(r.tt, "working") {
		t.Error("all calls are done, yet a working glyph shows")
	}
}

func TestChangesAppendAndResetReadsAgain(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt)
	if !r.tt.HasText("start builders for 609") {
		t.Fatalf("texts %v", r.tt.Texts())
	}
	r.write(lPrompt, lSaid2)
	if !r.tt.HasText("b-631 is done.") || len(r.ch.model.Rows()) != 2 {
		t.Fatalf("append: rows %d, texts %v", len(r.ch.model.Rows()), r.tt.Texts())
	}
	// A shorter file is a reset: the source tells, and the chat reads from 0.
	r.ch.changed(true)
	r.write(lSaid2)
	r.tt.Frame()
	if r.tt.HasText("start builders for 609") || len(r.ch.model.Rows()) != 1 {
		t.Errorf("reset: rows %d, texts %v", len(r.ch.model.Rows()), r.tt.Texts())
	}
}

func TestNoReadWhileHidden(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.write(lPrompt)
	if r.ch.model.Len() != 0 {
		t.Fatal("the transcript was read while the terminal showed")
	}
	r.chat()
	if !r.tt.HasText("start builders for 609") {
		t.Errorf("the chat did not read what changed while hidden: %v", r.tt.Texts())
	}
}

func TestBadFileIsLoggedNotShown(t *testing.T) {
	r := newRig(t)
	if err := os.MkdirAll(r.path, 0o755); err != nil { // a folder in the file's place
		t.Fatal(err)
	}
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	if !r.tt.HasText(TextEmpty) {
		t.Errorf("texts %v", r.tt.Texts())
	}
}

// TestTheSwitchChipIsCenteredOnTheSwitch is D248: the switch has no icon, so its ⌘E chip is
// centered on it, inside it and the window.
func TestTheSwitchChipIsCenteredOnTheSwitch(t *testing.T) {
	r := newRig(t)
	r.ch.Hints = true
	r.tt.Frame()
	chip := apptest.ChipOn(t, r.tt, kit.KeyHintLabel("E"), LabelSwitch, 1000, 900)
	apptest.Centered(t, "chip", chip, "the switch", apptest.Box(t, r.tt, LabelSwitch))
}

// The switch is solid: what is under it does not show through (#652).
func TestTheSwitchIsSolid(t *testing.T) {
	pixel := func(under ui.Color) color.RGBA {
		ch := New(view.Env{}, t.TempDir(), nil)
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(under).Padding(20).Children(func() { ch.Switch(c, term) })
		}, 300, 100)
		r, ok := tt.Find(LabelSwitchBack)
		if !ok {
			t.Fatal("no switch box")
		}
		// Inside the box, in the track's padding: no segment and no text there.
		return tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+1.5))
	}
	if a, b := pixel(ui.RGBA(255, 0, 0, 255)), pixel(ui.RGBA(0, 0, 255, 255)); a != b {
		t.Errorf("the background shows through the switch: %v over red, %v over blue", a, b)
	}
}

func TestTimeText(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*3600)
	now := time.Date(2026, 11, 10, 20, 0, 0, 0, zone)
	for at, want := range map[string]string{
		"2026-11-10T17:32:00Z": "14:32",           // today, in the zone
		"2026-11-10T02:59:00Z": "Yesterday 23:59", // the 9th in the zone
		"2026-11-09T03:00:00Z": "Yesterday 00:00",
		"2026-11-09T02:00:00Z": "8 Nov 23:00",
		"2025-12-31T12:05:00Z": "31 Dec 09:05",
	} {
		tm, err := time.Parse(time.RFC3339, at)
		if err != nil {
			t.Fatal(err)
		}
		if got := TimeText(tm, now); got != want {
			t.Errorf("TimeText(%s) = %q, want %q", at, got, want)
		}
	}
}

// The time of a prompt shows under it at the right; the time of an answer once, under its last
// text, at the left. A pending prompt and an entry with no time show none.
func TestSentTimes(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	now := r.tt.Now()
	at := func(d time.Duration) string { return now.Add(-d).UTC().Format(time.RFC3339Nano) }
	text := func(uuid, s string, ago time.Duration) string {
		return `{"type":"assistant","uuid":"` + uuid + `","timestamp":"` + at(ago) + `","message":{"content":[{"type":"text","text":"` + s + `"}]}}`
	}
	r.write(`{"type":"user","uuid":"p1","timestamp":"`+at(5*time.Minute)+`","message":{"content":"first"}}`,
		text("s1", "one", 4*time.Minute), text("s2", "two", 3*time.Minute),
		`{"type":"user","uuid":"p2","message":{"content":"no time"}}`)
	want := []string{TimeText(now.Add(-5*time.Minute), now), TimeText(now.Add(-3*time.Minute), now)}
	prompt, _ := r.tt.Find("first")
	two, _ := r.tt.Find("two")
	p, ok1 := r.tt.Find(want[0])
	s, ok2 := r.tt.Find(want[1])
	if !ok1 || !ok2 {
		t.Fatalf("times %q not in %v", want, r.tt.Texts())
	}
	if r.tt.HasText(TimeText(now.Add(-4*time.Minute), now)) {
		t.Error("the time of a text that is not the last of its answer shows")
	}
	if p.X+p.W < prompt.X+prompt.W-14 || p.Y < prompt.Y {
		t.Errorf("prompt time %+v is not under the bubble at the right %+v", p, prompt)
	}
	if s.X > two.X+1 || s.Y < two.Y {
		t.Errorf("answer time %+v is not under the text at the left %+v", s, two)
	}
}
