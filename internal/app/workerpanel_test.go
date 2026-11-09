package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

func stateSelection() *state.Selection { return state.NewSelection(nil) }

func panelSource() *source {
	return &source{
		text: "building the thing\nall tests pass",
		snap: agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Name: "orch", Address: "o1", Role: "orchestrator", Project: "a", Live: true}},
			Workers: []agents.Worker{{Name: "b-1", Address: "t1", Role: "builder", Issue: "162", Worktree: "/tmp/wt",
				Project: "a", Live: true, Status: "working"}},
		},
	}
}

// followRig is a frame whose right panel (hidden in the window, D122) shows the
// board stand-in "[board]". The tests draw the panel alone.
type followRig struct {
	f   *Frame
	src *source
	tt  *ui.Tester
}

func newFollowRig(t *testing.T) *followRig {
	t.Helper()
	src := panelSource()
	f := NewFrame()
	f.Selection = stateSelection()
	f.agentSnap = src.Snapshot
	f.takeSnapshot()
	f.Worker = &WorkerPanel{Source: src, Selection: f.Selection}
	f.Slots.Panel = func(c *ui.Context) { ui.Text(c, "[board]") }
	return &followRig{f: f, src: src, tt: ui.NewTester(panelBox(f), theme.PanelWidth, theme.WindowHeight)}
}

func (r *followRig) frame() { r.tt.Frame() }

func TestPanelFollowsOrchestratorSelection(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	r.frame()
	if !r.tt.HasText("[board]") {
		t.Errorf("an orchestrator should show the board: %q", r.tt.Texts())
	}
	if _, ok := r.tt.Find(LabelWorkerPanel); ok {
		t.Error("the worker view shows for an orchestrator")
	}
}

func TestPanelShowsTheBoardWithoutSelection(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.frame()
	if !r.tt.HasText("[board]") {
		t.Errorf("no selection should show the board: %q", r.tt.Texts())
	}
}

func TestPanelFollowsWorkerSelection(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	if r.tt.HasText("[board]") {
		t.Fatal("the board shows for a worker")
	}
	for _, want := range []string{"b-1", "builder #162", "/tmp/wt", "working", WorkerInterrupt, WorkerStop} {
		if !r.tt.HasText(want) {
			t.Errorf("worker view lacks %q; texts %q", want, r.tt.Texts())
		}
	}
	if !slices.Equal(r.src.read, []string{"t1"}) {
		t.Errorf("read %v, want one read of t1", r.src.read)
	}
	r.frame()
	if len(r.src.read) != 1 {
		t.Errorf("a repaint read the terminal again: %v", r.src.read)
	}
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	r.frame()
	if !r.tt.HasText("[board]") {
		t.Error("the board should come back with the orchestrator")
	}
}

func TestWorkerPanelShowsOutputAndReadsAgainOnSync(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	r.frame()
	if r.f.Worker.out != "building the thing\nall tests pass" {
		t.Errorf("output %q", r.f.Worker.out)
	}
	if _, ok := r.tt.Find(LabelWorkerOutput); !ok {
		t.Error("the output view is missing")
	}
	r.src.text = "new line"
	r.f.Worker.Sync()
	r.frame()
	if r.f.Worker.out != "new line" || len(r.src.read) != 2 {
		t.Errorf("sync should read again; reads %v out %q", r.src.read, r.f.Worker.out)
	}
}

func TestWorkerPanelShowsAReadError(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.src.readErr = errors.New("terminal gone")
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	r.frame()
	if !r.tt.HasText("terminal gone") {
		t.Errorf("error missing: %q", r.tt.Texts())
	}
}

func TestWorkerPanelReadsOffTheUIThread(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	var queued []func()
	r.f.Worker.Env.Run = Runner{Go: func(f func()) { queued = append(queued, f) }}
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	r.f.Worker.Sync() // while the first read runs
	if len(r.src.read) != 0 || len(queued) != 1 {
		t.Fatalf("reads %v queued %d: the read must wait for its goroutine, once", r.src.read, len(queued))
	}
	queued[0]()
	if len(queued) != 2 {
		t.Errorf("the sync during the read should read again after it: %d jobs", len(queued))
	}
}

func TestWorkerPanelGoneWorker(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	r.src.snap.Workers = nil
	r.frame()
	if !r.tt.HasText("worker gone") {
		t.Errorf("gone line missing: %q", r.tt.Texts())
	}
}

func TestWorkerPanelInterrupt(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	var status string
	r.f.Worker.Env.Status = func(s string) { status = s }
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	if err := r.tt.Click(WorkerInterrupt); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.src.signalled, []string{"t1"}) || status != "b-1 interrupted" {
		t.Errorf("signalled %v status %q", r.src.signalled, status)
	}
}

func TestWorkerPanelStopAsks(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	if err := r.tt.Click(WorkerStop); err != nil {
		t.Fatal(err)
	}
	if len(r.src.stopped) != 0 {
		t.Fatal("the first click stopped the worker")
	}
	r.frame()
	if !r.tt.HasText(WorkerConfirmStop) {
		t.Fatalf("the button should ask again: %q", r.tt.Texts())
	}
	if err := r.tt.Click(WorkerConfirmStop); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.src.stopped, []string{"t1"}) {
		t.Errorf("stopped %v", r.src.stopped)
	}
}

func TestWorkerPanelInterruptDisarmsStop(t *testing.T) {
	r := newFollowRig(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	_ = r.tt.Click(WorkerStop)
	r.frame()
	_ = r.tt.Click(WorkerInterrupt)
	r.frame()
	if r.tt.HasText(WorkerConfirmStop) {
		t.Error("Interrupt should disarm Stop")
	}
}

func TestWorkerPanelActionErrorShows(t *testing.T) {
	enableViewers(t)
	r := newFollowRig(t)
	r.src.actionErr = errors.New("hc said no")
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	r.frame()
	_ = r.tt.Click(WorkerInterrupt)
	r.frame()
	if !r.tt.HasText("hc said no") {
		t.Errorf("error missing: %q", r.tt.Texts())
	}
}

func TestFrameScopeFollowsTheSelectedOrchestrator(t *testing.T) {
	r := newFollowRig(t)
	if got := r.f.scope(); got != "" {
		t.Errorf("no selection: scope %q", got)
	}
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	if got := r.f.scope(); got != "a" {
		t.Errorf("orchestrator selected: scope %q", got)
	}
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	if got := r.f.scope(); got != "" {
		t.Errorf("worker selected: scope %q", got)
	}
}
