package sidebar

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/projects"
)

func TestSidebarShowsAProjectOfForeignTerminalsNotInTheStore(t *testing.T) {
	h, cwds := twoRepos(t)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18", Orchestrators: []agents.Worker{apptest.ForeignTerm("t1", agents.RoleOrchestrator, "alpha", cwds[0])},
		Terminals: []agents.Worker{apptest.ForeignTerm("t2", agents.RoleTerminal, "beta", cwds[1])}})
	h.list.SetProjects(&projects.Store{}) // a store that knows neither project
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Errorf("groups %s, want alpha,beta", got)
	}
	// The orchestrator row is the agent line of its project.
	if lines := agentLines(h.group(0), h.live.get()); len(lines) != 1 || !lines[0].orchestrator || lines[0].w.Address != "t1" {
		t.Errorf("agent lines of alpha: %+v", lines)
	}
}

func TestAdoptAddsALiveProjectOnceAndItSurvives(t *testing.T) {
	h, _ := twoRepos(t)
	alpha, beta := h.group(0).CommonDir, h.group(1).CommonDir
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	h.list.now = func() time.Time { return at }
	adopted := 0
	h.list.OnAdopted = func() { adopted++ }
	h.list.Adopt = true
	s := &projects.Store{}
	s.SetOrder([]string{beta}) // the owner knows beta only
	h.list.SetProjects(s)
	if adopted != 1 || !s.Known(alpha) {
		t.Fatalf("adopted %d times, store knows alpha: %v", adopted, s.Known(alpha))
	}
	if got := s.Order([]string{alpha, beta}); got[0] != beta || got[1] != alpha {
		t.Errorf("order %v, want beta then alpha", got)
	}
	if e := s.Recent(); len(e) != 1 || e[0].Dir != alpha || !e[0].At.Equal(at) || e[0].Path != filepath.Dir(alpha) {
		t.Errorf("recent %+v", e)
	}
	// More syncs add nothing and save nothing.
	h.send(h.live.get())
	h.list.SetProjects(s)
	if adopted != 1 {
		t.Errorf("adopted %d times, want once", adopted)
	}
	// The terminals end: the adopted project stays (beta was only ordered).
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18"})
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha" {
		t.Errorf("after the terminals ended: %s, want alpha", got)
	}
}

func TestAdoptLeavesAHiddenProjectAlone(t *testing.T) {
	h, _ := twoRepos(t)
	alpha := h.group(0).CommonDir
	adopted := 0
	h.list.OnAdopted = func() { adopted++ }
	h.list.Adopt = true
	s := &projects.Store{}
	s.Hide(alpha)
	h.list.SetProjects(s)
	if !s.Hidden(alpha) || s.Path(alpha) != "" || len(s.Recent()) != 1 {
		t.Errorf("hidden project touched: hidden=%v path=%q recent=%v", s.Hidden(alpha), s.Path(alpha), s.Recent())
	}
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta" {
		t.Errorf("groups %s, want beta", got)
	}
	if adopted != 1 { // beta only
		t.Errorf("adopted %d, want 1", adopted)
	}
}

func TestAdoptOffKeepsTheStoreAsItIs(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	h.list.OnAdopted = func() { t.Error("OnAdopted ran") }
	h.list.SetProjects(s) // Adopt is false: the store did not load well
	if len(s.Recent()) != 0 || s.Known(h.group(0).CommonDir) {
		t.Error("the store changed")
	}
}

func TestTabsShowAForeignPlainTerminalOfItsProject(t *testing.T) {
	s := agentstate.Snapshot{
		Orchestrators: []agents.Worker{apptest.ForeignTerm("t1", agents.RoleOrchestrator, "alpha", "/a")},
		Terminals: []agents.Worker{apptest.ForeignTerm("t2", agents.RoleTerminal, "alpha", "/a"),
			apptest.ForeignTerm("t3", agents.RoleTerminal, "beta", "/b")},
	}
	tabs := tabmodel.TitleTabs(s, "alpha", "", "t1", "")
	if len(tabs) != 2 || tabs[0].Address != "t1" || tabs[1].Address != "t2" || !tabs[1].Plain {
		t.Errorf("tabs %+v", tabs)
	}
}

