//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// gitIn runs git in dir with a scratch identity and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Dana Ruiz", "GIT_AUTHOR_EMAIL=dana@example.test",
		"GIT_COMMITTER_NAME=Dana Ruiz", "GIT_COMMITTER_EMAIL=dana@example.test",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, msg)
	}
}

// scratchRepo makes a git repository with one commit under base and returns
// its canonical path.
func scratchRepo(t *testing.T, base, name, branch string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "Start "+name)
	if branch != "main" {
		gitIn(t, dir, "checkout", "-q", "-b", branch)
	}
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

// TestSidebarServersAndProjects is #343: the Servers section with This Mac,
// and the projects in the owner's order, one with an orchestrator and a
// worker, one stored with nothing open. It opens the app in Dark and in
// Light, captures both, and changes the branch of a project to see the
// HEAD watch read it again. HOME and the core are scratch ones. PNGs go to
// tmp/e2e/sidebar-dark.png and tmp/e2e/sidebar-light.png:
//
//	go test -tags e2e -run TestSidebarServersAndProjects ./cmd/contr
func TestSidebarServersAndProjects(t *testing.T) {
	hcPath, err := exec.LookPath("hc")
	if err != nil {
		t.Skip("hc is not installed")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tmp", "e2e")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(out, "contr-e2e")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}
	// A scratch core with its own session and folders, and a scratch home.
	core := hctest.New(t, "contr-sb", hctest.Session("sb"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command(hcPath, core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	r := &realApp{out: out, dir: dir, cl: cl, ctx: ctx}
	base := filepath.Join(dir, "work")
	atlas := scratchRepo(t, base, "atlas", "feat/oauth-login")
	harbor := scratchRepo(t, base, "harbor", "main")

	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "sleep 600"}}
	mgr := agents.New(agents.HC{Client: r.cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "atlas-lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "atlas", Worktree: atlas},
		{Name: "b-218", Profile: "sleepy", Role: "builder", Project: "atlas", Issue: "218", Worktree: atlas},
	} {
		if _, err := mgr.Spawn(r.ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	// The owner's files: harbor is stored with no terminal, in front of atlas.
	cfgDir := filepath.Join(home, "Library", "Application Support", "contr")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, _ := json.Marshal(map[string]any{
		"version": 1,
		"order":   []string{filepath.Join(harbor, ".git"), filepath.Join(atlas, ".git")},
		"folders": map[string]string{filepath.Join(harbor, ".git"): harbor},
	})
	if err := os.WriteFile(filepath.Join(cfgDir, "projects.json"), store, 0o644); err != nil {
		t.Fatal(err)
	}
	appearance := func(name string) {
		body := `{"version":1,"appearance":"` + name + `"}`
		if err := os.WriteFile(filepath.Join(cfgDir, "settings.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	shot := func(name string) {
		t.Helper()
		appearance(name)
		r.sock = filepath.Join(dir, "a.sock")
		_ = os.Remove(r.sock)
		r.proc = exec.Command(bin)
		r.proc.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+r.sock, "HOME="+home, "SHELL=/bin/sh",
			"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
			"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
		if err := r.proc.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.proc.Process.Kill(); _ = r.proc.Wait() }()
		c := dial(t, r.sock)
		defer c.conn.Close()
		c.waitWindow(t)
		c.waitLabel(t, "atlas, feat/oauth-login")
		c.waitLabel(t, "harbor, nothing open")
		c.waitLabel(t, "This Mac, local")
		c.call(t, "idle", map[string]any{"timeout": 10000})
		time.Sleep(2500 * time.Millisecond) // the host sampler has its readings
		c.capture(t, filepath.Join(r.out, "sidebar-"+name+".png"))
		labels := c.labels(t)
		for _, want := range []string{"Servers header", "Projects header", "Open projects", "New orchestrator"} {
			if !labels[want] {
				t.Errorf("%s: no element %q; labels %v", name, want, keys(labels))
			}
		}
		a, h := c.boundsOf(t, "atlas, feat/oauth-login"), c.boundsOf(t, "harbor, nothing open")
		if h.Y >= a.Y {
			t.Errorf("%s: harbor at %v, atlas at %v: want the stored order", name, h.Y, a.Y)
		}
		if name == "dark" {
			// A new branch is read again from HEAD, with no polling.
			gitIn(t, atlas, "checkout", "-q", "-b", "fix/token-refresh")
			c.waitLabel(t, "atlas, fix/token-refresh")
			c.call(t, "idle", map[string]any{"timeout": 10000})
			gitIn(t, atlas, "checkout", "-q", "feat/oauth-login")
			c.waitLabel(t, "atlas, feat/oauth-login")
		}
	}
	shot("dark")
	shot("light")
}
