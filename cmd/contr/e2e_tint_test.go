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

// TestTintSliders opens Settings in the built app and moves the Background and Panels sliders in
// Dark and in Light. PNGs go to tmp/e2e/tint-dark.png and tint-light.png; settings.json must
// hold the values. HOME is a scratch folder. It never touches the owner's core:
//
//	go test -tags e2e -run TestTintSliders ./cmd/contr
func TestTintSliders(t *testing.T) {
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
	core := hctest.New(t, "contr-ti", hctest.Session("ti"))
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
	click("Dark")
	for _, name := range []string{"dark", "light"} {
		if name == "light" {
			click("Light")
		}
		// Step the Background slider down and the Panels slider up: the values show beside them.
		for i, label := range []string{app.LabelBackground, app.LabelPanels} {
			action := []string{"decrement", "increment"}[i]
			id := c.idOf(t, label+" alpha")
			for n := 0; n < 10; n++ {
				c.call(t, "act", map[string]any{"id": id, "action": action})
			}
			time.Sleep(300 * time.Millisecond)
		}
		c.capture(t, filepath.Join(out, "tint-"+name+".png"))
	}
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(stored(), `"light": {`) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	time.Sleep(time.Second)
	got := stored()
	for _, want := range []string{`"tint"`, `"dark": {`, `"window"`, `"panel"`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings.json = %s, want %s", got, want)
		}
	}
}
