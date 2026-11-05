package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
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
	f := NewFrame()
	r := newPRRig(t)
	f.PR = r.pane
	src := &source{snap: boardSnap(), text: "out"}
	f.Selection = state.NewSelection(nil)
	f.Worker = &WorkerPanel{Source: src, Selection: f.Selection, Run: Runner{Go: func(f func()) { f() }}}
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
	if f.PROpen() || !absent(tt, LabelPRPane) {
		t.Error("OpenPR opened the PR pane")
	}
	if !absent(tt, LabelPanel) || !absent(tt, LabelWorkerPanel) || !absent(tt, LabelBoardRoot) {
		t.Error("the board or the worker panel shows")
	}
	if f.paneOpener() != nil {
		t.Error("rows and cards get an action that opens the PR pane")
	}
}
