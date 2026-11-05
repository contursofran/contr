//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestReviewCapture selects a scratch repo in the sidebar, turns Review on and saves the window
// as tmp/e2e/review-whole.png and review-commit.png (By commit):
//
//	go test -tags e2e -run TestReviewCapture ./cmd/contr
//
// It uses a scratch hc core with one `sh` worker in the repo and never touches
// the owner's hc core.
func TestReviewCapture(t *testing.T) {
	skipWhileViewersHidden(t)
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

	// The scratch repo: branch feat is two commits ahead of main (an added, a modified and a
	// deleted file). The worker row selects it.
	repo, err := os.MkdirTemp("/tmp", "contr-review-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repo)
	if repo, err = filepath.EvalSymlinks(repo); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, msg)
		}
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	write("keep.txt", "one\ntwo\nthree\n")
	write("gone.txt", "old\n")
	git("add", "-A")
	git("commit", "-m", "base")
	git("checkout", "-b", "feat")
	write("new.txt", "fresh\nfile\n")
	git("add", "-A")
	git("commit", "-m", "add new.txt")
	write("keep.txt", "one\nTWO\nthree\nfour\n")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "change keep, drop gone")
	git("checkout", "-b", "e2e-wt")

	// A short path: macOS limits socket paths to 103 bytes.
	core := hctest.New(t, "contr-rv", hctest.Session("rv"))
	dir := core.Dir
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["sh"] = agents.Profile{Argv: []string{"sh"}}
	if _, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "rv", Profile: "sh", Role: "builder", Project: "review", Worktree: repo}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)

	// waitFor waits until a node's label starts with prefix, and returns the label.
	waitFor := func(prefix string, parts ...string) string {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			for _, nd := range c.nodes(t) {
				if strings.HasPrefix(nd.Label, prefix) && !strings.Contains(nd.Label, "header") && hasAll(nd.Label, parts) {
					return nd.Label
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("no element with prefix %q; labels %v", prefix, keys(c.labels(t)))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	press := func(label string) {
		t.Helper()
		c.call(t, "act", map[string]any{"id": c.idOf(t, label), "action": "press"})
		c.call(t, "idle", map[string]any{"timeout": 10000})
	}

	// The sidebar row selects on a real click, not on the press action.
	b := c.boundsOf(t, waitFor("rv, "))
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	c.call(t, "idle", map[string]any{"timeout": 10000})
	press("Review")
	waitFor("file new.txt")
	c.call(t, "idle", map[string]any{"timeout": 10000})
	for _, want := range []string{"branch feat", "file keep.txt", "file gone.txt", "file new.txt"} {
		if !c.hasPrefix(t, want) {
			t.Errorf("no element %q; labels %v", want, keys(c.labels(t)))
		}
	}
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "review-whole.png"))

	// By commit: the switch option's label is its text.
	press("By commit")
	waitFor("commit ")
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "review-commit.png"))
}

// hasAll tells whether s contains every part.
func hasAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
