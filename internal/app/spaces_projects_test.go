package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/projects"
)

// twoRepos makes the repos alpha and beta, each with one linked worktree, and
// a tree with a terminal in each. Group 0 is alpha, group 1 is beta.
func twoRepos(t *testing.T) (*spacesHarness, []string) {
	t.Helper()
	base := canonicalTmp(t)
	var wts, cwds []string
	for _, name := range []string{"alpha", "beta"} {
		gitDir := mainRepo(t, filepath.Join(base, name), "ref: refs/heads/main\n")
		wt := filepath.Join(base, name+"-wt")
		linkedWorktree(t, gitDir, "w1", wt, "ref: refs/heads/feature\n")
		wts = append(wts, wt)
		cwds = append(cwds, wt)
	}
	h := newSpaces(t, spTree(spPane("w1:p1", wts[0], nil), spPane("w1:p2", wts[1], nil)))
	return h, cwds
}

func groupNames(gs []spaces.RepoGroup) []string {
	var n []string
	for _, g := range gs {
		n = append(n, g.Name)
	}
	return n
}

func TestSidebarFollowsStoredOrder(t *testing.T) {
	h, _ := twoRepos(t)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Fatalf("tree order %s", got)
	}
	dirs := []string{h.group(0).CommonDir, h.group(1).CommonDir}
	s := &projects.Store{}
	s.SetOrder([]string{dirs[1], dirs[0]})
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha" {
		t.Errorf("stored order gave %s", got)
	}
}

func TestSidebarLeavesHiddenGroupsOut(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	s.Hide(h.group(0).CommonDir)
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta" {
		t.Errorf("groups %s, want beta", got)
	}
}

func TestFrameShowsLoadErrorAndKeepsDefaultOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.deps.Projects.Load = func() (*projects.Store, error) { return projects.Load(path) }
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) {
		return &source{Snap: agentstate.Snapshot{}}, func() {}
	}
	stop := f.Attach(w)
	defer stop()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		w.flush()
		if _, ok := lastToast(f); ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if text, _ := lastToast(f); !strings.HasPrefix(text, "projects: ") {
		t.Errorf("message %q", text)
	}
	if f.Projects == nil {
		t.Error("a corrupt file gave no empty store")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Error("the corrupt file was overwritten")
	}
}

func TestSidebarMissingStoreFileKeepsTreeOrder(t *testing.T) {
	h, _ := twoRepos(t)
	s, err := projects.Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Errorf("groups %s, want tree order", got)
	}
}

func TestSidebarGroupMissingFromStoredOrderGoesLast(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	s.SetOrder([]string{h.group(1).CommonDir})
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha" {
		t.Errorf("groups %s, want beta,alpha", got)
	}
}

func TestFrameWithoutLoaderReadsNoStore(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	stop := f.Attach(w)
	defer stop()
	w.flush()
	if f.Projects != nil {
		t.Error("a store appeared without a loader")
	}
}

func TestSidebarShowsStoredProjectWithoutTerminalAsEmptyGroup(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	h.list.SetProjects(s)
	h.tt.Frame()
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta,gamma" {
		t.Fatalf("groups %s", got)
	}
	g := h.list.Groups()[2]
	if g.Path != "/proj/gamma" || len(g.Rows) != 0 {
		t.Fatalf("empty group = %+v", g)
	}
	var started []string
	h.list.OnNewIn = func(dir string) { started = append(started, dir) }
	h.tt.Frame()
	h.tt.RightClick(ProjectLabel(g))
	h.tt.ChooseMenuItem(LabelNewIn(g))
	if len(started) != 1 || started[0] != "/proj/gamma" {
		t.Errorf("started %v", started)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, ok := h.tt.Find(LabelNewIn(spaces.RepoGroup{Name: name})); ok {
			t.Errorf("group %s with terminals has an empty-group button", name)
		}
	}
}

func TestSidebarStoredProjectHiddenOrWithTerminalIsNotEmptyGroup(t *testing.T) {
	h, _ := twoRepos(t)
	dir := h.group(0).CommonDir
	s := &projects.Store{}
	s.AddFolder(dir, "/somewhere") // a terminal sits in it: no empty copy
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	s.Hide("/proj/gamma/.git")
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Errorf("groups %s", got)
	}
	if len(h.group(0).Rows) == 0 || h.group(0).Path != "" {
		t.Errorf("group with terminals is empty: %+v", h.group(0))
	}
}

func TestSidebarEmptyProjectsGoLastWhateverTheStoredOrder(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	s.SetOrder([]string{"/proj/gamma/.git"})
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta,gamma" {
		t.Errorf("groups %s, want the empty project last", got)
	}
}

func TestSidebarProjectsWithAgentsKeepTheDraggedOrderAroundEmptyOnes(t *testing.T) {
	h, _ := twoRepos(t)
	beta, alpha := h.group(1).CommonDir, h.group(0).CommonDir
	s := &projects.Store{}
	s.SetOrder([]string{"/proj/gamma/.git", beta, "/proj/delta/.git", alpha})
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	s.AddFolder("/proj/delta/.git", "/proj/delta")
	h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha,gamma,delta" {
		t.Errorf("groups %s, want beta,alpha then the empty ones in their order", got)
	}
}
