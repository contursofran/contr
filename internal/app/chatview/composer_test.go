package chatview

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
)

const menuScreen = `╭──────────────────────────╮
│ Bash command             │
│   npm test               │
│ Do you want to proceed?  │
│ ❯ 1. Yes                 │
│   2. No                  │
╰──────────────────────────╯`

// sess is a rig whose terminal runs a Claude Code session in state status, with fake sends.
type sess struct {
	*rig
	status    string
	sent      []string
	sendErr   error
	stops     int
	keys      []string
	screen    string
	reads     int
	interrupt error
}

func newSession(t *testing.T, status string) *sess {
	t.Helper()
	s := &sess{rig: newRig(t), status: status, screen: menuScreen}
	s.ch.Worker = func(addr string) (agents.Worker, bool) {
		return agents.Worker{Name: "lead", Address: addr, Live: true, Status: s.status, Kind: "permission",
			Command: "claude", Turn: 3}, addr == term
	}
	s.ch.Send = func(addr, text string) error {
		if addr != term {
			t.Errorf("send to %q, want the session's own terminal %q", addr, term)
		}
		s.sent = append(s.sent, text)
		return s.sendErr
	}
	s.ch.Interrupt = func(addr string) error {
		if addr != term {
			t.Errorf("interrupt of %q, want %q", addr, term)
		}
		s.stops++
		return s.interrupt
	}
	s.ch.Read = func(addr string, lines int) (string, error) {
		s.reads++
		return s.screen, nil
	}
	s.ch.SendKeys = func(addr string, keys []byte) error {
		s.keys = append(s.keys, string(keys))
		return nil
	}
	s.ch.Meta = func(addr string) string { return "opus high · 212k" }
	s.savePath(term)
	s.selectTerm(term)
	s.chat()
	return s
}

// typeIn types text into the composer, which has the focus.
func (s *sess) typeIn(text string) {
	s.t.Helper()
	if !s.tt.Focused(LabelComposer) {
		s.t.Fatal("the composer does not have the focus")
	}
	s.tt.Type(text)
	s.tt.Frame()
}

func (s *sess) key(mods ui.Modifiers, k ui.Key) {
	s.tt.Key(mods, k)
	s.tt.Frame()
}

func (s *sess) draft() string { return *s.ch.draft(term) }

func TestComposerTakesTheFocusAndShowsNameAndMeta(t *testing.T) {
	s := newSession(t, "idle")
	if !s.tt.Focused(LabelComposer) {
		t.Error("the field does not take the focus when the chat shows")
	}
	if Placeholder("lead") != "Message lead…" || Placeholder("") != "Message…" {
		t.Errorf("placeholder %q", Placeholder("lead"))
	}
	if !s.tt.HasText("opus high · 212k") {
		t.Errorf("no model and context in %v", s.tt.Texts())
	}
	if s.has(LabelStop) {
		t.Error("Stop shows while idle")
	}
}

func TestEnterSendsAndClears(t *testing.T) {
	s := newSession(t, "idle")
	s.typeIn("hello")
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 1 || s.sent[0] != "hello" {
		t.Fatalf("sent %q", s.sent)
	}
	if s.draft() != "" || !s.tt.Focused(LabelComposer) {
		t.Errorf("after a send: draft %q, focused %v", s.draft(), s.tt.Focused(LabelComposer))
	}
	// A blank field sends nothing.
	s.typeIn("  ")
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 1 {
		t.Errorf("a blank field sent %q", s.sent)
	}
}

func TestShiftEnterAddsALine(t *testing.T) {
	s := newSession(t, "idle")
	s.typeIn("one")
	s.key(ui.Shift, ui.KeyEnter)
	s.typeIn("two")
	if s.draft() != "one\ntwo" || len(s.sent) != 0 {
		t.Errorf("draft %q, sent %q", s.draft(), s.sent)
	}
}

