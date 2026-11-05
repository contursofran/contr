package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
)

// inputLog records what the frame's terminal sends to hc.
type inputLog struct {
	mu  sync.Mutex
	buf []byte
}

func (l *inputLog) add(p []byte) { l.mu.Lock(); l.buf = append(l.buf, p...); l.mu.Unlock() }
func (l *inputLog) text() string { l.mu.Lock(); defer l.mu.Unlock(); return string(l.buf) }

// recStream is an attached terminal that logs the input and ends when closed.
type recStream struct {
	ev   chan hc.StreamEvent
	once sync.Once
	log  *inputLog
}

func (s *recStream) Events() <-chan hc.StreamEvent { return s.ev }
func (*recStream) Err() error                      { return nil }
func (s *recStream) Write(p []byte) (int, error)   { s.log.add(p); return len(p), nil }
func (*recStream) Resize(uint16, uint16) error     { return nil }
func (*recStream) Focus(bool) error                { return nil }
func (s *recStream) Close() error                  { s.once.Do(func() { close(s.ev) }); return nil }

type recSource struct{ log *inputLog }

func (s recSource) Attach(context.Context, hc.AttachParams) (ghostty.Stream, error) {
	return &recStream{ev: make(chan hc.StreamEvent), log: s.log}, nil
}
func (recSource) Watch(context.Context) (<-chan hc.Event, func(), error) { return nil, func() {}, nil }
func (recSource) Close() error                                           { return nil }

// waitInput runs the window and the frames until the terminal has sent text to hc.
func waitInput(t *testing.T, w *fakeWindow, tt *ui.Tester, log *inputLog, text string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
		if strings.Contains(log.text(), text) {
			return
		}
	}
	t.Fatalf("the terminal never sent %q to hc; it sent %q", text, log.text())
}

// findFrame returns a frame whose terminal slot shows an hc terminal with some lines of text, and
// a tester for the slot with the theme set, as the frame's View does.
func findFrame(t *testing.T) (*Frame, *fakeWindow, *ui.Tester) {
	f, w, tt, _ := findFrameLog(t)
	return f, w, tt
}

// findFrameLog is findFrame with the log of the input the terminal sends to hc.
func findFrameLog(t *testing.T) (*Frame, *fakeWindow, *ui.Tester, *inputLog) {
	t.Helper()
	f, w, _, _ := terminalFrame(t, nil)
	log := &inputLog{}
	f.TerminalSource = func(string) ghostty.Source { return recSource{log} }
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	if f.term == nil {
		t.Fatal("no terminal after the selection")
	}
	f.term.Feed([]byte("alpha one\r\nbeta two\r\nalpha three\r\ngamma\r\nalpha four\r\n"))
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c); f.buildTerminal(c) }, 800, 500)
	tt.Frame()
	return f, w, tt, log
}

// settle runs the window's queued updates and frames, so the terminal reports its search.
func settle(w *fakeWindow, tt *ui.Tester) {
	for i := 0; i < 5; i++ {
		w.flush()
		tt.Frame()
	}
}

func TestFindBarIsClosedByDefaultAndKeysDoNothing(t *testing.T) {
	f, w, tt := findFrame(t)
	if !absent(tt, LabelFind) {
		t.Fatal("the find bar shows by default")
	}
	// With the bar closed, the layer takes no key: next, previous, Enter and Escape do nothing.
	tt.Key(ui.Cmd, ui.KeyG)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyG)
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEscape)
	settle(w, tt)
	if f.find.open || f.find.shown || f.find.searched != "" {
		t.Errorf("a key changed the closed find bar: %+v", f.find)
	}
	if st := f.term.SearchStatus(); st.Text != "" {
		t.Errorf("the terminal searches %q with the bar closed", st.Text)
	}
	if !absent(tt, LabelFind) {
		t.Error("a key opened the find bar")
	}
}

func TestCmdFOpensTheFindBar(t *testing.T) {
	f, w, tt := findFrame(t)
	tt.Key(ui.Cmd, ui.KeyF)
	settle(w, tt)
	if !f.find.open || absent(tt, LabelFind) {
		t.Fatalf("⌘F did not open the find bar; open %v, texts %q", f.find.open, tt.Texts())
	}
}

func TestCmdFDoesNothingWithoutATerminal(t *testing.T) {
	f, _, tt, _ := terminalFrame(t, nil)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if f.find.open || !absent(tt, LabelFind) {
		t.Error("⌘F opened the find bar with no terminal")
	}
}

