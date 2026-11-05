package agentstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fterm is a terminal with no contr labels.
func fterm(addr, cwd string, extra map[string]any) json.RawMessage {
	t := map[string]any{"address": addr, "state": "live", "labels": map[string]string{}, "cwd": cwd, "turn": 1}
	for k, v := range extra {
		t[k] = v
	}
	b, _ := json.Marshal(t)
	return b
}

func TestSnapshotListsForeignTerminalsByProject(t *testing.T) {
	r := newRig(t,
		fterm("t1", "/x/alpha", map[string]any{"foreground": map[string]any{"name": "claude"}}),
		fterm("t2", "/x/alpha", map[string]any{"argv": []string{"zsh"}}),
		fterm("t3", "/x/beta", map[string]any{"argv": []string{"zsh"}}),
		fterm("t4", "/x/gone", map[string]any{"argv": []string{"zsh"}, "state": "finished"}),
	)
	r.m.resolve = func(dir string) string { return filepath.Base(dir) }
	r.start(t)
	waitFor(t, "projects", func() bool {
		s := r.m.Snapshot()
		return len(s.Terminals) == 2 && s.Terminals[0].Project != "" && len(s.Orchestrators) == 1 && s.Orchestrators[0].Project != ""
	})
	s := r.m.Snapshot()
	if s.Orchestrators[0].Address != "t1" || s.Orchestrators[0].Project != "alpha" {
		t.Errorf("orchestrator %+v", s.Orchestrators[0])
	}
	if s.Terminals[0].Project != "alpha" || s.Terminals[1].Project != "beta" {
		t.Errorf("terminals %+v", s.Terminals)
	}
}

func TestProjectLookupStaysOffTheCallerAndRunsOncePerFolder(t *testing.T) {
	r := newRig(t, fterm("t1", "/x/alpha", nil), fterm("t2", "/x/alpha", nil))
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	r.m.resolve = func(string) string {
		<-release // a slow git read
		mu.Lock()
		calls++
		mu.Unlock()
		return "alpha"
	}
	r.start(t)
	waitFor(t, "terminals", func() bool { return len(r.m.Snapshot().Terminals) == 2 })
	// The snapshot answers while the lookup waits: it does no I/O.
	done := make(chan struct{})
	go func() { _ = r.m.Snapshot(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Snapshot waited for the project lookup")
	}
	close(release)
	waitFor(t, "project", func() bool { return r.m.Snapshot().Terminals[0].Project == "alpha" })
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("lookups = %d, want 1 for one folder", calls)
	}
}

func TestProjectOf(t *testing.T) {
	base := t.TempDir()
	mk := func(p string) string {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := mk(filepath.Join(base, "myrepo"))
	mk(filepath.Join(repo, ".git"))
	sub := mk(filepath.Join(repo, "pkg", "deep"))
	// A linked worktree in another folder, as git writes it.
	admin := mk(filepath.Join(repo, ".git", "worktrees", "w1"))
	write(filepath.Join(admin, "commondir"), "../..\n")
	wt := mk(filepath.Join(base, "feature-wt"))
	write(filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	plain := mk(filepath.Join(base, "notes"))

	for name, c := range map[string]struct{ dir, want string }{
		"main checkout": {repo, "myrepo"},
		"subfolder":     {sub, "myrepo"},
		"worktree":      {wt, "myrepo"},
		"outside git":   {plain, "notes"},
		"missing":       {filepath.Join(base, "nope"), "nope"},
		"empty":         {"", ""},
	} {
		if got := ProjectOf(c.dir); got != c.want {
			t.Errorf("%s: ProjectOf(%q) = %q, want %q", name, c.dir, got, c.want)
		}
	}
}
