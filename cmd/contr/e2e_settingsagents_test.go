//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestSettingsAgentsTab opens Settings in the built app and captures the Agents and Updates tabs
// in Dark and Light (D186, D187), the Agents tab also scrolled to the Roles. PNGs go to tmp/e2e/settings-<tab>-<theme>.png. HOME is a scratch
// folder, agents.json a scratch file with a custom reviewer. It never touches the owner's core:
//
//	go test -tags e2e -run TestSettingsAgentsTab ./cmd/contr
func TestSettingsAgentsTab(t *testing.T) {
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
	core := hctest.New(t, "contr-sa", hctest.Session("sa"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), []byte(`{"profiles":{"reviewer":{"argv":["codex","-m","gpt-6.1"]}},"flow":{"plan":"when-needed","review":"code","debugger_after":3,"workers":2,"merge":"ask"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home,
		"SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()
	_, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	has := func(label string) bool {
		for _, n := range c.nodes(t) {
			if n.Label == label {
				return true
			}
		}
		return false
	}
	wait := func(label string) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); !has(label) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
		}
		if !has(label) {
			t.Fatalf("no %q; labels %v", label, keys(c.labels(t)))
		}
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(600 * time.Millisecond)
	}

	wait(app.LabelNewTab)
	c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
	wait(app.LabelSettings)
	for _, theme := range []string{"Dark", "Light"} {
		click("General")
		click(theme)
		for _, tab := range []string{"Agents", "Updates"} {
			click(tab)
			c.capture(t, filepath.Join(out, "settings-"+tab+"-"+theme+".png"))
			if tab == "Agents" {
				// The Flow section is on top; scroll to the Roles below it.
				b := c.boundsOf(t, app.LabelAgentsScroll)
				c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "wheel", "x": b.X + b.Width/2, "y": b.Y + b.Height/2, "dy": 600, "precise": true}}})
				time.Sleep(600 * time.Millisecond)
				c.capture(t, filepath.Join(out, "settings-Agents-roles-"+theme+".png"))
			}
		}
	}
}