func TestFailedSendKeepsTheTextAndShowsTheError(t *testing.T) {
	s := newSession(t, "idle")
	s.sendErr = errors.New("hc: terminal gone")
	s.typeIn("hello")
	s.key(0, ui.KeyEnter)
	if s.draft() != "hello" {
		t.Errorf("draft %q after a failed send", s.draft())
	}
	if !s.tt.HasText("hc: terminal gone") || s.has(LabelPending) {
		t.Errorf("texts %v, pending shown %v", s.tt.Texts(), s.has(LabelPending))
	}
	// Typing again clears the error.
	s.typeIn("!")
	if s.tt.HasText("hc: terminal gone") {
		t.Error("the error stays after a change")
	}
}

func TestEscInterruptsOnlyWhileWorking(t *testing.T) {
	s := newSession(t, "idle")
	s.key(0, ui.KeyEscape)
	if s.stops != 0 || !s.ch.On(term) || !s.tt.Focused(LabelComposer) {
		t.Fatalf("Esc while idle: stops %d, chat on %v, focused %v", s.stops, s.ch.On(term), s.tt.Focused(LabelComposer))
	}
	s.status = "working"
	s.tt.Frame()
	s.key(0, ui.KeyEscape)
	if s.stops != 1 || !s.ch.On(term) {
		t.Errorf("Esc while working: stops %d, chat on %v", s.stops, s.ch.On(term))
	}
	if err := s.tt.Click(LabelStop); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if s.stops != 2 {
		t.Errorf("Stop: stops %d", s.stops)
	}
	s.interrupt = errors.New("no stop key")
	s.key(0, ui.KeyEscape)
	if !s.tt.HasText("no stop key") {
		t.Errorf("a failed interrupt is not shown: %v", s.tt.Texts())
	}
	// Text can still be sent while it works.
	s.typeIn("also this")
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 1 {
		t.Errorf("sent %q while working", s.sent)
	}
}

func TestPendingPromptComesAndGoes(t *testing.T) {
	s := newSession(t, "idle")
	s.write(lPrompt)
	s.typeIn("next one")
	s.key(0, ui.KeyEnter)
	if !s.has(LabelPending) || !s.tt.HasText("next one") {
		t.Fatalf("no pending prompt: %v", s.tt.Texts())
	}
	s.write(lPrompt, `{"type":"user","uuid":"u9","message":{"content":"next one"}}`)
	if s.has(LabelPending) || !s.tt.HasText("next one") {
		t.Errorf("the transcript's entry does not replace the pending one: pending %v", s.has(LabelPending))
	}
	// A reset drops a pending prompt.
	s.typeIn("lost")
	s.key(0, ui.KeyEnter)
	s.ch.changed(true)
	s.tt.Frame()
	if s.has(LabelPending) {
		t.Error("a reset keeps the pending prompt")
	}
}

const (
	lTurn    = `{"type":"user","uuid":"p1","timestamp":"2026-11-10T10:00:00Z","message":{"content":"run the tests"}}`
	lRunning = `{"type":"assistant","uuid":"r1","timestamp":"2026-11-10T10:00:01Z","message":{"content":[{"type":"tool_use","id":"r1","name":"Bash","input":{"command":"go test ./..."}}]}}`
)

const lDone = `{"type":"user","uuid":"r2","message":{"content":[{"type":"tool_result","tool_use_id":"r1","content":"ok"}]}}`

func TestLiveLineWorking(t *testing.T) {
	s := newSession(t, "working")
	turn := s.tt.Now().Add(-65 * time.Second).UTC().Format(time.RFC3339Nano)
	lTurn := strings.Replace(lTurn, "2026-11-10T10:00:00Z", turn, 1)
	s.write(lTurn, lRunning)
	if !s.has(LabelLive+" Bash · go test ./...") || !s.tt.HasText("1m 5s") {
		t.Fatalf("running tool: texts %v", s.tt.Texts())
	}
	if !s.has(LabelStop) {
		t.Error("no Stop while working")
	}
	// With Reduce Motion the glyph stands still, so only the line's own redraw moves the time.
	s.tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	s.tt.Frame()
	if !s.ch.ticks {
		t.Error("a working agent with a turn time asks for no redraw")
	}
	s.tt.Advance(1100 * time.Millisecond)
	if !s.tt.HasText("1m 6s") {
		t.Errorf("the time does not count: %v", s.tt.Texts())
	}
	s.write(lTurn, lRunning, lThink)
	if !s.has(LabelLive + " " + TextThinking) {
		t.Errorf("thinking: texts %v", s.tt.Texts())
	}
	s.write(lTurn, lRunning, lThink, lDone, lSaid2)
	if !s.has(LabelLive + " " + TextWorking) {
		t.Errorf("working: texts %v", s.tt.Texts())
	}
	// Without a time, no time shows and no redraw is asked.
	s.write(lTurn, lRunning, lThink, lDone, lSaid2, `{"type":"user","uuid":"p2","message":{"content":"go"}}`)
	for _, x := range s.tt.Texts() {
		if elapsedRe.MatchString(x) {
			t.Errorf("a turn without time shows %q", x)
		}
	}
	if s.ch.ticks {
		t.Error("a turn without time asks for a redraw")
	}
}

