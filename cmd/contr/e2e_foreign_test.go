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

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestForeignTerminals runs the built app against a scratch hc core and a
// scratch git project "demo". Two terminals start with no contr labels: a
// shell and a program named claude. The sidebar must show the orchestrator
// row, the shell must be a tab of the project, and the project must be saved
// in the scratch projects.json (D149). PNGs go to tmp/e2e/foreign-*.png:
//
//	go test -tags e2e -run TestForeignTerminals ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core.
func TestForeignTerminals(t *testing.T) {
	for _, tool := range []string{"hc", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
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
	core := hctest.New(t, "contr-fo", hctest.Session("fo"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q", "-b", "main")
	git.Dir = repo
	git.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if msg, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, msg)
	}
	// The fake Claude Code: a program whose name is claude.
	fake := filepath.Join(dir, "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '\\n  Claude Code\\n'\nexec sleep 600\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	poke := func() {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
	}
	has := func(label string) bool {
		for _, n := range c.nodes(t) {
			if n.Label == label {
				return true
			}
		}
		return false
	}
	waitFor := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			poke()
			if ok() {
				return
			}
		}
		t.Fatalf("timeout: %s; labels %v", what, keys(c.labels(t)))
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	}
	pick := func(appearance string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		waitFor("settings", 20*time.Second, func() bool { return has(app.LabelSettings) })
		click(appearance)
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		waitFor("settings closed", 20*time.Second, func() bool { return !has(app.LabelSettings) })
		time.Sleep(600 * time.Millisecond)
	}

	// Terminals that carry no contr labels, as `hc run` makes them.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	for name, argv := range map[string][]string{"claude-lead": {fake}, "2": {"sh"}} {
		p := map[string]any{"argv": argv, "name": name, "cwd": repo, "wait": false, "no_parent": true, "outlive_parent": true}
		if err := cl.Call(ctx, "terminal.run", p, nil); err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
	}

	pick("Dark")
	waitFor("the orchestrator row", 30*time.Second, func() bool { return c.labelWithPrefix(t, "claude-lead, ") != "" })
	waitFor("the plain terminal tab", 30*time.Second, func() bool { return has("2") || c.labelWithPrefix(t, "$ demo") != "" })
	time.Sleep(1500 * time.Millisecond)
	t.Logf("labels: %v", keys(c.labels(t)))
	c.capture(t, filepath.Join(out, "foreign-dark.png"))
	pick("Light")
	time.Sleep(800 * time.Millisecond)
	c.capture(t, filepath.Join(out, "foreign-light.png"))

	// The project is saved in the scratch projects.json.
	var saved string
	waitFor("projects.json", 20*time.Second, func() bool {
		matches, _ := filepath.Glob(filepath.Join(home, "*", "*", "contr", "projects.json"))
		if len(matches) == 0 {
			matches, _ = filepath.Glob(filepath.Join(home, ".config", "contr", "projects.json"))
		}
		if len(matches) == 0 {
			return false
		}
		b, _ := os.ReadFile(matches[0])
		saved = string(b)
		return strings.Contains(saved, "demo")
	})
	t.Logf("projects.json: %s", saved)
}
