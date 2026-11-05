//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/contursofran/contr/internal/hc/hctest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

// TestTerminalOnHC runs the built app against a scratch hc
// core with one `sh` worker. The test selects the worker, checks that the
// plugin's grid shows and that hc keeps its size while contr only watches
// (D91), types `echo hi` and checks that the shell got it and that hc then
// has the grid's size, then runs vim with syntax colors. PNGs go to
// tmp/e2e/:
//
//	go test -tags e2e -run TestTerminalOnHC ./cmd/contr
//
// It never touches the owner's hc core.
func TestTerminalOnHC(t *testing.T) {
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
	// A short path: macOS limits socket paths to 103 bytes.
	core := hctest.New(t, "contr-e2e", hctest.Session("e2e"))
	dir := core.Dir
	env := core.Env

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["sh"] = agents.Profile{Argv: []string{"sh"}}
	repo := scratchRepo(t, dir, "demo", "main")
	w, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "w1", Profile: "sh", Role: "builder", Project: "contr", Worktree: repo})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	size := func() string {
		var tm struct{ Cols, Rows int }
		if err := cl.Call(ctx, "terminal.get", map[string]any{"terminal": w.Address}, &tm); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%dx%d", tm.Cols, tm.Rows)
	}

	start := size()
	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(env, "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	row := ""
	for deadline := time.Now().Add(20 * time.Second); row == "" && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}}) // draws a frame
		row = c.labelWithPrefix(t, "w1, ")
	}
	if row == "" {
		t.Fatalf("no worker row; labels %v", keys(c.labels(t)))
	}
	b := c.boundsOf(t, row)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	time.Sleep(2 * time.Second)
	c.capture(t, filepath.Join(out, "terminal-selected.png"))
	if got := size(); got != start {
		t.Errorf("size after selecting = %s, want %s: contr only watches until a key", got, start)
	}

	grid := c.boundsOf(t, "terminal grid")
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": grid.X + grid.Width/2, "y": grid.Y + grid.Height/2}}})
	c.typeLine(t, "echo h''i")
	var text string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		var rd struct{ Text string }
		if err := cl.Call(ctx, "terminal.read", map[string]any{"terminal": w.Address}, &rd); err != nil {
			t.Fatal(err)
		}
		if text = rd.Text; strings.Contains(strings.ReplaceAll(text, "echo h''i", ""), "hi") {
			break
		}
	}
	if !strings.Contains(strings.ReplaceAll(text, "echo h''i", ""), "hi") {
		t.Errorf("the shell did not run echo: %q", text)
	}
	if got := size(); got == start || got == "0x0" {
		t.Errorf("size after typing = %s, want the window's grid", got)
	}
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "terminal-typed.png"))

	c.typeLine(t, `printf '\033[1;32mgreen bold\033[0m \033[38;2;255;120;0mtruecolor\033[0m\n'; vim -u NONE -c 'syntax on' -c 'set nu' /etc/zprofile`)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		var rd struct{ Text string }
		if err := cl.Call(ctx, "terminal.read", map[string]any{"terminal": w.Address}, &rd); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rd.Text, "zprofile") {
			break
		}
	}
	time.Sleep(1500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "terminal-vim.png"))
	c.typeLine(t, ":q!")
}

// labelWithPrefix returns the first label that starts with prefix, or "".
func (c *client) labelWithPrefix(t *testing.T, prefix string) string {
	t.Helper()
	for _, nd := range c.nodes(t) {
		if strings.HasPrefix(nd.Label, prefix) {
			return nd.Label
		}
	}
	return ""
}

func (c *client) boundsOf(t *testing.T, label string) rect {
	t.Helper()
	var tree struct {
		Nodes []struct {
			Label  string
			Bounds rect
		}
	}
	if err := json.Unmarshal(c.call(t, "tree", map[string]any{}), &tree); err != nil {
		t.Fatal(err)
	}
	for _, n := range tree.Nodes {
		if n.Label == label {
			return n.Bounds
		}
	}
	t.Fatalf("no element %q", label)
	return rect{}
}

type rect struct{ X, Y, Width, Height float64 }

func (c *client) text(t *testing.T, s string) {
	t.Helper()
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "text", "text": s}}})
}

func (c *client) key(t *testing.T, k string) {
	t.Helper()
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "key", "key": k}}})
}

func (c *client) typeLine(t *testing.T, s string) {
	t.Helper()
	c.text(t, s)
	c.key(t, "Enter")
}
