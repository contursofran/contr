//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestFindBarCapture runs the built app against a scratch hc core with one orchestrator whose
// terminal prints some lines, opens the find bar with ⌘F, types a query and captures the window
// in Dark and in Light with a match highlighted. It then closes the bar with Escape. HOME and the
// state folders are scratch folders. PNGs go to tmp/e2e/find-*.png:
//
//	go test -tags e2e -run TestFindBarCapture ./cmd/contr
//
// It never touches the owner's hc core.
func TestFindBarCapture(t *testing.T) {
	if _, err := exec.LookPath("hc"); err != nil {
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
	core := hctest.New(t, "contr-fd", hctest.Session("fd"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// The project is a scratch repo, so the sidebar lists the agent under it.
	repo := filepath.Join(dir, "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "start"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=demo", "GIT_AUTHOR_EMAIL=demo@example.com", "GIT_COMMITTER_NAME=demo", "GIT_COMMITTER_EMAIL=demo@example.com")
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, msg)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	script := `printf 'build started\n' ` +
		`&& printf 'compiling internal/app ... ok\ncompiling internal/hc ... ok\n' ` +
		`&& printf 'warning: unused variable in titlebar.go\n' ` +
		`&& printf 'compiling internal/agents ... ok\n' ` +
		`&& printf 'warning: slow test in sidebar_test.go\n' ` +
		`&& printf 'compiling cmd/contr ... ok\nwarning: none left\ndone\n\n'; exec sh`
	cfg := agents.DefaultConfig()
	cfg.Profiles["lead"] = agents.Profile{Argv: []string{"sh", "-c", script}}
	if _, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "lead", Profile: "lead", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
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
	waitFor := func(label string, want bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); has(label) != want && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		if has(label) != want {
			t.Fatalf("%q present %v, want %v; labels %v", label, has(label), want, keys(c.labels(t)))
		}
	}
	row := ""
	for deadline := time.Now().Add(20 * time.Second); row == "" && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		poke()
		row = c.labelWithPrefix(t, "lead, ")
	}
	if row == "" {
		t.Fatalf("no orchestrator row; labels %v", keys(c.labels(t)))
	}
	b := c.boundsOf(t, row)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	waitFor(app.LabelTerminalGrid, true)
	time.Sleep(2 * time.Second) // the lines reach the screen

	pick := func(appearance string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		waitFor(settings.LabelSettings, true)
		sb := c.boundsOf(t, appearance)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": sb.X + sb.Width/2, "y": sb.Y + sb.Height/2}}})
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		waitFor(settings.LabelSettings, false)
		time.Sleep(600 * time.Millisecond)
	}
	for _, theme := range []string{"Dark", "Light"} {
		pick(theme)
		poke()
		// ⌘F opens the bar over the terminal; Escape closes it.
		c.key(t, "CmdOrCtrl+F")
		waitFor(app.LabelFind, true)
		c.text(t, "warning")
		time.Sleep(time.Second)
		c.key(t, "Enter")
		time.Sleep(500 * time.Millisecond)
		poke()
		c.capture(t, filepath.Join(out, "find-"+lower(theme)+".png"))
		c.key(t, "Escape")
		waitFor(app.LabelFind, false)
	}
	// The Search button opens the bar too.
	sb := c.boundsOf(t, titlebar.LabelSearch)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": sb.X + sb.Width/2, "y": sb.Y + sb.Height/2}}})
	waitFor(app.LabelFind, true)
}
