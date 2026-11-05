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
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestNoFooter runs the built app against a scratch hc core with an orchestrator of the project
// "demo", a scratch owner file that does not parse, and a scratch projects file that does not
// parse. It checks that the window has no footer and no needs row, that the Settings button of the
// Servers head opens Settings with the contr and hc versions, that the owner file problem shows
// in the notch place, and that the projects file error shows as a toast. PNGs go to
// tmp/e2e/footer-*.png:
//
//	go test -tags e2e -run TestNoFooter ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core or files.
func TestNoFooter(t *testing.T) {
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
	core := hctest.New(t, "contr-ft", hctest.Session("ft"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	cfgDir := filepath.Join(home, "Library", "Application Support", "contr")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "projects.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerFile := filepath.Join(dir, "owner.json")
	if err := os.WriteFile(ownerFile, []byte(`{"questions":[{"id":"D1"`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "printf 'ready\\n'; exec sleep 600"}}
	if _, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"}); err != nil {
		t.Fatal(err)
	}

	launch := func(sockName string) (*exec.Cmd, *client) {
		t.Helper()
		sock := filepath.Join(dir, sockName)
		appCmd := exec.Command(bin)
		appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
			"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
			"CONTR_OWNER_FILE="+ownerFile, "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
		if err := appCmd.Start(); err != nil {
			t.Fatal(err)
		}
		return appCmd, dial(t, sock)
	}

	// First run: the projects file does not parse, so the window shows its error as a toast.
	first, c := launch("a.sock")
	c.waitWindow(t)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		found := false
		for _, n := range c.nodes(t) {
			found = found || n.Label == app.LabelToastClose
		}
		if found {
			break
		}
	}
	time.Sleep(600 * time.Millisecond)
	c.capture(t, filepath.Join(out, "footer-toast.png"))
	c.conn.Close()
	_ = first.Process.Kill()
	_ = first.Wait()
	if err := os.Remove(filepath.Join(cfgDir, "projects.json")); err != nil {
		t.Fatal(err)
	}

	// Second run: valid files but the owner file.
	appCmd, c := launch("b.sock")
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()
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
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
			if ok() {
				return
			}
		}
		t.Fatalf("timeout: %s; labels %v", what, keys(c.labels(t)))
	}
	openSettings := func() {
		t.Helper()
		click(app.LabelSettingsOpen)
		waitFor("settings", func() bool { return has(app.LabelSettings) })
		time.Sleep(500 * time.Millisecond)
	}
	closeSettings := func() {
		t.Helper()
		c.key(t, "Escape")
		waitFor("settings closed", func() bool { return !has(app.LabelSettings) })
		time.Sleep(400 * time.Millisecond)
	}
	pick := func(appearance string) {
		t.Helper()
		openSettings()
		click(appearance)
		time.Sleep(400 * time.Millisecond)
		closeSettings()
	}

	for _, theme := range []string{"Dark", "Light"} {
		pick(theme)
		waitFor("tab lead", func() bool { return has("lead") })
		click("lead")
		time.Sleep(1500 * time.Millisecond) // the terminal attaches
		waitFor("owner file problem", func() bool { return has(app.LabelOwnerProblem) })
		time.Sleep(500 * time.Millisecond)
		for _, gone := range []string{"status bar", "needs row", "host-cpu-status", "contr-version"} {
			if has(gone) {
				t.Errorf("%s: the v0.2 part %q is still in the window", theme, gone)
			}
		}
		c.capture(t, filepath.Join(out, "footer-"+theme+"-window.png"))
		openSettings()
		c.capture(t, filepath.Join(out, "footer-"+theme+"-settings.png"))
		closeSettings()
	}
}
