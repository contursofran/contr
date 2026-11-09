package app

// The tests of the frame, the panels and the rows that use the gh reads of package ghsource.

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/ghsource/ghsourcetest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/app/viewers"
	"github.com/contursofran/contr/internal/app/viewers/viewerstest"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

func TestAttachGivesOneGHQueueToPRsAndTitles(t *testing.T) {
	f, _ := themeFrame(t, nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(&fakeWindow{visible: true})
	defer stop()
	if f.WorkerPRs.GH == nil || f.WorkerPRs.GH != f.IssueTitles.GH {
		t.Errorf("queues %p and %p, want one shared queue", f.WorkerPRs.GH, f.IssueTitles.GH)
	}
}

func TestFrameOpenIssueRunsOffThreadAndShowsAnError(t *testing.T) {
	f := NewFrame(Deps{})
	var gotDir string
	var gotN int
	var queued []func()
	f.WorkerPRs = &ghsource.WorkerPRs{Env: view.Env{Run: view.Runner{Go: func(fn func()) { queued = append(queued, fn) }}}}
	f.OpenIssueWeb = func(d string, n int) error { gotDir, gotN = d, n; return errors.New("gh: no browser") }
	f.OpenIssue("/wt/a", 151)
	if gotN != 0 || len(queued) != 1 {
		t.Fatal("the issue opened on the calling thread")
	}
	queued[0]()
	if gotDir != "/wt/a" || gotN != 151 {
		t.Errorf("opened %q #%d", gotDir, gotN)
	}
}

func TestAgentRowOpenPRGoesToTheBrowserNotThePane(t *testing.T) {
	r := viewerstest.NewPRRig(t)
	f := NewFrame(Deps{})
	f.PR = r.Pane
	var gotDir string
	var gotN int
	var queued []func()
	f.WorkerPRs = &ghsource.WorkerPRs{Env: view.Env{Run: view.Runner{Go: func(fn func()) { queued = append(queued, fn) }}}}
	f.Toasts = state.NewToasts(func() {})
	f.OpenPRWeb = func(d string, n int) error { gotDir, gotN = d, n; return errors.New("gh: no browser") }
	e := f.agentRowEntry()
	if e.OpenPR != nil {
		t.Error("the agent rows can reach the PR pane")
	}
	if e.OpenPRWeb == nil {
		t.Fatal("no browser open")
	}
	e.OpenPRWeb("/wt/a", 307)
	if gotN != 0 || len(queued) != 1 {
		t.Fatal("the pull request opened on the calling thread")
	}
	queued[0]()
	if gotDir != "/wt/a" || gotN != 307 {
		t.Errorf("opened %q #%d", gotDir, gotN)
	}
	if f.PROpen() {
		t.Error("the PR pane opened")
	}
	if msg, ok := lastToast(f); !ok || msg != "gh: no browser" {
		t.Errorf("status %q, %v", msg, ok)
	}
}

func TestWorkerPanelOpensPR(t *testing.T) {
	entry, calls := ghsourcetest.EntryRig(307)
	src := &source{Snap: agentstate.Snapshot{Workers: []agents.Worker{apptest.EntryWorker}}}
	sel := stateSelection()
	sel.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	p := &viewers.WorkerPanel{Entry: entry, Source: src, Selection: sel}
	tt := ui.NewTester(p.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	if err := tt.Click("Open PR #307"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if len(calls.PRs) != 1 || calls.PRs[0] != [2]any{"/wt/a", 307} || len(calls.Issues) != 0 {
		t.Errorf("calls %v %v", calls.PRs, calls.Issues)
	}
}

func TestWorkerPanelOpensIssueWithoutPR(t *testing.T) {
	entry, calls := ghsourcetest.EntryRig(0)
	src := &source{Snap: agentstate.Snapshot{Workers: []agents.Worker{apptest.EntryWorker}}}
	sel := stateSelection()
	sel.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	p := &viewers.WorkerPanel{Entry: entry, Source: src, Selection: sel}
	tt := ui.NewTester(p.View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	if err := tt.Click("Open issue #268"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if len(calls.Issues) != 1 || calls.Issues[0] != [2]any{"/wt/a", 268} || len(calls.PRs) != 0 {
		t.Errorf("calls %v %v", calls.PRs, calls.Issues)
	}
}

func TestWorkerPanelNoEntryWithoutIssueOrPR(t *testing.T) {
	entry, _ := ghsourcetest.EntryRig(0)
	w := apptest.EntryWorker
	w.Issue = ""
	if label, open := entry.For(w); label != "" || open != nil {
		t.Errorf("entry %q for a worker with no issue and no PR", label)
	}
	w.Worktree = ""
	if _, open := entry.For(apptest.EntryWorker); open == nil {
		t.Error("worker with an issue should have an entry")
	}
	if _, open := entry.For(w); open != nil {
		t.Error("worker with no worktree should have no entry")
	}
}

func TestFrameOpenPRSetsTheItemOfTheWorker(t *testing.T) {
	enableViewers(t)
	r := viewerstest.NewPRRig(t)
	f := NewFrame(Deps{})
	f.PR = r.Pane
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{Workers: []agents.Worker{apptest.EntryWorker}}
	}
	f.takeSnapshot()
	f.OpenPR("/wt/a", 642)
	if r.Pane.Item != "#268" {
		t.Errorf("item %q", r.Pane.Item)
	}
	f.OpenPR("/elsewhere", 642)
	if r.Pane.Item != "" {
		t.Errorf("item %q for a folder with no worker", r.Pane.Item)
	}
}
