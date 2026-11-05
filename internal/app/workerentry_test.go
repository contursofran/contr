package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

type entryCalls struct{ prs, issues [][2]any }

// entryRig makes a WorkerEntry with fake openers. The worktree /wt/a has the PR prN (0: none).
func entryRig(prN int) (WorkerEntry, *entryCalls) {
	calls := &entryCalls{}
	rig := newPRSourceRig(nil)
	if prN > 0 {
		rig = newPRSourceRig(map[string]string{"/wt/a": `{"number":` + itoa(prN) + `}`})
	}
	rig.w.Sync(liveWorker("/wt/a"))
	rig.flush()
	return WorkerEntry{
		PRs:       rig.w,
		OpenPR:    func(dir string, n int) { calls.prs = append(calls.prs, [2]any{dir, n}) },
		OpenIssue: func(dir string, n int) { calls.issues = append(calls.issues, [2]any{dir, n}) },
	}, calls
}

func itoa(n int) string {
	return string(rune('0'+n/100)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10))
}

var entryWorker = agents.Worker{Name: "b-1", Address: "t1", Role: "builder", Issue: "268", Worktree: "/wt/a",
	Project: "a", Live: true, Status: "blocked"}

func TestWorkerPanelOpensPR(t *testing.T) {
	entry, calls := entryRig(307)
	src := &source{snap: agentstate.Snapshot{Workers: []agents.Worker{entryWorker}}}
	sel := stateSelection()
	sel.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	p := &WorkerPanel{Entry: entry, Source: src, Selection: sel, Run: Runner{Go: func(f func()) { f() }}}
	tt := ui.NewTester(p.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	if err := tt.Click("Open PR #307"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if len(calls.prs) != 1 || calls.prs[0] != [2]any{"/wt/a", 307} || len(calls.issues) != 0 {
		t.Errorf("calls %v %v", calls.prs, calls.issues)
	}
}

func TestWorkerPanelOpensIssueWithoutPR(t *testing.T) {
	entry, calls := entryRig(0)
	src := &source{snap: agentstate.Snapshot{Workers: []agents.Worker{entryWorker}}}
	sel := stateSelection()
	sel.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	p := &WorkerPanel{Entry: entry, Source: src, Selection: sel, Run: Runner{Go: func(f func()) { f() }}}
	tt := ui.NewTester(p.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	if err := tt.Click("Open issue #268"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if len(calls.issues) != 1 || calls.issues[0] != [2]any{"/wt/a", 268} || len(calls.prs) != 0 {
		t.Errorf("calls %v %v", calls.prs, calls.issues)
	}
}

func TestWorkerPanelNoEntryWithoutIssueOrPR(t *testing.T) {
	entry, _ := entryRig(0)
	w := entryWorker
	w.Issue = ""
	if label, open := entry.For(w); label != "" || open != nil {
		t.Errorf("entry %q for a worker with no issue and no PR", label)
	}
	w.Worktree = ""
	if _, open := entry.For(entryWorker); open == nil {
		t.Error("worker with an issue should have an entry")
	}
	if _, open := entry.For(w); open != nil {
		t.Error("worker with no worktree should have no entry")
	}
}

func TestPRHeaderForItem(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.pane.Item = "#268"
	r.flush()
	if !r.tt.HasText("for #268") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	r.pane.Open("/w", 642)
	r.flush()
	if r.tt.HasText("for #268") {
		t.Error("Open should clear the item")
	}
}

func TestItemOfFindsTheWorkersIssue(t *testing.T) {
	ws := []agents.Worker{entryWorker, {Name: "x", Worktree: "/wt/b"}}
	if got := itemOf(ws, "/wt/a"); got != "#268" {
		t.Errorf("item %q", got)
	}
	if got := itemOf(ws, "/wt/b"); got != "" {
		t.Errorf("item %q for a worker with no issue", got)
	}
	if got := itemOf(ws, "/other"); got != "" {
		t.Errorf("item %q for an unknown folder", got)
	}
	if got := itemOf(ws, ""); got != "" {
		t.Errorf("item %q for no folder", got)
	}
}

func TestFrameOpenPRSetsTheItemOfTheWorker(t *testing.T) {
	enableViewers(t)
	r := newPRRig(t)
	f := NewFrame()
	f.PR = r.pane
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{Workers: []agents.Worker{entryWorker}}
	}
	f.OpenPR("/wt/a", 642)
	if r.pane.Item != "#268" {
		t.Errorf("item %q", r.pane.Item)
	}
	f.OpenPR("/elsewhere", 642)
	if r.pane.Item != "" {
		t.Errorf("item %q for a folder with no worker", r.pane.Item)
	}
}
