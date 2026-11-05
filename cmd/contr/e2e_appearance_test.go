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
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestAppearanceRow opens Settings in the built app, picks Dark, then Light, and checks that the
// choice reaches settings.json. HOME is a scratch folder, so the owner's settings stay as they
// are. PNGs go to tmp/e2e/appearance-*.png. It never touches the owner's core:
//
//	go test -tags e2e -run TestAppearanceRow ./cmd/contr
func TestAppearanceRow(t *testing.T) {
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
	core := hctest.New(t, "contr-ap", hctest.Session("ap"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
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
	stored := func() string {
		data, _ := os.ReadFile(filepath.Join(home, "Library", "Application Support", "contr", "settings.json"))
		return string(data)
	}

	wait(app.LabelNewTab)
	c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
	wait(app.LabelSettings)
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "appearance-0-system.png"))

	click("Dark")
	c.capture(t, filepath.Join(out, "appearance-1-dark.png"))
	click("Light")
	c.capture(t, filepath.Join(out, "appearance-2-light.png"))
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(stored(), `"light"`) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	if got := stored(); !strings.Contains(got, `"appearance": "light"`) {
		t.Errorf("settings.json = %q, want light", got)
	}
}
