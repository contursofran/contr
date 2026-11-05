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

// TestTerminalLook runs the built app against a scratch hc core with one worker
// whose terminal prints the 16 ANSI colors. It captures the window in Dark,
// then in Light after the Appearance row changes, then in Dark again, and checks
// that the terminal keeps its place while the palette changes (no reconnect).
// HOME is a scratch folder. PNGs go to tmp/e2e/terminallook-*.png:
//
//	go test -tags e2e -run TestTerminalLook ./cmd/contr
//
// It never touches the owner's hc core.
func TestTerminalLook(t *testing.T) {
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
	core := hctest.New(t, "contr-tl", hctest.Session("tl"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	// Each line: a block and a name in one color. 0-7 normal, 8-15 bright.
	script := `i=0; while [ $i -lt 8 ]; do ` +
		`printf '\033[3%dm\342\226\210\342\226\210\342\226\210 ansi %2d normal   \033[9%dm\342\226\210\342\226\210\342\226\210 ansi %2d bright\033[0m\n' $i $i $i $((i+8)); ` +
		`i=$((i+1)); done; printf 'default text, \033[2mdim\033[0m, \033[1mbold\033[0m\n'; exec sh`
	cfg := agents.DefaultConfig()
	cfg.Profiles["ansi"] = agents.Profile{Argv: []string{"sh", "-c", script}}
	w, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "w1", Profile: "ansi", Role: "builder", Project: "contr"})
	if err != nil {
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
	row := ""
	for deadline := time.Now().Add(20 * time.Second); row == "" && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		poke()
		row = c.labelWithPrefix(t, "w1, ")
	}
	if row == "" {
		t.Fatalf("no worker row; labels %v", keys(c.labels(t)))
	}
	b := c.boundsOf(t, row)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	time.Sleep(2 * time.Second)

	has := func(label string) bool {
		for _, n := range c.nodes(t) {
			if n.Label == label {
				return true
			}
		}
		return false
	}
	pick := func(appearance string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		for deadline := time.Now().Add(10 * time.Second); !has(app.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		b := c.boundsOf(t, appearance)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		for deadline := time.Now().Add(5 * time.Second); has(app.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		time.Sleep(600 * time.Millisecond)
	}
	grid := func() rect { return c.boundsOf(t, "terminal grid") }

	pick("Dark")
	c.capture(t, filepath.Join(out, "terminallook-dark.png"))
	g0 := grid()
	pick("Light")
	c.capture(t, filepath.Join(out, "terminallook-light.png"))
	if g1 := grid(); g1 != g0 {
		t.Errorf("the grid moved on a theme change: %+v then %+v", g0, g1)
	}
	pick("Dark")
	c.capture(t, filepath.Join(out, "terminallook-dark-again.png"))

	// The terminal kept its program through both changes: the colors are still on its screen.
	var rd struct{ Text string }
	if err := cl.Call(ctx, "terminal.read", map[string]any{"terminal": w.Address}, &rd); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rd.Text, "ansi 15 bright") {
		t.Errorf("the terminal lost its screen: %q", rd.Text)
	}
}
