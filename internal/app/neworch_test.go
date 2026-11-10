package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
)

// newOrchHarness shows the spaces column and the new orchestrator's dialog,
// with `+ new` wired to a fake picker, a fake spawner and a fake git. Runner
// is empty, so work runs at once.
type newOrchHarness struct {
	h      *spacesRig
	no     *NewOrchestrator
	live   agentstate.Snapshot
	sel    *state.Selection
	picked int
	spawn  []string
	status []string
	pickFn func() (string, error)
	spawnE error
	repo   bool     // what IsRepo answers
	git    *fakeGit // the git commands
}

// fakeGit records git commands and fails the one named by fail.
type fakeGit struct {
	calls []string
	fail  string
}

func (g *fakeGit) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	g.calls = append(g.calls, dir+": "+strings.Join(args, " "))
	if args[0] == g.fail {
		return nil, errors.New("git " + g.fail + " failed")
	}
	return nil, nil
}

func newOrch(t *testing.T) *newOrchHarness {
	t.Helper()
	n := &newOrchHarness{repo: true, git: &fakeGit{}, sel: state.NewSelection(nil)}
	n.live = apptest.SpTree(apptest.SpPane("w1:p1", t.TempDir(), nil))
	l := sidebar.NewSpacesList(func() agentstate.Snapshot { return n.live }, n.sel, view.Env{})
	l.Env.Run.Go = func(f func()) { f() }
	l.WatchHead = func(context.Context, string, func()) error { return nil }
	l.Sync()
	n.pickFn = func() (string, error) { return "/work/proj", nil }
	n.no = &NewOrchestrator{
		Pick: func() (string, error) { n.picked++; return n.pickFn() },
		Spawn: func(dir string) (string, error) {
			n.spawn = append(n.spawn, dir)
			if n.spawnE != nil {
				return "", n.spawnE
			}
			return "orch-new", nil
		},
		IsRepo:  func(string) bool { return n.repo },
		Git:     n.git,
		Spawned: l.SelectWhenShown,
		Env:     view.Env{Status: func(s string) { n.status = append(n.status, s) }},
	}
	l.OnNew = n.no.Start
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).Children(func() { l.View(c); n.no.View(c) })
	}, theme.SidebarWidth*3, theme.WindowHeight)
	n.h = &spacesRig{list: l, tt: tt}
	n.h.tt.Frame()
	return n
}

func TestPlusNewPicksAFolderAndSpawns(t *testing.T) {
	n := newOrch(t)
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if n.picked != 1 || len(n.spawn) != 1 || n.spawn[0] != "/work/proj" {
		t.Errorf("picked %d, spawn %q", n.picked, n.spawn)
	}
	if len(n.status) != 0 {
		t.Errorf("no message on success, got %q", n.status)
	}
}

func TestPlusNewCancelledPickerSpawnsNothing(t *testing.T) {
	n := newOrch(t)
	n.pickFn = func() (string, error) { return "", projectsview.ErrPickCancelled }
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if n.picked != 1 || len(n.spawn) != 0 || len(n.status) != 0 {
		t.Errorf("cancel: picked %d, spawn %q, status %q", n.picked, n.spawn, n.status)
	}
}

func TestPlusNewShowsASpawnError(t *testing.T) {
	n := newOrch(t)
	n.spawnE = errors.New(`no "orchestrator" profile in agents.json`)
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if len(n.status) != 1 || !strings.Contains(n.status[0], "agents.json") {
		t.Errorf("status %q", n.status)
	}
}

func TestPlusNewShowsAPickerError(t *testing.T) {
	n := newOrch(t)
	n.pickFn = func() (string, error) { return "", errors.New("dialog failed") }
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if len(n.spawn) != 0 || len(n.status) != 1 || !strings.Contains(n.status[0], "dialog failed") {
		t.Errorf("spawn %q, status %q", n.spawn, n.status)
	}
}

func TestPlusNewIgnoresAClickWhileOneRuns(t *testing.T) {
	n := newOrch(t)
	var queued []func()
	n.no.Env.Run = view.Runner{Go: func(f func()) { queued = append(queued, f) }}
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if len(queued) != 1 {
		t.Fatalf("queued %d, want 1", len(queued))
	}
	queued[0]()
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if len(queued) != 2 {
		t.Errorf("after it ends a click starts again; queued %d", len(queued))
	}
}

func TestPlusNewRunsOffTheCallingThread(t *testing.T) {
	n := newOrch(t)
	var queued []func()
	n.no.Env.Run = view.Runner{Go: func(f func()) { queued = append(queued, f) }}
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if n.picked != 0 {
		t.Error("the picker must not run in the click handler")
	}
}

