package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// closeFrame is an agentFrame with a Close Terminal action. stopped records
// the calls to Stop; the runner works at once. A good stop removes the
// terminal from the snapshot, as the core's event does.
type closeFrame struct {
	*Frame
	tt       *ui.Tester
	snap     *agentstate.Snapshot
	stopped  []string
	stopErr  error
	statuses []string
}

func newCloseFrame(t *testing.T, snap *agentstate.Snapshot) *closeFrame {
	t.Helper()
	f, tt := agentFrame(t, snap)
	tt.Frame()
	cf := &closeFrame{Frame: f, tt: tt, snap: snap}
	f.CloseTerm = &CloseTerminal{
		Stop: func(addr string) error {
			cf.stopped = append(cf.stopped, addr)
			if cf.stopErr == nil {
				cf.snap.Terminals = slices.DeleteFunc(cf.snap.Terminals, func(w agents.Worker) bool { return w.Address == addr })
			}
			return cf.stopErr
		},
		Run:    Runner{Go: func(f func()) { f() }},
		Status: func(s string) { cf.statuses = append(cf.statuses, s) },
		Closed: f.terminalClosed,
	}
	return cf
}

func termSnap() agentstate.Snapshot {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{{Name: "term-1", Address: "tp", Project: "p", Role: agents.RoleTerminal, Worktree: "/work/p", Live: true}}
	return snap
}

func TestCloseTerminalStopsTheSelectedPlainTerminalAndSelectsAnotherTab(t *testing.T) {
	snap := termSnap()
	cf := newCloseFrame(t, &snap)
	cf.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	cf.closeSelectedTerminal()
	if !slices.Equal(cf.stopped, []string{"tp"}) {
		t.Fatalf("stopped %v, want [tp]", cf.stopped)
	}
	for _, tab := range cf.titleTabs() {
		if tab.Address == "tp" {
			t.Error("the closed terminal still has a tab")
		}
	}
	if sel, _ := cf.Selection.Current(); sel != (state.Selected{Kind: state.KindOrchestrator, ID: "o"}) {
		t.Errorf("selection %+v, want the first remaining tab", sel)
	}
}

func TestCloseTerminalOfTheLastTabClearsTheSelection(t *testing.T) {
	snap := agentstate.Snapshot{Terminals: []agents.Worker{{Name: "term-1", Address: "tp", Project: "p", Role: agents.RoleTerminal, Live: true}}}
	cf := newCloseFrame(t, &snap)
	cf.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	cf.closeSelectedTerminal()
	if len(cf.stopped) != 1 {
		t.Fatalf("stopped %v", cf.stopped)
	}
	if sel, ok := cf.Selection.Current(); ok {
		t.Errorf("selection %+v, want none", sel)
	}
}

func TestCloseTerminalDoesNothingOnOtherSelections(t *testing.T) {
	cases := map[string]state.Selected{
		"worker":       {Kind: state.KindWorker, ID: "w1"},
		"orchestrator": {Kind: state.KindOrchestrator, ID: "o"},
		"worktree":     {Kind: state.KindWorktree, ID: "/work/p"},
	}
	for name, sel := range cases {
		t.Run(name, func(t *testing.T) {
			snap := termSnap()
			cf := newCloseFrame(t, &snap)
			cf.Selection.Select(sel)
			cf.closeSelectedTerminal()
			if len(cf.stopped) != 0 || len(cf.statuses) != 0 {
				t.Errorf("stopped %v, statuses %q", cf.stopped, cf.statuses)
			}
			if got, _ := cf.Selection.Current(); got != sel {
				t.Errorf("selection %+v, want %+v", got, sel)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		snap := termSnap()
		cf := newCloseFrame(t, &snap)
		cf.closeSelectedTerminal()
		if len(cf.stopped) != 0 {
			t.Errorf("stopped %v", cf.stopped)
		}
	})
}

func TestCloseTerminalErrorShowsInTheStatusBarAndKeepsTheTab(t *testing.T) {
	snap := termSnap()
	cf := newCloseFrame(t, &snap)
	cf.stopErr = errors.New("no such terminal")
	cf.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	cf.closeSelectedTerminal()
	if !slices.Equal(cf.statuses, []string{"Close terminal: no such terminal"}) {
		t.Errorf("statuses %q", cf.statuses)
	}
	if sel, _ := cf.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "tp"}) {
		t.Errorf("selection %+v, want the terminal kept", sel)
	}
	cf.stopErr = nil
	cf.closeSelectedTerminal() // the busy mark must clear after an error
	if len(cf.stopped) != 2 {
		t.Errorf("stopped %v, want a second try to run", cf.stopped)
	}
}