func TestTypingFindsTheMatches(t *testing.T) {
	f, w, tt := findFrame(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("alpha")
	settle(w, tt)
	if f.find.query != "alpha" {
		t.Fatalf("query %q, want alpha", f.find.query)
	}
	if st := f.term.SearchStatus(); st.Text != "alpha" || st.Total != 3 || st.Selected != 1 {
		t.Errorf("status %+v, want 3 matches of alpha with the newest selected", st)
	}
	if !tt.HasText("1 of 3") {
		t.Errorf("no count in the bar; texts %q", tt.Texts())
	}
	// A query with no match says so.
	tt.Type("zzz")
	settle(w, tt)
	if !tt.HasText("No matches") {
		t.Errorf("no \"No matches\" for alphazzz; texts %q", tt.Texts())
	}
}

func TestEnterAndShiftEnterGoToTheNextAndPreviousMatch(t *testing.T) {
	f, w, tt := findFrame(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("alpha")
	settle(w, tt)
	selected := func() int { return f.term.SearchStatus().Selected }
	if selected() != 1 {
		t.Fatalf("selected %d, want 1", selected())
	}
	tt.Key(0, ui.KeyEnter)
	settle(w, tt)
	if selected() != 2 {
		t.Errorf("after Enter: selected %d, want 2", selected())
	}
	tt.Key(0, ui.KeyEnter)
	settle(w, tt)
	if selected() != 3 {
		t.Errorf("after a second Enter: selected %d, want 3", selected())
	}
	tt.Key(0, ui.KeyEnter) // wraps round
	settle(w, tt)
	if selected() != 1 {
		t.Errorf("after a third Enter: selected %d, want 1 (wrapped)", selected())
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	settle(w, tt)
	if selected() != 3 {
		t.Errorf("after Shift+Enter: selected %d, want 3 (wrapped back)", selected())
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	settle(w, tt)
	if selected() != 2 {
		t.Errorf("after a second Shift+Enter: selected %d, want 2", selected())
	}
	if !tt.HasText("2 of 3") {
		t.Errorf("the bar does not say 2 of 3; texts %q", tt.Texts())
	}
}

func TestEscapeClosesTheFindBarEndsTheSearchAndFocusesTheTerminal(t *testing.T) {
	f, w, tt, log := findFrameLog(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("alpha")
	settle(w, tt)
	// The keys went to the bar's field, not to the terminal and so not to hc.
	if got := log.text(); got != "" {
		t.Fatalf("typing in the find bar sent %q to hc", got)
	}
	tt.Key(0, ui.KeyEscape)
	settle(w, tt)
	if f.find.open || !absent(tt, LabelFind) {
		t.Errorf("Escape did not close the bar; open %v", f.find.open)
	}
	if st := f.term.SearchStatus(); st.Text != "" {
		t.Errorf("the terminal still searches %q", st.Text)
	}
	// The terminal has the focus again: a typed key goes to it, and to hc.
	tt.Type("q")
	waitInput(t, w, tt, log, "q")
	if f.find.query != "alpha" {
		t.Errorf("the key went to the closed bar: query %q", f.find.query)
	}
}

func TestSearchButtonOpensTheFindBarAndShowsItsKey(t *testing.T) {
	f, w, _, _ := terminalFrame(t, nil)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	settle(w, tt)
	if !absent(tt, LabelFind) {
		t.Fatal("the find bar shows before the click")
	}
	r := box(t, tt, LabelSearch)
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(time.Second)
	if !tt.HasText("Search  ⌘F") {
		t.Errorf("no tooltip \"Search  ⌘F\"; texts %q", tt.Texts())
	}
	if err := tt.Click(LabelSearch); err != nil {
		t.Fatal(err)
	}
	settle(w, tt)
	if !f.find.open || absent(tt, LabelFind) {
		t.Errorf("the Search button did not open the find bar; open %v", f.find.open)
	}
}

func TestSelectingAnotherTerminalClosesTheFindBar(t *testing.T) {
	f, w, tt := findFrame(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("alpha")
	settle(w, tt)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t3"})
	settle(w, tt)
	if f.find.open || f.find.query != "" || !absent(tt, LabelFind) {
		t.Errorf("the find bar stayed with another terminal: %+v", f.find)
	}
}

func TestCmdFWhileOpenFocusesTheFieldAgain(t *testing.T) {
	f, w, tt := findFrame(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("alpha")
	settle(w, tt)
	tt.Key(ui.Cmd, ui.KeyF)
	settle(w, tt)
	if !f.find.open || f.find.gen != 1 {
		t.Errorf("second ⌘F: open %v gen %d, want open with gen 1", f.find.open, f.find.gen)
	}
	// The field has the focus with its text selected: typing replaces the query.
	tt.Type("beta")
	settle(w, tt)
	if f.find.query != "beta" {
		t.Errorf("query %q after retyping, want beta", f.find.query)
	}
}
