package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/viewers"
	"github.com/contursofran/contr/internal/session"
)

func TestFilesDirFollowsTheSelectedAgent(t *testing.T) {
	f := NewFrame(Deps{})
	f.Selection = state.NewSelection(nil)
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Address: "o1", Project: "p", Worktree: "/work/main"}},
			Workers:       []agents.Worker{{Address: "w1", Project: "p", Worktree: "/work/wt-1"}},
		}
	}
	f.takeSnapshot()
	if got := f.filesDir(); got != "" {
		t.Errorf("no selection and no project: %q", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	if got := f.filesDir(); got != "/work/wt-1" {
		t.Errorf("worker selected: %q", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o1"})
	if got := f.filesDir(); got != "/work/main" {
		t.Errorf("orchestrator selected: %q", got)
	}
}

// The frame has the tree in the files slot, and the board is gone from it.
func TestFramePanelShowsTheFileTreeNotTheBoard(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	tt.Frame()
	apptest.Box(t, tt, files.LabelFileTree)
	if !apptest.Absent(tt, LabelPanel) || !apptest.Absent(tt, "[panel]") {
		t.Errorf("the board panel shows: %q", tt.Texts())
	}
}

// A click on a file opens it in Review at the current branch.
func TestFileClickOpensTheFileInReview(t *testing.T) {
	s := apptest.NewRepo(t)
	s.Git("checkout", "-b", "other")
	s.Commit("o", map[string]string{"o.txt": "o\n"})
	s.Git("checkout", "-b", "work")
	s.Commit("w", map[string]string{"first.txt": "1\n", "wanted.txt": "w1\nw2\n"})
	f := NewFrame(Deps{})
	f.Review, _ = newTestReview()
	f.openFileInReview(s.Dir, "wanted.txt")
	if !f.ReviewOpen() {
		t.Fatal("Review did not open")
	}
	if f.Review.Branch() != "work" || f.Review.File() != "wanted.txt" {
		t.Errorf("review at %q file %q, want work wanted.txt", f.Review.Branch(), f.Review.File())
	}
	// A file the branch does not change opens the first file.
	f.openFileInReview(s.Dir, "base.txt")
	if f.Review.File() != "first.txt" {
		t.Errorf("unchanged file: review file %q, want the first", f.Review.File())
	}
}

// With the board disabled (D122), selecting an orchestrator or a worker, or
// pressing the key that opened the context sheet, leaves the file tree in the slot.
func TestFramePanelStaysTheFileTreeWhateverIsSelected(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.Selection = state.NewSelection(nil)
	f.Worker = &viewers.WorkerPanel{Source: panelSource(), Selection: f.Selection}
	for _, sel := range []state.Selected{
		{Kind: state.KindOrchestrator, ID: "o1"}, {Kind: state.KindWorker, ID: "t1"}, {Kind: state.KindTerminal, ID: "x"},
	} {
		f.Selection.Select(sel)
		tt.Key(ui.Cmd, ui.KeyJ)
		tt.Frame()
		apptest.Box(t, tt, files.LabelFileTree)
		if !apptest.Absent(tt, viewers.LabelWorkerPanel) || !apptest.Absent(tt, LabelPanel) {
			t.Errorf("%v: the board or worker panel shows", sel.Kind)
		}
	}
}

// The head says "Files", not the project name, now that the agent's todos
// and artifacts sit above it (D197).
func TestFilesPanelHeadSaysFiles(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.openProject = "contr"
	tt.Frame()
	apptest.Box(t, tt, LabelPanelFiles+" name")
	if !apptest.Absent(tt, "contr name") {
		t.Errorf("the head still shows the project: %q", tt.Texts())
	}
	apptest.Box(t, tt, LabelPanelFiles) // the group keeps its name
}

func TestFilesPanelShowsSessionAboveTheTree(t *testing.T) {
	f, tt := newTester(t)
	f.Files.Env.Run.Go = func(fn func()) { fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	dir := t.TempDir()
	if err := session.Write(dir, "t1", &session.Session{Todos: []session.Todo{{Text: "one", Status: session.Pending}}}); err != nil {
		t.Fatal(err)
	}
	f.Session.StateDir = dir
	f.Session.Terminal = func() string { return "t1" }
	f.Session.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	tt.Frame()
	todos, head := apptest.Box(t, tt, files.LabelTodos), apptest.Box(t, tt, LabelPanelFiles+" name")
	if todos.Y >= head.Y {
		t.Errorf("todos at %v, files head at %v: want the todos above", todos, head)
	}
}
