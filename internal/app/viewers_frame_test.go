package app

// The tests of the frame that drive the hidden viewers of package viewers (D147).

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/viewers"
	"github.com/contursofran/contr/internal/app/viewers/viewerstest"
	"github.com/egoist/mygo/ui"
)

func TestFramePanelRowClickShowsTheWorker(t *testing.T) {
	enableViewers(t)
	src := &source{Snap: apptest.BoardSnap(), Text: "out"}
	f := NewFrame(Deps{})
	f.Selection = state.NewSelection(nil)
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	f.Worker = &viewers.WorkerPanel{Source: src, Selection: f.Selection}
	f.agentSnap = src.Snapshot
	f.takeSnapshot()
	scope := f.scope
	f.Panel = &viewers.BoardPanel{Snapshot: src.Snapshot, Scope: scope, Needs: agentstate.NeedItems(src.Snapshot, scope),
		Select: func(a string) { f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: a}) }}
	f.Slots.Panel = f.Panel.View
	tt := ui.NewTester(panelBox(f), theme.PanelWidth, theme.WindowHeight)
	tt.Frame()
	apptest.Box(t, tt, viewers.LabelBoardRoot)
	tt.Click(viewers.WorkRowLabel(viewers.WorkRows(src.Snap, "a")[1]))
	tt.Frame()
	apptest.Box(t, tt, viewers.LabelWorkerPanel)
}

func TestFrameOpenPRShowsThePaneAndCollapsesTabs(t *testing.T) {
	enableViewers(t)
	f := NewFrame(Deps{})
	r := viewerstest.NewPRRig(t)
	f.PR = r.Pane
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	if f.PROpen() {
		t.Fatal("closed at start")
	}
	f.OpenPR("/w", 642)
	r.Flush()
	tt.Frame()
	if !f.PROpen() || !tt.HasText("Fix bag drag target") {
		t.Errorf("pane missing: %q", tt.Texts())
	}
	if rect := apptest.Box(t, tt, viewers.LabelPRPane); rect.W != theme.PRPaneWidth {
		t.Errorf("pane width %v", rect.W)
	}
	f.ClosePR()
	tt.Frame()
	if f.PROpen() || tt.HasText("Fix bag drag target") {
		t.Error("ClosePR should hide the pane")
	}
}

func TestFramePROrchestratorFollowsTheWorkersProject(t *testing.T) {
	f := NewFrame(Deps{})
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Address: "o-a", Project: "a"}, {Address: "o-b", Project: "b"}},
			Workers:       []agents.Worker{{Address: "w1", Project: "b", Worktree: "/wt/b"}},
		}
	}
	f.takeSnapshot()
	f.openProject = "a"
	if got := f.prOrchestrator("/wt/b"); got != "o-b" {
		t.Errorf("worker folder gave %q", got)
	}
	if got := f.prOrchestrator("/other"); got != "o-a" {
		t.Errorf("open project gave %q", got)
	}
	f.openProject = "none"
	if got := f.prOrchestrator("/other"); got != "" {
		t.Errorf("no orchestrator gave %q", got)
	}
}