func TestCloseTerminalKeepsASelectionThatMovedOn(t *testing.T) {
	snap := termSnap()
	cf := newCloseFrame(t, &snap)
	cf.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w2"})
	cf.terminalClosed("tp")
	if sel, _ := cf.Selection.Current(); sel != (state.Selected{Kind: state.KindWorker, ID: "w2"}) {
		t.Errorf("selection %+v, want w2 kept", sel)
	}
}

func TestCloseTerminalKeepsAnotherTerminalSelected(t *testing.T) {
	snap := termSnap()
	cf := newCloseFrame(t, &snap)
	cf.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "t2"})
	cf.terminalClosed("tp")
	if sel, _ := cf.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "t2"}) {
		t.Errorf("selection %+v, want t2 kept", sel)
	}
}

func TestCloseTerminalWithoutStopOrActionDoesNothing(t *testing.T) {
	(&CloseTerminal{}).Start("tp")
	snap := termSnap()
	f, _ := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	f.closeSelectedTerminal() // CloseTerm is nil: must not panic
}

func TestCloseTerminalBusy(t *testing.T) {
	var jobs []func()
	c := &CloseTerminal{Stop: func(string) error { return nil }, Run: Runner{Go: func(f func()) { jobs = append(jobs, f) }}}
	c.Start("a")
	c.Start("a")
	c.Start("b")
	if len(jobs) != 2 {
		t.Fatalf("started %d stops, want 2 (one for a, one for b)", len(jobs))
	}
	jobs[0]()
	c.Start("a")
	if len(jobs) != 3 {
		t.Error("a new stop after the first ended did not start")
	}
}

func TestAttachWiresCmdWToTheSourceClose(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &source{snap: agentstate.Snapshot{Terminals: []agents.Worker{{Name: "term-1", Address: "tp", Role: agents.RoleTerminal, Live: true}}}}
	f.StartAgents = func(func()) (AgentSource, func()) { return src, func() {} }
	stop := f.Attach(w)
	if f.CloseTerm == nil {
		t.Fatal("Attach did not make the close action")
	}
	f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	w.flush()
	it := find(t, BuildMenus().Items()[1].Submenu, "Close Terminal")
	it.Click(it, nil)
	w.flush() // runs closeSelectedTerminal, which starts the stop on a goroutine
	// Wait for the call itself: another update can be queued before it.
	var stopped, closed []string
	for deadline := time.Now().Add(5 * time.Second); len(closed)+len(stopped) == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
		stopped, closed = src.calls()
	}
	// The close result reaches the window through a later Update: flush until
	// it has run (it clears the closed terminal's selection), so no update is
	// left queued when the frame stops.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		w.flush()
		if _, ok := f.Selection.Current(); !ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := f.Selection.Current(); ok {
		t.Fatal("the close result never reached the window")
	}
	w.flush()
	// Close, not Stop: a stopped terminal stays in hc as finished, and its
	// tab would stay with it.
	if !slices.Equal(closed, []string{"tp"}) || len(stopped) != 0 {
		t.Errorf("closed %v, stopped %v; want closed [tp]", closed, stopped)
	}
	stop()
	it.Click(it, nil) // the handler is cleared with the frame
	if w.queued() != 0 {
		t.Error("the Close Terminal item still runs after the frame stopped")
	}
}