var elapsedRe = regexp.MustCompile(`^\d+(m \d+)?s$`)

func TestIdleHasNoLineAndNoRedraw(t *testing.T) {
	s := newSession(t, "idle")
	s.write(lTurn, lRunning)
	if s.has(LabelLive) {
		t.Errorf("a live line while idle: %v", s.tt.Texts())
	}
	if s.ch.ticks || s.tt.FramePending() {
		t.Error("a redraw is asked while idle")
	}
}

func TestNeedsYouWithChoices(t *testing.T) {
	s := newSession(t, "blocked")
	if !s.has(LabelLive+" Do you want to proceed?") || !s.tt.HasText(ChoiceText(0, "Yes")) || !s.tt.HasText(ChoiceText(1, "No")) {
		t.Fatalf("no menu: %v", s.tt.Texts())
	}
	reads := s.reads
	s.tt.Frame()
	if s.reads != reads {
		t.Error("the menu is read again with no change of the worker")
	}
	// Enter with text does not reach a menu (D51).
	s.typeIn("hi")
	s.key(0, ui.KeyEnter)
	if len(s.sent) != 0 || !s.tt.HasText(TextAnswerFirst) || s.draft() != "hi" {
		t.Errorf("Enter on a menu: sent %q, texts %v", s.sent, s.tt.Texts())
	}
	if err := s.tt.Click(ChoiceText(1, "No")); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if len(s.keys) != 1 || s.keys[0] != "2" {
		t.Fatalf("keys %q", s.keys)
	}
	// A digit in the empty field sends its choice at once, with no Enter.
	*s.ch.draft(term) = ""
	s.tt.Frame()
	s.typeIn("1")
	if len(s.keys) != 2 || s.keys[1] != "1" || s.draft() != "" {
		t.Errorf("digit: keys %q, draft %q", s.keys, s.draft())
	}
	// A menu that changed sends nothing.
	s.screen = strings.Replace(menuScreen, "2. No ", "2. Nah", 1)
	if err := s.tt.Click(ChoiceText(0, "Yes")); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if len(s.keys) != 2 || !s.tt.HasText("The prompt changed; look again.") {
		t.Errorf("changed menu: keys %q, texts %v", s.keys, s.tt.Texts())
	}
}

func TestNeedsYouFallback(t *testing.T) {
	s := newSession(t, "blocked")
	s.screen = "no menu here"
	s.ch.menu = menuState{}
	s.tt.Frame()
	if !s.has(LabelLive+" "+TextNeedsYou) || s.tt.HasText(ChoiceText(0, "Yes")) {
		t.Fatalf("no fallback: %v", s.tt.Texts())
	}
	// An error state has no menu to read.
	s.status = "error"
	s.screen = menuScreen
	s.ch.menu = menuState{}
	reads := s.reads
	s.tt.Frame()
	if s.reads != reads || s.tt.HasText(ChoiceText(0, "Yes")) {
		t.Error("the menu of an error state is read")
	}
	if !s.has(LabelLive + " " + TextNeedsYou) {
		t.Fatalf("no fallback for an error: %v", s.tt.Texts())
	}
	if err := s.tt.Click(TextToTerminal); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if s.ch.On(term) {
		t.Error("Switch to Terminal does not switch")
	}
}
