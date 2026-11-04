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
	change := h.list.SetProjects(s)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta" {
		t.Errorf("groups %s, want beta", got)
	}
	if !change.Rebuilt {
		t.Errorf("hiding a group did not report a change: %+v", change)
	}
}

func TestFrameShowsLoadErrorAndKeepsDefaultOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.LoadProjects = func() (*projects.Store, error) { return projects.Load(path) }
	f.StartAgents = func(func()) (AgentSource, func()) {
		return &source{snap: agentstate.Snapshot{}}, func() {}
	}
	stop := f.Attach(w)
	defer stop()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		w.flush()
		if _, ok := f.Status.Text(); ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if text, _ := f.Status.Text(); !strings.HasPrefix(text, "projects: ") {
		t.Errorf("message %q", text)
	}
	if f.Projects == nil {
		t.Error("a corrupt file gave no empty store")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Error("the corrupt file was overwritten")
	}
}
