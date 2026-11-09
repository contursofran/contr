package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/projects"
)

// foreignTerm is a terminal that contr did not start, in dir.
func foreignTerm(addr, role, project, dir string) agents.Worker {
	return agents.Worker{Name: addr, Address: addr, Role: role, Project: project, Worktree: dir, Live: true, Foreign: true}
}

func TestSidebarShowsAProjectOfForeignTerminalsNotInTheStore(t *testing.T) {
	h, cwds := twoRepos(t)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18", Orchestrators: []agents.Worker{foreignTerm("t1", agents.RoleOrchestrator, "alpha", cwds[0])},
		Terminals: []agents.Worker{foreignTerm("t2", agents.RoleTerminal, "beta", cwds[1])}})
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
		Orchestrators: []agents.Worker{foreignTerm("t1", agents.RoleOrchestrator, "alpha", "/a")},
		Terminals: []agents.Worker{foreignTerm("t2", agents.RoleTerminal, "alpha", "/a"),
			foreignTerm("t3", agents.RoleTerminal, "beta", "/b")},
	}
	tabs := TitleTabs(s, "alpha", "", "t1", "")
	if len(tabs) != 2 || tabs[0].Address != "t1" || tabs[1].Address != "t2" || !tabs[1].Plain {
		t.Errorf("tabs %+v", tabs)
	}
}

// adoptFrame attaches a frame to a core with one unlabeled Claude Code in a
// scratch repo. It returns the frame, its window and the repo's common dir.
func adoptFrame(t *testing.T, load func() (*projects.Store, error), save func(*projects.Store) error) (*Frame, *fakeWindow, func()) {
	t.Helper()
	repo := filepath.Join(canonicalTmp(t), "scratch")
	mainRepo(t, repo, "ref: refs/heads/main\n")
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.deps.Projects.Load, f.deps.Projects.Save = load, save
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) {
		return &source{Snap: agentstate.Snapshot{CoreVersion: "0.1.18",
			Orchestrators: []agents.Worker{foreignTerm("t1", agents.RoleOrchestrator, "scratch", repo)}}}, func() {}
	}
	return f, w, f.Attach(w)
}

func TestFrameSavesAnAdoptedProjectOffTheUIThread(t *testing.T) {
	saved := make(chan *projects.Store, 4)
	f, w, stop := adoptFrame(t,
		func() (*projects.Store, error) { return &projects.Store{}, nil },
		func(s *projects.Store) error { saved <- s; return nil })
	defer stop()
	var got *projects.Store
	for deadline := time.Now().Add(5 * time.Second); got == nil && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		w.flush()
		select {
		case got = <-saved:
		default:
		}
	}
	if got == nil {
		t.Fatal("the adopted project was not saved")
	}
	if len(got.Folders()) != 1 || filepath.Base(got.Path(got.Folders()[0])) != "scratch" {
		t.Errorf("saved folders %v", got.Folders())
	}
	if len(f.SpacesList.Groups()) != 1 {
		t.Errorf("groups %v", f.SpacesList.Groups())
	}
}

func TestFrameDoesNotAdoptOverACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	saves := 0
	_, w, stop := adoptFrame(t,
		func() (*projects.Store, error) { return projects.Load(path) },
		func(s *projects.Store) error { saves++; return s.Save(path) })
	defer stop()
	for i := 0; i < 50; i++ {
		w.flush()
		time.Sleep(time.Millisecond)
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" || saves != 0 {
		t.Errorf("the corrupt file was written over (saves %d)", saves)
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
	a := foreignTerm("ta", agents.RoleTerminal, "app", "/one/app")
	a.ProjectKey = "/one/app/.git"
	b := foreignTerm("tb", agents.RoleTerminal, "app", "/two/app")
	b.ProjectKey = "/two/app/.git"
	s := agentstate.Snapshot{Terminals: []agents.Worker{a, b}}
	tabs := TitleTabs(s, "app", "/two/app/.git", "", "")
	if len(tabs) != 1 || tabs[0].Address != "tb" {
		t.Errorf("tabs of /two/app: %+v", tabs)
	}
	tabs = TitleTabs(s, "app", "/one/app/.git", "", "")
	if len(tabs) != 1 || tabs[0].Address != "ta" {
		t.Errorf("tabs of /one/app: %+v", tabs)
	}
}