func TestAdoptKeepsTheStoredOrderOfProjectsWithoutTerminals(t *testing.T) {
	h, _ := twoRepos(t)
	alpha, beta := h.group(0).CommonDir, h.group(1).CommonDir
	gone := "/x/gone/.git"
	h.list.Adopt = true
	s := &projects.Store{}
	s.SetOrder([]string{gone, beta}) // gone has no live terminal; alpha is live and unknown
	h.list.SetProjects(s)
	if !s.Known(gone) {
		t.Fatal("a stored project without a live terminal was dropped")
	}
	if got := strings.Join(s.Order([]string{gone, beta, alpha}), ","); got != strings.Join([]string{gone, beta, alpha}, ",") {
		t.Errorf("order %s, want gone, beta, alpha", got)
	}
	// Adopting again adds alpha no second time.
	h.list.SetProjects(s)
	n := 0
	for _, d := range s.Order([]string{gone, beta, alpha}) {
		if d == alpha {
			n++
		}
	}
	if n != 1 {
		t.Errorf("alpha is %d times in the order", n)
	}
}

func TestTabsKeepTwoReposWithTheSameFolderNameApart(t *testing.T) {
	a := apptest.ForeignTerm("ta", agents.RoleTerminal, "app", "/one/app")
	a.ProjectKey = "/one/app/.git"
	b := apptest.ForeignTerm("tb", agents.RoleTerminal, "app", "/two/app")
	b.ProjectKey = "/two/app/.git"
	s := agentstate.Snapshot{Terminals: []agents.Worker{a, b}}
	tabs := tabmodel.TitleTabs(s, "app", "/two/app/.git", "", "")
	if len(tabs) != 1 || tabs[0].Address != "tb" {
		t.Errorf("tabs of /two/app: %+v", tabs)
	}
	tabs = tabmodel.TitleTabs(s, "app", "/one/app/.git", "", "")
	if len(tabs) != 1 || tabs[0].Address != "ta" {
		t.Errorf("tabs of /one/app: %+v", tabs)
	}
}

// A stored folder outside git that became a git repository (D236): the scan
// finds it as the main checkout of alpha, so the stored entry moves to
// alpha's common dir and one alpha group shows, at its stored place, folded
// as before.
func TestGitInitOfAStoredFolderShowsOneGroup(t *testing.T) {
	h, _ := twoRepos(t)
	alpha, beta := h.group(0).CommonDir, h.group(1).CommonDir
	folder := filepath.Dir(alpha) // the stored key from before `git init`
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	s := &projects.Store{}
	s.AddFolder(beta, filepath.Dir(beta))
	s.AddFolder(folder, folder)
	s.Touch(folder, folder, at)
	s.SetOrder([]string{beta, folder})
	h.list.Fold(folder)
	saved := 0
	h.list.OnAdopted = func() { saved++ }
	h.list.Adopt = true
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha" {
		t.Errorf("groups %s, want beta,alpha (one alpha, at its stored place)", got)
	}
	if s.Known(folder) || s.Path(alpha) != folder || saved != 1 {
		t.Errorf("store knows the folder %v, path %q, saved %d times", s.Known(folder), s.Path(alpha), saved)
	}
	if e := s.Recent(); len(e) != 1 || e[0].Dir != alpha || !e[0].At.Equal(at) {
		t.Errorf("recent %+v, want the folder's entry under alpha", e)
	}
	if !h.list.Folded(alpha) || h.list.Folded(folder) {
		t.Errorf("folded alpha %v, folder %v; the fold must follow", h.list.Folded(alpha), h.list.Folded(folder))
	}
	h.send(h.live.get())
	if saved != 1 {
		t.Errorf("saved %d times, want once", saved)
	}
}

// A store that failed to load (Adopt off) is never changed, so the rekey
// waits too.
func TestGitInitRekeyWaitsForAGoodStore(t *testing.T) {
	h, _ := twoRepos(t)
	folder := filepath.Dir(h.group(0).CommonDir)
	s := &projects.Store{}
	s.AddFolder(folder, folder)
	h.list.SetProjects(s)
	if !s.Known(folder) {
		t.Error("the store changed while Adopt is off")
	}
}
