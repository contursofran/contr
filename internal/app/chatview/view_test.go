package chatview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
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
	if err := r.tt.Click(TextToTerminal); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.ch.On(term) {
		t.Error("the dock's button does not switch to Terminal")
	}
}

func TestNoTranscriptYet(t *testing.T) {
	r := newRig(t)
	r.selectTerm(term)
	r.chat()
	if !r.tt.HasText(TextEmpty) || !r.tt.HasText(TextDock) || r.ch.HasPath(term) {
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

func TestTasksAndAgentMessagesShowAsLines(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt, lCall1, lRes1, lTaskOK, lAgent, lNudge, lTaskBad, lSaid2)
	text := "1 worker note · 1 background task · 1 tool call"
	if !r.tt.HasText(text) {
		t.Fatalf("no group %q in %v", text, r.tt.Texts())
	}
	// The failed task needs the owner: outside the fold.
	if apptest.Absent(r.tt, LabelOwnerNote+" "+TextTask) || !r.tt.HasText("failed") || !r.tt.HasText(`· "go vet" failed with exit code 1`) {
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
	for _, want := range []string{TextTask, "completed", `· "go test ./..." completed (exit code 0)`, "planner-12", "The plan is ready."} {
		if !r.tt.HasText(want) {
			t.Errorf("open group: no %q in %v", want, r.tt.Texts())
		}
	}
	if apptest.Absent(r.tt, LabelNote+" "+TextTask) || apptest.Absent(r.tt, LabelNote+" planner-12") {
		t.Error("no task or agent line in the group")
	}
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, "<") || strings.Contains(s, "hasn't heard") {
			t.Errorf("raw harness text shows: %q", s)
		}
	}
}

func TestTaskStatusInk(t *testing.T) {
	pal := theme.Current()
	if statusInk(pal, "completed", pal.Ink42) != pal.Ink42 {
		t.Error("a completed task's status is not dim")
	}
	for _, s := range []string{"failed", "killed", "running"} {
		if statusInk(pal, s, pal.Ink42) != pal.Blocked {
			t.Errorf("status %q is not in blocked", s)
		}
	}
}
