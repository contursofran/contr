package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// quietStream is an attached terminal that never sends output.
type quietStream struct{ ev chan hc.StreamEvent }

func (s quietStream) Events() <-chan hc.StreamEvent { return s.ev }
func (quietStream) Err() error                      { return nil }
func (quietStream) Write(p []byte) (int, error)     { return len(p), nil }
func (quietStream) Resize(uint16, uint16) error     { return nil }
func (quietStream) Focus(bool) error                { return nil }
func (s quietStream) Close() error                  { return nil }

// scriptSource attaches with err, or with a quiet stream when err is nil. It
// counts its attaches and closes.
type scriptSource struct {
	err error

	mu      sync.Mutex
	attach  int
	closed  int
	address string
}

func (s *scriptSource) Attach(context.Context, hc.AttachParams) (ghostty.Stream, error) {
	s.mu.Lock()
	s.attach++
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return quietStream{make(chan hc.StreamEvent)}, nil
}
func (*scriptSource) Watch(context.Context) (<-chan hc.Event, func(), error) {
	return nil, func() {}, nil
}
func (s *scriptSource) Close() error { s.mu.Lock(); s.closed++; s.mu.Unlock(); return nil }
func (s *scriptSource) counts() (attach, closed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attach, s.closed
}

// terminalFrame returns a frame with a terminal slot, its window and a tester
// for the slot. sources are made on demand, one for each address.
func terminalFrame(t *testing.T, errs map[string]error) (*Frame, *fakeWindow, *ui.Tester, map[string]*scriptSource) {
	t.Helper()
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	f.Selection = state.NewSelection(f.syncTerminal)
	srcs := map[string]*scriptSource{}
	f.TerminalSource = func(addr string) ghostty.Source {
		s := &scriptSource{err: errs[addr], address: addr}
		srcs[addr] = s
		return s
	}
	stop := f.attachTerminal(w)
	t.Cleanup(stop)
	return f, w, ui.NewTester(f.buildTerminal, 600, 400), srcs
}

// waitLine runs the window's queued updates and frames until the slot shows
// text.
func waitLine(t *testing.T, w *fakeWindow, tt *ui.Tester, text string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
		if strings.Contains(strings.Join(tt.Texts(), "\n"), text) {
			return
		}
	}
	t.Fatalf("the terminal slot never showed %q; texts %q", text, tt.Texts())
}

func TestTerminalSlotSaysSelectAnAgentWithoutOne(t *testing.T) {
	_, _, tt, _ := terminalFrame(t, nil)
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), TerminalOffLine) {
		t.Errorf("texts %q, want %q", tt.Texts(), TerminalOffLine)
	}
	if !absent(tt, LabelTerminalGrid) {
		t.Error("a grid shows with no agent selected")
	}
}

func TestTerminalGridIsInsideTheTerminalAreaAndShowsNoLineWhileLive(t *testing.T) {
	f, w, _, _ := terminalFrame(t, nil)
	live := connected()
	fl := &fakeLive{}
	fl.set(live)
	f.Herdr = fl
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	tt := ui.NewTester(f.terminal, 800, 500)
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w.flush()
		tt.Frame()
		if f.termState.Kind == ghostty.Live {
			break
		}
	}
	if f.termState.Kind != ghostty.Live {
		t.Fatalf("state %+v, want live", f.termState)
	}
	tt.Frame()
	grid, area := box(t, tt, LabelTerminalGrid), box(t, tt, LabelTerminal)
	if grid.X < area.X || grid.Y < area.Y || grid.X+grid.W > area.X+area.W+0.5 || grid.Y+grid.H > area.Y+area.H+0.5 {
		t.Errorf("grid %+v is not inside the terminal area %+v", grid, area)
	}
	if got := strings.Join(tt.Texts(), "\n"); strings.Contains(got, herdr.NotRunningLine) || strings.Contains(got, TerminalOffLine) {
		t.Errorf("a live terminal shows a line: %q", got)
	}
}

func TestTerminalSlotShowsWhyItIsNotRunning(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"finished":     {&hc.Error{Kind: "finished", Message: "done"}, herdr.NotRunningLine},
		"incompatible": {&hc.Error{Kind: hc.KindIncompatible, Message: "core too old"}, "core too old"},
	} {
		t.Run(name, func(t *testing.T) {
			f, w, tt, _ := terminalFrame(t, map[string]error{"t2": tc.err})
			f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
			waitLine(t, w, tt, tc.want)
			if _, ok := tt.Find(LabelTerminalGrid); !ok {
				t.Error("the grid should stay under the line")
			}
		})
	}
}

func TestSwitchingTheAgentClosesTheOldSource(t *testing.T) {
	f, _, _, srcs := terminalFrame(t, nil)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t3"})
	if _, closed := srcs["t2"].counts(); closed != 1 {
		t.Errorf("the source of t2 closed %d times, want 1", closed)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: "/repo"})
	if f.term != nil || f.conn != nil {
		t.Error("a worktree selection should show no terminal")
	}
	if _, closed := srcs["t3"].counts(); closed != 1 {
		t.Errorf("the source of t3 closed %d times, want 1", closed)
	}
}

func TestTerminalStopClosesTheTerminal(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	f.Selection = state.NewSelection(f.syncTerminal)
	src := &scriptSource{}
	f.TerminalSource = func(string) ghostty.Source { return src }
	stop := f.attachTerminal(w)
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "t1"})
	stop()
	if _, closed := src.counts(); closed != 1 {
		t.Errorf("source closed %d times, want 1", closed)
	}
	if f.conn != nil || w.focus != nil {
		t.Error("stop should end the terminal and the focus watching")
	}
}

func TestAHiddenWindowDoesNotRetryAndShowingItDoes(t *testing.T) {
	w := &fakeWindow{visible: false}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &scriptSource{err: &hc.Error{Kind: "unavailable", Message: "no core"}}
	f.TerminalSource = func(string) ghostty.Source { return src }
	f.StartAgents = func(func()) (AgentSource, func()) { // t2 is a known worker: the syncs keep it selected
		return &source{snap: agentstate.Snapshot{Workers: []agents.Worker{{Name: "w2", Address: "t2", Live: true}}}}, func() {}
	}
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	defer stop()
	w.flush() // before the terminal exists: the terminal must start hidden by itself
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	tt := ui.NewTester(f.buildTerminal, 600, 400)
	waitLine(t, w, tt, herdr.NotRunningLine)
	time.Sleep(1200 * time.Millisecond) // longer than the first backoff step
	if n, _ := src.counts(); n != 1 {
		t.Fatalf("attaches while hidden = %d, want 1", n)
	}
	w.visible = true
	w.show()
	w.flush()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if n, _ := src.counts(); n >= 2 {
			return
		}
	}
	t.Error("showing the window did not attach again")
}

func TestWindowFocusReachesTheConnection(t *testing.T) {
	f, w, _, _ := terminalFrame(t, nil)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	w.blur()
	if f.winFocused {
		t.Error("a blur should clear the focus")
	}
	w.focus()
	if !f.winFocused {
		t.Error("a focus should set the focus")
	}
}
