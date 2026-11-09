package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// enableViewers turns the hidden v0.2 viewers on for one test (D147).
func enableViewers(t *testing.T) {
	t.Helper()
	old := viewersEnabled
	viewersEnabled = true
	t.Cleanup(func() { viewersEnabled = old })
}

// While the viewers are hidden (D147), no entry point opens the PR pane, the board or the
// worker panel.
func TestHiddenViewersHaveNoEntryPoint(t *testing.T) {
	if viewersEnabled {
		t.Fatal("the viewers must be hidden by default")
	}
	f := NewFrame(Deps{})
	r := newPRRig(t)
	f.PR = r.pane
	src := &source{snap: boardSnap(), text: "out"}
	f.Selection = state.NewSelection(nil)
	f.Worker = &WorkerPanel{Source: src, Selection: f.Selection}
	f.Panel = &BoardPanel{Snapshot: src.Snapshot, Needs: NeedItems(src.Snapshot, nil)}
	f.Slots.Panel = f.Panel.View
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	f.OpenPR("/w", 642)
	r.flush()
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	tt.Frame()
	if f.PROpen() || !apptest.Absent(tt, LabelPRPane) {
		t.Error("OpenPR opened the PR pane")
	}
	if !apptest.Absent(tt, LabelPanel) || !apptest.Absent(tt, LabelWorkerPanel) || !apptest.Absent(tt, LabelBoardRoot) {
		t.Error("the board or the worker panel shows")
	}
	if f.paneOpener() != nil {
		t.Error("rows and cards get an action that opens the PR pane")
	}
}

// panelBox draws the v0.2 right panel for the tests of the board and the worker panel (D147):
// the worker view while a worker is selected, the slot (the board) otherwise. The frame does
// not draw it (D122).
func panelBox(f *Frame) func(c *ui.Context) {
	return func(c *ui.Context) {
		ui.Box(c).Label(LabelPanel).FillWidth().FillHeight().Children(func() {
			if f.Worker != nil && f.Worker.Following() {
				f.Worker.View(c)
				return
			}
			f.run(c, f.Slots.Panel)
		})
	}
}

// readSource is an agent source that reports each terminal Read on a channel, from any goroutine.
type readSource struct {
	*source
	reads chan string
}

func (s readSource) Read(name string, _ int) (string, error) {
	s.reads <- name
	return "out", nil
}

// attachWithWorker attaches a frame to an agent source, selects worker t1 and runs one agent
// change, as the notify of StartAgents does.
func attachWithWorker(t *testing.T) (*Frame, readSource) {
	t.Helper()
	src := readSource{source: &source{snap: boardSnap()}, reads: make(chan string, 16)}
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var notify func()
	f.deps.StartAgents = func(n func()) (AgentSource, func()) { notify = n; return src, func() {} }
	t.Cleanup(f.Attach(w))
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	notify()
	w.flush()
	return f, src
}

// Switch off: Attach builds no PR pane, worker panel or board, and an agent change with a
// worker selected reads no terminal (#560).
func TestHiddenViewersAreNotBuiltOrSynced(t *testing.T) {
	f, src := attachWithWorker(t)
	if f.PR != nil || f.Worker != nil || f.Panel != nil {
		t.Errorf("Attach built a hidden part: PR %v, worker %v, board %v", f.PR != nil, f.Worker != nil, f.Panel != nil)
	}
	select {
	case name := <-src.reads:
		t.Errorf("an agent change read terminal %q for a hidden viewer", name)
	case <-time.After(200 * time.Millisecond):
	}
}

// Switch on: Attach builds the three parts, and an agent change reads the selected worker.
func TestShownViewersAreBuiltAndSynced(t *testing.T) {
	enableViewers(t)
	f, src := attachWithWorker(t)
	if f.PR == nil || f.Worker == nil || f.Panel == nil {
		t.Fatalf("Attach did not build a part: PR %v, worker %v, board %v", f.PR != nil, f.Worker != nil, f.Panel != nil)
	}
	if f.PR.Send == nil || f.PR.Orchestrator == nil || f.Panel.Select == nil {
		t.Error("the PR pane or the board is not wired to the agent source")
	}
	select {
	case name := <-src.reads:
		if name != "t1" {
			t.Errorf("read %q, want t1", name)
		}
	case <-time.After(5 * time.Second):
		t.Error("an agent change did not read the selected worker")
	}
}
