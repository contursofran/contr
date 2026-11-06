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
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestTabPanelCapture runs the built app against a scratch hc core and a scratch project with an
// orchestrator whose name is long, so its tab is selected and its title ends in an ellipsis. It
// captures the window in Dark and Light (#448, D175). PNGs go to tmp/e2e/tabpanel-*.png:
//
//	go test -tags e2e -run TestTabPanelCapture ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core.
func TestTabPanelCapture(t *testing.T) {
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
	core := hctest.New(t, "contr-tp", hctest.Session("tp"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "demo")
	for _, d := range []string{home, repo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, msg)
	}

	cfg := agents.DefaultConfig()
	cfg.Profiles["orchestrator"] = agents.Profile{Argv: []string{"sh", "-c",
		`printf '\n\342\227\217 Reading the spec before I start.\n'; exec sleep 600`, "claude", "--model", "opus"}}
	cfgPath := filepath.Join(dir, "agents.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+cfgPath)...)
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
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	name := "herdr-worker-orchestrator-demo"
	if _, err := mgr.Spawn(ctx, agents.Spec{Name: name, Profile: "orchestrator", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo}); err != nil {
		t.Fatal(err)
	}
	waitFor("the tab", 30*time.Second, func() bool { return has(name) })
	time.Sleep(1500 * time.Millisecond) // the terminal attaches

	pick("Dark")
	c.capture(t, filepath.Join(out, "tabpanel-dark.png"))
	pick("Light")
	time.Sleep(1000 * time.Millisecond)
	c.capture(t, filepath.Join(out, "tabpanel-light.png"))
}
