//go:build e2e

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestUpdatePillCapture runs the built app with a fake updater (CONTR_E2E_UPDATE) against a
// scratch core and captures the update pill, its toast and its panel in Dark and Light (D176).
// PNGs go to tmp/e2e/updatepill-*.png:
//
//	go test -tags e2e -run TestUpdatePillCapture ./cmd/contr
func TestUpdatePillCapture(t *testing.T) {
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
	core := hctest.New(t, "contr-up", hctest.Session("up"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home,
		"SHELL=/bin/sh", "CONTR_E2E_UPDATE=0.4.0",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()

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
	wait := func(label string, want bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); has(label) != want && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
		}
		if has(label) != want {
			t.Fatalf("%q shows %v; labels %v", label, !want, keys(c.labels(t)))
		}
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(600 * time.Millisecond)
	}
	theme := func(name string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		wait(settings.LabelSettings, true)
		click(name)
		click(settings.SettingsDone)
		wait(settings.LabelSettings, false)
	}

	wait(app.LabelUpdatePill, true)
	wait(app.LabelUpdateToast, true)
	theme("Dark")
	c.capture(t, filepath.Join(out, "updatepill-dark-toast.png"))
	click(app.UpdateLater)
	wait(app.LabelUpdateToast, false)
	click(app.LabelUpdatePill)
	wait(app.LabelUpdatePanel, true)
	c.capture(t, filepath.Join(out, "updatepill-dark-panel.png"))
	click(app.LabelUpdatePill)
	wait(app.LabelUpdatePanel, false)

	theme("Light")
	c.capture(t, filepath.Join(out, "updatepill-light-pill.png"))
	click(app.LabelUpdatePill)
	wait(app.LabelUpdatePanel, true)
	c.capture(t, filepath.Join(out, "updatepill-light-panel.png"))
}
