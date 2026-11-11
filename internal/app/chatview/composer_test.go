package chatview

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
)

// sess is a rig whose terminal runs a Claude Code session in state status, with fake sends.
type sess struct {
	*rig
	status    string
	sent      []string
	sendErr   error
	stops     int
	interrupt error
}

func newSession(t *testing.T, status string) *sess {
	t.Helper()
	s := &sess{rig: newRig(t), status: status}
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
	if !s.has(LabelLive+" "+TextWorking+" 1m 5s · Bash · go test ./...") || s.tt.HasText(TextKeys) {
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
	if !s.tt.HasText(TextWorking + " 1m 6s") {
		t.Errorf("the time does not count: %v", s.tt.Texts())
	}
	// Without a time, no time shows and no redraw is asked.
	s.write(lTurn, lRunning, lThink, lDone, lSaid2, `{"type":"user","uuid":"p2","message":{"content":"go"}}`)
	for _, x := range s.tt.Texts() {
		if elapsedRe.MatchString(x) || x != TextWorking && strings.HasPrefix(x, TextWorking+" ") {
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
	if s.has(LabelLive) || !s.tt.HasText(TextKeys) {
		t.Errorf("idle: a working status or no keys: %v", s.tt.Texts())
	}
	s.tt.Advance(2 * theme.MotionSelect) // past the end of the fade of the Chat | Terminal switch (D256)
	if s.ch.ticks || s.tt.FramePending() {
		t.Error("a redraw is asked while idle")
	}
}

// A session that waits on a menu has no line in the dock: the notch answers it (D260). Enter
// sends nothing and keeps the text; a digit is text, not a choice (D51). An error state is the
// same.
func TestNeedsYouHasNoLineAndEnterWaits(t *testing.T) {
	for _, status := range []string{"blocked", "error"} {
		s := newSession(t, status)
		if s.tt.HasText(LabelLive) || s.tt.HasText("Do you want to proceed?") || s.tt.HasText("Switch to Terminal") {
			t.Errorf("%s: a needs-you line in the dock: %v", status, s.tt.Texts())
		}
		s.typeIn("1")
		s.key(0, ui.KeyEnter)
		if len(s.sent) != 0 || s.draft() != "1" || !s.tt.HasText("Answer in the notch first (⌘J), or switch to Terminal.") {
			t.Errorf("%s: Enter on a menu: sent %q, draft %q, texts %v", status, s.sent, s.draft(), s.tt.Texts())
		}
	}
}

func TestActionOf(t *testing.T) {
	for _, tc := range []struct {
		text             string
		working, canStop bool
		want             Action
	}{
		{"", false, true, ActionIdle},
		{"  \n", false, true, ActionIdle},
		{"hi", false, true, ActionSend},
		{"", true, true, ActionStop},
		{"hi", true, true, ActionSend}, // the message queues (D265)
		{"", true, false, ActionIdle},  // nothing can stop it
	} {
		if got := ActionOf(tc.text, tc.working, tc.canStop); got != tc.want {
			t.Errorf("ActionOf(%q, %v, %v) = %v, want %v", tc.text, tc.working, tc.canStop, got, tc.want)
		}
	}
}

// rect is where label shows; the test fails when it does not.
func (s *sess) rect(label string) ui.Rect {
	s.t.Helper()
	r, ok := s.tt.Find(label)
	if !ok {
		s.t.Fatalf("no %q in %v", label, s.tt.Texts())
	}
	return r
}

// Idle and empty, the card has no button and the footer shows the keys (D276); with text, Send
// shows.
func TestComposerIdleEmptyHasNoButton(t *testing.T) {
	s := newSession(t, "idle")
	if s.has(LabelSend) || s.has(LabelStop) {
		t.Fatalf("idle and empty: send %v, stop %v", s.has(LabelSend), s.has(LabelStop))
	}
	if !s.tt.HasText(TextKeys) || s.has(LabelLive) {
		t.Errorf("idle footer: %v", s.tt.Texts())
	}
	field := s.rect(LabelComposer)
	if field.H != actionSize {
		t.Errorf("the empty field is %vpx high, want %d", field.H, actionSize)
	}
	s.typeIn("hi")
	if !s.has(LabelSend) || s.has(LabelStop) || !s.tt.HasText(TextKeys) {
		t.Errorf("typing: send %v, stop %v, texts %v", s.has(LabelSend), s.has(LabelStop), s.tt.Texts())
	}
}

// With text, the round button sends the field as Enter does, clears it and keeps the focus.
func TestComposerSendButtonSends(t *testing.T) {
	s := newSession(t, "idle")
	s.typeIn("hello")
	if err := s.tt.Click(LabelSend); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if len(s.sent) != 1 || s.sent[0] != "hello" || s.draft() != "" || !s.has(LabelPending) {
		t.Fatalf("Send: sent %q, draft %q, pending %v", s.sent, s.draft(), s.has(LabelPending))
	}
	if !s.tt.Focused(LabelComposer) {
		t.Error("the field loses the focus after Send")
	}
}

// While the agent works, the empty field has Stop; text makes it Send, and a click queues the
// message (D265). Emptied again, it is Stop again.
func TestComposerWorkingStopThenSend(t *testing.T) {
	s := newSession(t, "working")
	if !s.has(LabelStop) || s.has(LabelSend) {
		t.Fatalf("working and empty: stop %v, send %v", s.has(LabelStop), s.has(LabelSend))
	}
	s.typeIn("also this")
	if s.has(LabelStop) || !s.has(LabelSend) {
		t.Fatalf("working with text: stop %v, send %v", s.has(LabelStop), s.has(LabelSend))
	}
	if err := s.tt.Click(LabelSend); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if len(s.sent) != 1 || s.stops != 0 || !s.has(LabelStop) {
		t.Errorf("Send while working: sent %q, stops %d, stop shown %v", s.sent, s.stops, s.has(LabelStop))
	}
	if !s.tt.Focused(LabelComposer) {
		t.Error("the field loses the focus after Send")
	}
}

// While the session waits on a menu, Send keeps the text and says to answer the menu first.
func TestComposerSendButtonOnAMenuWaits(t *testing.T) {
	s := newSession(t, "blocked")
	s.typeIn("1")
	if err := s.tt.Click(LabelSend); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	if len(s.sent) != 0 || s.draft() != "1" || !s.tt.HasText(TextAnswerFirst) {
		t.Errorf("Send on a menu: sent %q, draft %q, texts %v", s.sent, s.draft(), s.tt.Texts())
	}
}

// todayDock is the height of the dock before the card (D271): the team row (20), its gap to the
// field (12) and the field box (44).
const todayDock = 20 + 12 + 44

// With the team row, the row sits above the card, outside it, its text at the footer's inset; the button is in line with the field's one line; the model and context sit under
// the field, outside it.
func TestComposerCardAtRestWithTeam(t *testing.T) {
	s := newSession(t, "working")
	s.ch.Team = func(string) (Team, bool) { return Team{Agents: 2, Working: 2}, true }
	s.write(lTurn, lRunning)
	card, team, field, stop := s.rect(LabelCard), s.rect(LabelTeam), s.rect(LabelComposer), s.rect(LabelStop)
	if card.H > todayDock+4 {
		t.Errorf("the card is %vpx high, today's dock %dpx", card.H, todayDock)
	}
	if team.Y+team.H > card.Y || team.X != card.X {
		t.Errorf("the team row %v is not above the card %v", team, card)
	}
	if head := s.rect("Orchestrating · 2 agents"); head.Y+head.H > card.Y || head.X < card.X+footerInset {
		t.Errorf("the team head %v is not at the footer's inset above the card %v", head, card)
	}
	if field.Y < card.Y || field.Y+field.H > card.Y+card.H {
		t.Errorf("the field %v is not in the card %v", field, card)
	}
	if stop.W != actionSize || stop.H != actionSize || stop.Y+stop.H != field.Y+field.H {
		t.Errorf("the button %v is not 30px and in line with the field %v", stop, field)
	}
	foot, meta := s.rect(LabelFooter), s.rect("opus high · 212k")
	if foot.Y < card.Y+card.H || foot.H != footerHeight || meta.Y < foot.Y || meta.X+meta.W != foot.X+foot.W-footerInset {
		t.Errorf("the footer %v with the meta %v is not under the card %v at its right", foot, meta, card)
	}
	if !s.footerWorks() {
		t.Errorf("no working status in the footer: %v", s.tt.Texts())
	}
	if field.X+field.W > stop.X {
		t.Errorf("the field %v runs under the button %v", field, stop)
	}
}

// Idle with no team, the card has no top line: it is the field alone, with its foot.
func TestComposerCardWithoutTopLine(t *testing.T) {
	s := newSession(t, "idle")
	s.write(lTurn, lRunning)
	if s.has(LabelTeam) || s.has(LabelLive) {
		t.Fatalf("a top line with no team and nothing working: %v", s.tt.Texts())
	}
	card, field := s.rect(LabelCard), s.rect(LabelComposer)
	if top := field.Y - card.Y; top != 1+cardPadY {
		t.Errorf("the field is %vpx under the card's top, want %d", top, 1+cardPadY)
	}
}

// The field grows with its text to 8 lines, then scrolls; the button stays in line with the last
// line it shows.
func TestComposerGrowsToEightLines(t *testing.T) {
	s := newSession(t, "idle")
	one := s.rect(LabelComposer).H
	lines := func(n int) float32 {
		s.t.Helper()
		*s.ch.draft(term) = strings.TrimSuffix(strings.Repeat("line\n", n), "\n")
		s.tt.Frame()
		return s.rect(LabelComposer).H
	}
	line := float32(composerText.Line)
	if h := lines(3); h != one+2*line {
		t.Errorf("3 lines: %vpx, want %v", h, one+2*line)
	}
	eight := lines(composerLines)
	if eight != one+(composerLines-1)*line {
		t.Errorf("8 lines: %vpx, want %v", eight, one+(composerLines-1)*line)
	}
	if h := lines(12); h != eight {
		t.Errorf("12 lines: %vpx, want the height of 8, %v", h, eight)
	}
	*s.ch.draft(term) += "x"
	s.tt.Frame()
	field, send := s.rect(LabelComposer), s.rect(LabelSend)
	if send.Y+send.H != field.Y+field.H {
		t.Errorf("the button %v is not in line with the last line of the field %v", send, field)
	}
}

// footerWorks says whether the footer shows the working status, "Working" and a time.
func (r *rig) footerWorks() bool {
	for _, x := range r.tt.Texts() {
		if strings.HasPrefix(x, LabelLive+" "+TextWorking+" ") {
			return true
		}
	}
	return false
}
