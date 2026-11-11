package chatview

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
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
					r.ch.View(c)
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
	for _, want := range []string{"start builders for 609", "#633", "open", "2 worker notes · 2 tool calls", "b-631 is done."} {
		if !r.tt.HasText(want) {
			t.Errorf("no %q in %v", want, r.tt.Texts())
		}
	}
	for _, hidden := range []string{"check the board", "<user-prompt-submit-hook>check the board</user-prompt-submit-hook>", "Caveat: local commands", "hm", "subagent text"} {
		if r.tt.HasText(hidden) {
			t.Errorf("hidden text %q shows", hidden)
		}
	}
	if apptest.Absent(r.tt, LabelPrompt) {
		t.Error("no prompt bubble")
	}
	// A worker that ended its turn without a report folds: no line outside, no red (D259).
	if !apptest.Absent(r.tt, LabelOwnerNote+" b-632") || r.tt.HasText("ended its turn without a report. Last words: waiting") {
		t.Error("a worker note shows outside the fold")
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

// The time shows once per turn, under its prompt at the right; Claude's texts show none (D266).
// A pending prompt and a prompt with no time show none.
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
	prompt, _ := r.tt.Find("first")
	p, ok := r.tt.Find(TimeText(now.Add(-5*time.Minute), now))
	if !ok {
		t.Fatalf("no prompt time in %v", r.tt.Texts())
	}
	for _, d := range []time.Duration{4 * time.Minute, 3 * time.Minute} {
		if x := TimeText(now.Add(-d), now); r.tt.HasText(x) {
			t.Errorf("the time %q of a text of Claude shows", x)
		}
	}
	if p.X+p.W < prompt.X+prompt.W-14 || p.Y < prompt.Y {
		t.Errorf("prompt time %+v is not under the bubble at the right %+v", p, prompt)
	}
}

func TestTasksAndAgentMessagesShowAsLines(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	anon := `{"type":"user","uuid":"u12","message":{"content":"Another Claude session sent a message:\nrebase please"}}`
	r.write(lPrompt, lCall1, lRes1, lTaskOK, lAgent, anon, lNudge, lTaskBad, lSaid2)
	text := "2 worker notes · 1 background task · 1 tool call"
	if !r.tt.HasText(text) {
		t.Fatalf("no group %q in %v", text, r.tt.Texts())
	}
	// The failed task needs the owner: outside the fold.
	if apptest.Absent(r.tt, LabelOwnerNote+" "+TextTask) || !slices.Contains(r.tt.Texts(), "failed") || !r.tt.HasText(`· "go vet" failed with exit code 1`) {
		t.Errorf("no failed task outside the fold: %v", r.tt.Texts())
	}
	prompts := 0
	for _, row := range r.ch.model.Rows() {
		if row.Kind == RowPrompt {
			prompts++
		}
	}
	if prompts != 1 {
		t.Errorf("%d prompt bubbles, want 1: a harness text shows as a prompt", prompts)
	}
	if err := r.tt.Click(LabelGroup + " " + text); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !slices.Contains(r.tt.Texts(), "completed") {
		t.Errorf("no status word of the completed task: %v", r.tt.Texts())
	}
	for _, want := range []string{TextTask, `· "go test ./..." completed (exit code 0)`, "planner-12", "The plan is ready.", "session", "rebase please"} {
		if !r.tt.HasText(want) {
			t.Errorf("open group: no %q in %v", want, r.tt.Texts())
		}
	}
	if apptest.Absent(r.tt, LabelNote+" "+TextTask) || apptest.Absent(r.tt, LabelNote+" planner-12") || apptest.Absent(r.tt, LabelNote+" session") {
		t.Error("no task or agent line in the group")
	}
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, "<") || strings.Contains(s, "hasn't heard") {
			t.Errorf("raw harness text shows: %q", s)
		}
	}
}

// A failed task stays outside the fold, with no dot and no red (D259).
func TestFailedTaskHasNoRed(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt, lCall1, lRes1, lTaskBad, lSaid2)
	if !slices.Contains(r.tt.Texts(), "failed") || !r.tt.HasText(`· "go vet" failed with exit code 1`) {
		t.Fatalf("no failed task outside the fold: %v", r.tt.Texts())
	}
	if !apptest.Absent(r.tt, TextTask+" needs you") {
		t.Error("a failed task has a dot")
	}
	if n := reddish(r.tt.Image(), apptest.Box(t, r.tt, LabelOwnerNote+" "+TextTask)); n > 0 {
		t.Errorf("the failed task's line has %d red pixels", n)
	}
}

// reddish counts the pixels of box whose red stands well above green and blue: the blocked hues,
// not the greys of the inks.
func reddish(img *image.RGBA, box ui.Rect) int {
	n := 0
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			p := img.RGBAAt(x, y)
			if int(p.R)-int(max(p.G, p.B)) > 40 {
				n++
			}
		}
	}
	return n
}

// A blocked worker's dot in a fold row is ink-100, not red: "N needs you" is the one red (D259).
func TestBlockedSenderDotHasNoRed(t *testing.T) {
	r := newRig(t)
	r.ch.Worker = func(addr string) (agents.Worker, bool) {
		return agents.Worker{Name: "b-631", Address: addr, Status: "blocked"}, addr == "w2:t1"
	}
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt, lNote1, lSaid2)
	if apptest.Absent(r.tt, "b-631 blocked") {
		t.Fatalf("no dot of the blocked sender: %v", r.tt.Texts())
	}
	if n := reddish(r.tt.Image(), apptest.Box(t, r.tt, "b-631 blocked")); n > 0 {
		t.Errorf("the blocked sender's dot has %d red pixels", n)
	}
}

// The chat text size scales the chat's text (D272): a larger size makes the prompt bubble taller, and no TextScale is the Default size.
func TestTextScaleGrowsTheChatText(t *testing.T) {
	heights := func(scale float32) float32 {
		r := newRig(t)
		if scale != 0 {
			r.ch.TextScale = func() float32 { return scale }
		}
		r.savePath(term)
		r.selectTerm(term)
		r.chat()
		r.write(lPrompt, lSaid)
		r.tt.Frame()
		p, ok := r.tt.Find(LabelPrompt)
		if !ok {
			t.Fatal("no prompt")
		}
		return p.H
	}
	p0, p1, p2 := heights(0), heights(1), heights(1.3)
	if p0 != p1 {
		t.Errorf("no TextScale: prompt %v, want the Default %v", p0, p1)
	}
	if p2 <= p1 {
		t.Errorf("at 1.3 the prompt is %v, default %v; want taller", p2, p1)
	}
}
