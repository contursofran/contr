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
	"github.com/contursofran/contr/internal/owner"
)

// TestOutlineCapture runs the built app against a scratch hc core with one
// orchestrator and a scratch owner store with three pinned messages, one of
// them new (it is pinned while the owner is away from the terminal). It
// captures the window in Dark and in Light, with the ticks and then with the
// card of the new tick, and checks that a click marks it read. HOME and the
// owner store are scratch folders. PNGs go to tmp/e2e/outline-*.png:
//
//	go test -tags e2e -run TestOutlineCapture ./cmd/contr
//
// It never touches the owner's hc core or owner store.
func TestOutlineCapture(t *testing.T) {
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
	core := hctest.New(t, "contr-ol", hctest.Session("ol"))
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
	script := `printf '\033[38;5;173m✱\033[0m Orchestrating contr, tracker #333\n\n' ` +
		`&& printf '  b-342  title bar        \033[2mpushed, CI running\033[0m\n  b-343  sidebar          \033[2mwaiting for review\033[0m\n  b-346  questions notch  \033[2mmerged\033[0m\n\n' ` +
		`&& printf '> pin the sidebar PR for the owner\n'; exec sh`
	cfg := agents.DefaultConfig()
	cfg.Profiles["lead"] = agents.Profile{Argv: []string{"sh", "-c", script}}
	if _, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "lead", Profile: "lead", Role: agents.RoleOrchestrator, Project: "contr"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Two messages from before the owner opened contr; the third comes later.
	ownerFile := filepath.Join(dir, "owner.json")
	now := time.Now()
	old := &owner.Store{Path: ownerFile, Now: func() time.Time { return now.Add(-3 * time.Hour) }}
	for _, p := range []owner.Todo{
		{Project: "contr", Kind: "do", From: "lead", Text: "Release name: pick one before the tag\nv0.9 needs a codename; the tag waits on this."},
		{Project: "contr", Kind: "know", From: "lead", Text: "Sidebar PR is ready for your review\nb-343 pushed 4 commits; CI is green on the last one."},
	} {
		if _, err := old.Pin(p); err != nil {
			t.Fatal(err)
		}
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+ownerFile, "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
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
		row = c.labelWithPrefix(t, "lead, ")
	}
	if row == "" {
		t.Fatalf("no orchestrator row; labels %v", keys(c.labels(t)))
	}
	// The owner is away from the terminal (no agent is selected): the third
	// message comes now, so it is new when the owner opens the terminal.
	time.Sleep(500 * time.Millisecond)
	if _, err := (&owner.Store{Path: ownerFile}).Pin(owner.Todo{Project: "contr", Kind: "do", From: "lead",
		Text: "Notch branch merged into main\nRebase your own worktree before you test the card."}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	b := c.boundsOf(t, row)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})

	has := func(label string) bool {
		for _, n := range c.nodes(t) {
			if n.Label == label {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(15 * time.Second); !has(app.LabelOutlineCount) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		poke()
	}
	if !has(app.LabelOutlineCount) || !has("Notch branch merged into main, new") || !has("Release name: pick one before the tag") {
		t.Fatalf("the outline did not show one new tick of three; labels %v", keys(c.labels(t)))
	}
	pick := func(appearance string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		for deadline := time.Now().Add(10 * time.Second); !has(app.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		sb := c.boundsOf(t, appearance)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": sb.X + sb.Width/2, "y": sb.Y + sb.Height/2}}})
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		for deadline := time.Now().Add(5 * time.Second); has(app.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		time.Sleep(600 * time.Millisecond)
	}
	hover := func() {
		tb := c.boundsOf(t, "Notch branch merged into main, new")
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": tb.X + tb.Width/2, "y": tb.Y + tb.Height/2}}})
		for deadline := time.Now().Add(10 * time.Second); !has(app.LabelOutlineCard) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		}
		time.Sleep(400 * time.Millisecond)
	}
	for _, theme := range []string{"Dark", "Light"} {
		pick(theme)
		poke()
		time.Sleep(300 * time.Millisecond)
		c.capture(t, filepath.Join(out, "outline-"+lower(theme)+".png"))
		hover()
		if !has(app.LabelOutlineCard) {
			t.Fatalf("%s: no card on hover; labels %v", theme, keys(c.labels(t)))
		}
		c.capture(t, filepath.Join(out, "outline-"+lower(theme)+"-card.png"))
		poke()
		time.Sleep(300 * time.Millisecond)
	}

	// A click marks the tick read: the count goes.
	tb := c.boundsOf(t, "Notch branch merged into main, new")
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": tb.X + tb.Width/2, "y": tb.Y + tb.Height/2}}})
	for deadline := time.Now().Add(10 * time.Second); has(app.LabelOutlineCount) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	if has(app.LabelOutlineCount) {
		t.Errorf("the count stays after the click; labels %v", keys(c.labels(t)))
	}
}

func lower(s string) string {
	if s == "Dark" {
		return "dark"
	}
	return "light"
}
