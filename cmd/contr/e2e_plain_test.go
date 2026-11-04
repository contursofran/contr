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
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestPlainTerminal runs the built app against a scratch hc core
// . It clicks "+", checks that a plain terminal opens as a
// tab and shows its grid, types into it, then opens a second one from the File
// menu. Cmd+T is not tested here: the automation sends keys to the view, not to
// the menu's key equivalents. Then it closes the second one from the File menu. PNGs go to
// tmp/e2e/plain-*.png. It needs `hc` and never touches the owner's core:
//
//	go test -tags e2e -run TestPlainTerminal ./cmd/contr
func TestPlainTerminal(t *testing.T) {
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
	core := hctest.New(t, "contr-pl", hctest.Session("pl"))
	dir := core.Dir
	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock,
		"SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	mgr := agents.New(agents.HC{Client: cl}, agents.DefaultConfig())
	terminals := func() []agents.Worker {
		ws, err := mgr.List(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		var out []agents.Worker
		for _, w := range ws {
			if w.Plain() {
				out = append(out, w)
			}
		}
		return out
	}
	waitTerminals := func(n int) []agents.Worker {
		t.Helper()
		var ws []agents.Worker
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if ws = terminals(); len(ws) >= n {
				return ws
			}
		}
		t.Fatalf("hc has %d plain terminals, want %d", len(ws), n)
		return nil
	}

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	wake := func() {
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
	for deadline := time.Now().Add(20 * time.Second); !has(app.LabelNewTab) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		wake()
	}
	if !has(app.LabelNewTab) {
		t.Fatalf("no + button; labels %v", keys(c.labels(t)))
	}
	c.capture(t, filepath.Join(out, "plain-0-start.png"))

	b := c.boundsOf(t, app.LabelNewTab)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	first := waitTerminals(1)[0]
	for deadline := time.Now().Add(20 * time.Second); !has(app.LabelTerminalGrid) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		wake()
	}
	if !has(app.LabelTerminalGrid) {
		t.Fatalf("no terminal grid after +; labels %v", keys(c.labels(t)))
	}
	time.Sleep(time.Second)
	grid := c.boundsOf(t, app.LabelTerminalGrid)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": grid.X + grid.Width/2, "y": grid.Y + grid.Height/2}}})
	c.typeLine(t, "echo pl''ain")
	var text string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		text, _ = mgr.Read(ctx, first.Name, 20)
		if strings.Contains(strings.ReplaceAll(text, "echo pl''ain", ""), "plain") {
			break
		}
	}
	if !strings.Contains(strings.ReplaceAll(text, "echo pl''ain", ""), "plain") {
		t.Errorf("the shell did not run echo: %q", text)
	}
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "plain-1-plus.png"))

	c.call(t, "menu", map[string]any{"path": []string{"File", "New Terminal"}})
	waitTerminals(2)
	time.Sleep(time.Second)
	c.capture(t, filepath.Join(out, "plain-2-menu.png"))

	// File > Close Terminal closes the selected (second) terminal. hc must
	// drop it, finished ones included: a stop alone leaves it as finished,
	// and its tab would stay (D85).
	c.call(t, "menu", map[string]any{"path": []string{"File", "Close Terminal"}})
	var left []agents.Worker
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if left = terminals(); len(left) == 1 {
			break
		}
	}
	if len(left) != 1 || left[0].Name != first.Name {
		t.Fatalf("after Close Terminal hc has %+v, want only %s", left, first.Name)
	}
	time.Sleep(time.Second)
	wake()
	c.capture(t, filepath.Join(out, "plain-3-closed.png"))
}
