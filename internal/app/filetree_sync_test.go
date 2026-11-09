package app

import (
	"path/filepath"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/projects"
)

// The file tree follows a change, not the draw (#585): a selection change points it at the new
// folder before the next draw, and a redraw with no change starts no read.
func TestFileTreeFollowsTheSelectionBeforeTheDraw(t *testing.T) {
	dirP, dirQ := t.TempDir(), t.TempDir()
	snap := tabSnap()
	snap.Workers[0].Worktree = dirP // w1, project p
	snap.Workers[2].Worktree = dirQ // w3, project q
	f, tt := agentFrame(t, &snap)
	reads := 0
	f.Files.Env.Run.Go = func(fn func()) { reads++; fn() }
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	defer f.Files.Close()

	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	if got := f.Files.Shown(); got != dirP {
		t.Fatalf("after the selection the tree shows %q, want %q", got, dirP)
	}
	if reads != 1 {
		t.Fatalf("the selection started %d reads, want 1", reads)
	}
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	if reads != 1 {
		t.Errorf("redraws with no change started %d reads", reads-1)
	}

	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w3"})
	if got := f.Files.Shown(); got != dirQ {
		t.Errorf("after a selection in another project the tree shows %q, want %q", got, dirQ)
	}
}

// A change of the git-scan groups that moves the folder points the tree at the new folder,
// with no draw (#585).
func TestFileTreeFollowsTheGroups(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	store := &projects.Store{}
	store.AddFolder(filepath.Join(dir, ".git"), dir)
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.StartAgents = func(func()) (AgentSource, func()) {
		return &source{snap: agentstate.Snapshot{CoreVersion: "0.1.18",
			Orchestrators: []agents.Worker{orchW("o", "lead", "p")}}}, func() {}
	}
	stop := f.Attach(w)
	defer stop()
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	if got := f.Files.Shown(); got != "" {
		t.Fatalf("before the projects are set the tree shows %q", got)
	}
	// The projects land as loadProjects lands them, on the UI thread: no goroutine to wait for.
	f.SpacesList.SetProjects(store)
	if got := f.Files.Shown(); got != dir {
		t.Errorf("after the groups changed the tree shows %q, want %q", got, dir)
	}
}
