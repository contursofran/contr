//go:build e2e

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTerminalTypingAndPaste runs the built app against the dev herdr with
// CONTR_SURFACE=1 and checks that the terminal grid shows the dev shell, that
// typed text, Enter and Backspace reach it once, and that Cmd-V through the
// Edit menu pastes once. The shell writes what it reads to a file in a
// scratch folder, so the test counts bytes and not pixels. PNGs go to
// tmp/e2e/. Start the dev herdr first:
//
//	eval "$(scripts/dev-herdr.sh)"
//	go test -tags e2e -run Terminal ./cmd/contr
//
// It refuses to run against any herdr but the dev one. The test sets the
// system clipboard for the paste check and restores its text afterwards.
func TestTerminalTypingAndPaste(t *testing.T) {
	herdrSock := os.Getenv("HERDR_SOCKET_PATH")
	if !strings.Contains(herdrSock, "contr-go-dev-herdr") {
		t.Skip("HERDR_SOCKET_PATH is not the dev herdr; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	if os.Getenv("CONTR_SURFACE") != "1" {
		t.Skip("CONTR_SURFACE is not 1; run eval \"$(scripts/dev-herdr.sh)\" first")
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
	dir, err := os.MkdirTemp("", "contr-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"))
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	c.call(t, "idle", map[string]any{"timeout": 10000})

	// The grid is in the window once herdr is connected and the link is up.
	deadline := time.Now().Add(20 * time.Second)
	for !c.labels(t)["terminal grid"] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !c.labels(t)["terminal grid"] {
		c.capture(t, filepath.Join(out, "terminal-nogrid.png"))
		t.Fatalf("no terminal grid; labels %v", keys(c.labels(t)))
	}
	grid := c.boundsOf(t, "terminal grid")
	x, y := grid.X+grid.Width/2, grid.Y+grid.Height/2
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": x, "y": y}}})
	time.Sleep(time.Second)
	c.capture(t, filepath.Join(out, "terminal-shell.png"))

	// The shell cats what it reads into a file, so the file is what the
	// shell got from the app.
	capture := func(name string, send func()) string {
		t.Helper()
		file := filepath.Join(dir, name)
		c.typeLine(t, "cat > "+file)
		time.Sleep(500 * time.Millisecond)
		send()
		time.Sleep(300 * time.Millisecond)
		c.key(t, "Enter")
		c.key(t, "Ctrl+D")
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(file); err == nil && len(b) > 0 {
				time.Sleep(300 * time.Millisecond)
				b, _ = os.ReadFile(file)
				return string(b)
			}
			time.Sleep(100 * time.Millisecond)
		}
		return ""
	}

	if got := capture("typed", func() { c.text(t, "hi") }); got != "hi\n" {
		t.Errorf("typed %q, want %q (text and Enter once)", got, "hi\n")
	}
	c.capture(t, filepath.Join(out, "terminal-typed.png"))

	if got := capture("backspace", func() { c.text(t, "abc"); c.key(t, "Backspace") }); got != "ab\n" {
		t.Errorf("backspace %q, want %q (Backspace once)", got, "ab\n")
	}

	old, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste: %v", err)
	}
	defer func() {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(string(old))
		_ = cmd.Run()
	}()
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader("pasted")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if got := capture("paste", func() {
		c.call(t, "menu", map[string]any{"path": []string{"Edit", "Paste"}})
	}); got != "pasted\n" {
		t.Errorf("paste %q, want %q (one paste)", got, "pasted\n")
	}
	c.capture(t, filepath.Join(out, "terminal-pasted.png"))
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