func TestStartInSpawnsWithoutPicker(t *testing.T) {
	n := newOrch(t)
	n.no.StartIn("/proj/gamma")
	if n.picked != 0 || len(n.spawn) != 1 || n.spawn[0] != "/proj/gamma" {
		t.Errorf("picked %d, spawn %v", n.picked, n.spawn)
	}
}

func TestStartInShowsSpawnError(t *testing.T) {
	n := newOrch(t)
	n.spawnE = errors.New("no profile")
	n.no.StartIn("/proj/gamma")
	if len(n.status) != 1 || !strings.Contains(n.status[0], "no profile") {
		t.Errorf("status %v", n.status)
	}
}

// askMessage is the title of the dialog for /work/proj.
const askMessage = "/work/proj is not a git repository."

func TestPlusNewOutsideGitAsksAndMakesARepository(t *testing.T) {
	n := newOrch(t)
	n.repo = false
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	n.h.tt.Frame()
	if len(n.spawn) != 0 || !n.h.tt.HasText(askMessage) || !n.h.tt.HasText("Workers need one. Make it a git repository?") {
		t.Fatalf("spawn %q; the dialog must ask first", n.spawn)
	}
	if err := n.h.tt.Click(LabelMakeRepo); err != nil {
		t.Fatal(err)
	}
	n.h.tt.Frame()
	want := []string{"/work/proj: init", "/work/proj: commit --allow-empty -m Initial commit"}
	if strings.Join(n.git.calls, "|") != strings.Join(want, "|") {
		t.Errorf("git %q, want %q", n.git.calls, want)
	}
	if len(n.spawn) != 1 || n.spawn[0] != "/work/proj" || len(n.status) != 0 || n.h.tt.HasText(askMessage) {
		t.Errorf("spawn %q, status %q, dialog %v", n.spawn, n.status, n.h.tt.HasText(askMessage))
	}
}

func TestStartInOutsideGitCancelStartsNothing(t *testing.T) {
	n := newOrch(t)
	n.repo = false
	n.no.StartIn("/work/proj")
	n.h.tt.Frame()
	if !n.h.tt.HasText(askMessage) {
		t.Fatal("StartIn outside git must ask")
	}
	if err := n.h.tt.Click(LabelMakeRepoCancel); err != nil {
		t.Fatal(err)
	}
	n.h.tt.Frame()
	if len(n.spawn) != 0 || len(n.git.calls) != 0 || len(n.status) != 0 || n.h.tt.HasText(askMessage) {
		t.Errorf("cancel: spawn %q, git %q, status %q", n.spawn, n.git.calls, n.status)
	}
	n.repo = true
	n.no.StartIn("/work/proj")
	if len(n.spawn) != 1 {
		t.Errorf("after cancel a start runs again; spawn %q", n.spawn)
	}
}

func TestStartInOutsideGitShowsAGitError(t *testing.T) {
	n := newOrch(t)
	n.repo = false
	n.git.fail = "commit"
	n.no.StartIn("/work/proj")
	n.h.tt.Frame()
	if err := n.h.tt.Click(LabelMakeRepo); err != nil {
		t.Fatal(err)
	}
	if len(n.spawn) != 0 || len(n.status) != 1 || !strings.Contains(n.status[0], "git commit failed") {
		t.Errorf("spawn %q, status %q", n.spawn, n.status)
	}
	n.repo = true
	n.no.StartIn("/work/proj")
	if len(n.spawn) != 1 {
		t.Errorf("after an error a start runs again; spawn %q", n.spawn)
	}
}

func TestPlusNewIgnoresAClickWhileItAsks(t *testing.T) {
	n := newOrch(t)
	n.repo = false
	n.no.StartIn("/work/proj")
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if n.picked != 0 {
		t.Errorf("picked %d while the dialog asks", n.picked)
	}
}

func TestPlusNewSelectsTheNewOrchestrator(t *testing.T) {
	n := newOrch(t)
	repo := apptest.CanonicalTmp(t)
	apptest.MainRepo(t, repo, "ref: refs/heads/main\n")
	n.live = apptest.SpTree(apptest.SpPane("w1:p1", repo, nil))
	n.h.list.Sync()
	n.no.StartIn(repo)
	if sel, ok := n.sel.Current(); ok && sel.ID == "orch-new" {
		t.Fatal("selected before its row shows")
	}
	o := agents.Worker{Name: "orch-new", Address: "orch-new", Role: "orchestrator", Worktree: repo, Live: true}
	n.live.Orchestrators = []agents.Worker{o}
	n.h.list.Sync()
	if sel, _ := n.sel.Current(); sel.Kind != state.KindOrchestrator || sel.ID != "orch-new" {
		t.Errorf("selection %+v, want orchestrator orch-new", sel)
	}
}
